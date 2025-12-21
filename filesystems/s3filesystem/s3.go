package s3filesystem

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/jimmitjoo/tjo/filesystems"
	"os"
	"path"
)

type S3 struct {
	Key      string
	Secret   string
	Region   string
	Endpoint string
	Bucket   string
}

func (s *S3) getCredentials() *credentials.Credentials {
	creds := credentials.NewStaticCredentials(s.Key, s.Secret, "")

	return creds
}

func (s *S3) Put(fileName, folder string) error {
	creds := s.getCredentials()
	sess := session.Must(session.NewSession(&aws.Config{
		Endpoint:    &s.Endpoint,
		Region:      &s.Region,
		Credentials: creds,
	}))

	uploader := s3manager.NewUploader(sess)

	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(fileName),
		Body:   file,
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *S3) List(prefix string) ([]filesystems.Listing, error) {
	var listing []filesystems.Listing

	creds := s.getCredentials()
	sess := session.Must(session.NewSession(&aws.Config{
		Endpoint:    &s.Endpoint,
		Region:      &s.Region,
		Credentials: creds,
	}))

	service := s3.New(sess)
	input := &s3.ListObjectsInput{
		Bucket: aws.String(s.Bucket),
		Prefix: aws.String(prefix),
	}

	result, err := service.ListObjects(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket:
				fmt.Println(s3.ErrCodeNoSuchBucket, aerr.Error())
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}

		return nil, err
	}

	for _, key := range result.Contents {
		b := float64(*key.Size)
		kb := b / 1024
		mb := kb / 1024
		current := filesystems.Listing{
			Etag:         *key.ETag,
			LastModified: *key.LastModified,
			Key:          *key.Key,
			Size:         mb,
		}
		listing = append(listing, current)
	}

	return listing, nil
}

func (s *S3) Delete(items []string) bool {

	creds := s.getCredentials()
	sess := session.Must(session.NewSession(&aws.Config{
		Endpoint:    &s.Endpoint,
		Region:      &s.Region,
		Credentials: creds,
	}))

	service := s3.New(sess)

	iter := s3manager.NewDeleteListIterator(service, &s3.ListObjectsInput{
		Bucket: aws.String(s.Bucket),
	})
	if err := s3manager.NewBatchDeleteWithClient(service).Delete(aws.BackgroundContext(), iter); err != nil {
		return false
	}

	return true
}

func (s *S3) Get(destination string, items ...string) error {

	creds := s.getCredentials()
	sess := session.Must(session.NewSession(&aws.Config{
		Endpoint:    &s.Endpoint,
		Region:      &s.Region,
		Credentials: creds,
	}))

	downloader := s3manager.NewDownloader(sess)

	for _, file := range items {
		fileName, err := os.Create(path.Base(file))
		if err != nil {
			return err
		}
		defer func() {
			err := fileName.Close()
			if err != nil {
				fmt.Println(err)
			}
		}()

		_, err = downloader.Download(fileName, &s3.GetObjectInput{
			Bucket: aws.String(s.Bucket),
			Key:    aws.String(file),
		})
		if err != nil {
			return err
		}

	}

	return nil
}
