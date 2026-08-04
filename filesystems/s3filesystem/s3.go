package s3filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jimmitjoo/tjo/filesystems"
)

// deleteBatchLimit is the maximum number of keys DeleteObjects accepts in one
// call. v1 hid this behind s3manager.NewBatchDeleteWithClient, which has no v2
// equivalent, so the batching is ours now.
const deleteBatchLimit = 1000

type S3 struct {
	Key      string
	Secret   string
	Region   string
	Endpoint string
	Bucket   string
}

func (s *S3) getCredentials() aws.CredentialsProvider {
	return credentials.NewStaticCredentialsProvider(s.Key, s.Secret, "")
}

// client builds an S3 client for this configuration.
//
// UsePathStyle is set deliberately. In v1, supplying a custom Endpoint implied
// path-style addressing, which is what made this work against MinIO and other
// S3-compatible servers. v2 infers nothing: without it the client builds
// virtual-host URLs like https://bucket.minio.local/, which resolve nowhere.
// That is a connection error at runtime rather than a compile error, so it
// would have shipped.
func (s *S3) client() *s3.Client {
	cfg := aws.Config{
		Region:      s.Region,
		Credentials: s.getCredentials(),
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if s.Endpoint != "" {
			o.BaseEndpoint = aws.String(s.Endpoint)
		}
		o.UsePathStyle = true
	})
}

func (s *S3) Put(fileName, folder string) error {
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	uploader := manager.NewUploader(s.client())

	_, err = uploader.Upload(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(fileName),
		Body:   file,
	})

	return err
}

func (s *S3) List(prefix string) ([]filesystems.Listing, error) {
	var listing []filesystems.Listing

	paginator := s3.NewListObjectsV2Paginator(s.client(), &s3.ListObjectsV2Input{
		Bucket: aws.String(s.Bucket),
		Prefix: aws.String(prefix),
	})

	// v1 called ListObjects, which is the deprecated V1 API and returns at most
	// one page. Paginating means a bucket with more than 1000 objects no longer
	// lists as though it had exactly 1000.
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			// v2 replaced v1's string error codes with concrete types, so this
			// is errors.As rather than a switch on aerr.Code().
			var noBucket *types.NoSuchBucket
			if errors.As(err, &noBucket) {
				fmt.Println("NoSuchBucket:", err.Error())
			} else {
				fmt.Println(err.Error())
			}

			return nil, err
		}

		for _, key := range page.Contents {
			b := float64(aws.ToInt64(key.Size))
			kb := b / 1024
			mb := kb / 1024
			listing = append(listing, filesystems.Listing{
				Etag:         aws.ToString(key.ETag),
				LastModified: aws.ToTime(key.LastModified),
				Key:          aws.ToString(key.Key),
				Size:         mb,
			})
		}
	}

	return listing, nil
}

// Delete removes every object in the bucket.
//
// v1 expressed this as s3manager.NewDeleteListIterator fed into
// NewBatchDeleteWithClient. Neither exists in v2, so the list-then-delete loop
// and the 1000-key API limit are handled here.
//
// The signature takes items and ignores them. That is inherited behaviour, not
// an oversight introduced here: the v1 implementation also built its iterator
// from a bucket-wide ListObjects rather than from the argument. Preserved
// deliberately so an SDK migration does not quietly change what Delete deletes.
func (s *S3) Delete(items []string) bool {
	client := s.client()
	ctx := context.Background()

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.Bucket),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return false
		}

		batch := make([]types.ObjectIdentifier, 0, deleteBatchLimit)
		for _, object := range page.Contents {
			batch = append(batch, types.ObjectIdentifier{Key: object.Key})

			if len(batch) == deleteBatchLimit {
				if !s.deleteBatch(ctx, client, batch) {
					return false
				}
				batch = batch[:0]
			}
		}

		if len(batch) > 0 && !s.deleteBatch(ctx, client, batch) {
			return false
		}
	}

	return true
}

func (s *S3) deleteBatch(ctx context.Context, client *s3.Client, batch []types.ObjectIdentifier) bool {
	out, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(s.Bucket),
		Delete: &types.Delete{Objects: batch},
	})
	if err != nil {
		return false
	}

	// DeleteObjects reports per-key failures in the response body rather than
	// as an error, so without this a partial failure looks like success.
	return len(out.Errors) == 0
}

func (s *S3) Get(destination string, items ...string) error {
	downloader := manager.NewDownloader(s.client())

	for _, file := range items {
		if err := s.download(downloader, destination, file); err != nil {
			return err
		}
	}

	return nil
}

// download fetches one object into destination.
//
// Split out of Get so the file is closed at the end of each iteration rather
// than accumulating deferred closes until the whole batch finishes.
func (s *S3) download(downloader *manager.Downloader, destination, key string) error {
	target, err := os.Create(destinationPath(destination, key))
	if err != nil {
		return err
	}
	defer func() {
		if err := target.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	_, err = downloader.Download(context.Background(), target, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
	})

	return err
}

// destinationPath returns where an object is written locally.
//
// Get used to ignore its destination argument entirely and call
// os.Create(path.Base(key)), dropping every download into the process's working
// directory instead of where the caller asked. The minio implementation of the
// same interface honoured it.
//
// Only the base name of the key is used, so an object named "../../etc/passwd"
// cannot write outside destination.
func destinationPath(destination, key string) string {
	return filepath.Join(destination, path.Base(key))
}
