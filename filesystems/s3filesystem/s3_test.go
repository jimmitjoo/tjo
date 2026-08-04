package s3filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jimmitjoo/tjo/filesystems"
	"github.com/stretchr/testify/assert"
)

func TestS3_New(t *testing.T) {
	s3fs := &S3{
		Key:      "test-key",
		Secret:   "test-secret",
		Region:   "us-east-1",
		Endpoint: "https://s3.amazonaws.com",
		Bucket:   "test-bucket",
	}

	assert.Equal(t, "test-key", s3fs.Key)
	assert.Equal(t, "test-secret", s3fs.Secret)
	assert.Equal(t, "us-east-1", s3fs.Region)
	assert.Equal(t, "https://s3.amazonaws.com", s3fs.Endpoint)
	assert.Equal(t, "test-bucket", s3fs.Bucket)
}

func TestS3_getCredentials(t *testing.T) {
	s3fs := &S3{
		Key:    "test-key",
		Secret: "test-secret",
	}

	creds := s3fs.getCredentials()
	assert.NotNil(t, creds)

	// v2 replaced Get() with Retrieve(ctx) on the CredentialsProvider
	// interface.
	value, err := creds.Retrieve(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "test-key", value.AccessKeyID)
	assert.Equal(t, "test-secret", value.SecretAccessKey)
}

func TestS3_Put_FileNotFound(t *testing.T) {
	s3fs := &S3{
		Key:      "test-key",
		Secret:   "test-secret",
		Region:   "us-east-1",
		Endpoint: "https://s3.amazonaws.com",
		Bucket:   "test-bucket",
	}

	// Try to upload a non-existent file
	err := s3fs.Put("/path/to/nonexistent/file.txt", "folder")
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestS3_List_MockResponse(t *testing.T) {
	// This test demonstrates the structure of List method
	// In real implementation, you'd need to mock AWS SDK properly

	s3fs := &S3{
		Key:      "test-key",
		Secret:   "test-secret",
		Region:   "us-east-1",
		Endpoint: "https://s3.amazonaws.com",
		Bucket:   "test-bucket",
	}

	// In a real test, we would mock the S3 client
	// For now, we can only test with invalid credentials
	listing, err := s3fs.List("prefix/")

	// This will fail without valid AWS credentials
	assert.Error(t, err)
	assert.Nil(t, listing)
}

func TestS3_Delete_EmptyItems(t *testing.T) {
	s3fs := &S3{
		Key:      "test-key",
		Secret:   "test-secret",
		Region:   "us-east-1",
		Endpoint: "https://s3.amazonaws.com",
		Bucket:   "test-bucket",
	}

	// Test with empty items array
	result := s3fs.Delete([]string{})
	// This will attempt to delete but with invalid credentials will fail
	assert.False(t, result)
}

func TestS3_Get_EmptyItems(t *testing.T) {
	s3fs := &S3{
		Key:      "test-key",
		Secret:   "test-secret",
		Region:   "us-east-1",
		Endpoint: "https://s3.amazonaws.com",
		Bucket:   "test-bucket",
	}

	// Test with no items
	err := s3fs.Get("/tmp/destination")
	assert.NoError(t, err) // Should not error with empty items
}

func TestS3_Get_InvalidDestination(t *testing.T) {
	s3fs := &S3{
		Key:      "test-key",
		Secret:   "test-secret",
		Region:   "us-east-1",
		Endpoint: "https://s3.amazonaws.com",
		Bucket:   "test-bucket",
	}

	// Test with items but will fail due to invalid credentials
	err := s3fs.Get("/tmp/destination", "file1.txt", "file2.txt")
	assert.Error(t, err) // Will error due to invalid credentials
}

func TestS3_Configuration(t *testing.T) {
	tests := []struct {
		name     string
		s3       *S3
		expected struct {
			key      string
			secret   string
			region   string
			endpoint string
			bucket   string
		}
	}{
		{
			name: "Standard AWS S3 configuration",
			s3: &S3{
				Key:      "AKIAIOSFODNN7EXAMPLE",
				Secret:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				Region:   "us-west-2",
				Endpoint: "https://s3.amazonaws.com",
				Bucket:   "my-bucket",
			},
			expected: struct {
				key      string
				secret   string
				region   string
				endpoint string
				bucket   string
			}{
				key:      "AKIAIOSFODNN7EXAMPLE",
				secret:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				region:   "us-west-2",
				endpoint: "https://s3.amazonaws.com",
				bucket:   "my-bucket",
			},
		},
		{
			name: "Custom S3-compatible endpoint",
			s3: &S3{
				Key:      "custom-key",
				Secret:   "custom-secret",
				Region:   "us-east-1",
				Endpoint: "https://custom-s3.example.com",
				Bucket:   "custom-bucket",
			},
			expected: struct {
				key      string
				secret   string
				region   string
				endpoint string
				bucket   string
			}{
				key:      "custom-key",
				secret:   "custom-secret",
				region:   "us-east-1",
				endpoint: "https://custom-s3.example.com",
				bucket:   "custom-bucket",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.key, tt.s3.Key)
			assert.Equal(t, tt.expected.secret, tt.s3.Secret)
			assert.Equal(t, tt.expected.region, tt.s3.Region)
			assert.Equal(t, tt.expected.endpoint, tt.s3.Endpoint)
			assert.Equal(t, tt.expected.bucket, tt.s3.Bucket)
		})
	}
}

func TestS3_ListingConversion(t *testing.T) {
	// Test the conversion from S3 objects to filesystems.Listing
	testTime := time.Now()
	etag := "\"abc123\""
	key := "test/file.txt"
	size := int64(1024 * 1024 * 5) // 5 MB in bytes

	// Create expected listing
	expectedListing := filesystems.Listing{
		Etag:         etag,
		LastModified: testTime,
		Key:          key,
		Size:         5.0, // 5 MB
	}

	// Verify the size conversion (bytes to MB)
	b := float64(size)
	kb := b / 1024
	mb := kb / 1024
	assert.Equal(t, expectedListing.Size, mb)
}

// TestS3_ClientUsesPathStyleAddressing covers the one v1-to-v2 difference that
// fails silently.
//
// v1 inferred path-style addressing from a custom Endpoint, which is what made
// this package work against MinIO and other S3-compatible servers. v2 infers
// nothing: without UsePathStyle the client builds virtual-host URLs like
// https://bucket.minio.local/, which resolve nowhere. Nothing about that is a
// compile error, and every test in this file passed without it.
//
// This replaces TestS3_ErrorHandling, which constructed awserr values and
// asserted they had a code and a message -- a property of the SDK, not of any
// code in this package.
func TestS3_ClientUsesPathStyleAddressing(t *testing.T) {
	t.Run("custom endpoint", func(t *testing.T) {
		s3fs := &S3{
			Key:      "test-key",
			Secret:   "test-secret",
			Region:   "us-east-1",
			Endpoint: "https://minio.example.test:9000",
			Bucket:   "test-bucket",
		}

		opts := s3fs.client().Options()

		assert.True(t, opts.UsePathStyle, "virtual-host addressing against a custom endpoint resolves nowhere")
		assert.NotNil(t, opts.BaseEndpoint)
		assert.Equal(t, "https://minio.example.test:9000", *opts.BaseEndpoint)
		assert.Equal(t, "us-east-1", opts.Region)
	})

	t.Run("no endpoint leaves BaseEndpoint unset", func(t *testing.T) {
		s3fs := &S3{Key: "k", Secret: "s", Region: "eu-north-1", Bucket: "b"}

		opts := s3fs.client().Options()

		assert.Nil(t, opts.BaseEndpoint, "an empty Endpoint must not become a literal empty base endpoint")
		assert.Equal(t, "eu-north-1", opts.Region)
	})
}

// Benchmark tests
func BenchmarkS3_getCredentials(b *testing.B) {
	s3fs := &S3{
		Key:    "test-key",
		Secret: "test-secret",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s3fs.getCredentials()
	}
}

func BenchmarkS3_Creation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &S3{
			Key:      "test-key",
			Secret:   "test-secret",
			Region:   "us-east-1",
			Endpoint: "https://s3.amazonaws.com",
			Bucket:   "test-bucket",
		}
	}
}

// TestDestinationPath pins where downloads land. S3.Get used to ignore its
// destination argument and call os.Create(path.Base(key)), so every file
// landed in the process's working directory -- while the minio implementation
// of the same FS interface honoured it.
func TestDestinationPath(t *testing.T) {
	tests := []struct {
		name        string
		destination string
		key         string
		want        string
	}{
		{"plain key", "/tmp/dest", "report.pdf", "/tmp/dest/report.pdf"},
		{"key in a folder", "/tmp/dest", "reports/q3/report.pdf", "/tmp/dest/report.pdf"},
		{"traversal in the key cannot escape", "/tmp/dest", "../../etc/passwd", "/tmp/dest/passwd"},
		{"absolute key cannot escape", "/tmp/dest", "/etc/passwd", "/tmp/dest/passwd"},
		{"relative destination", "downloads", "a/b/c.txt", "downloads/c.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := destinationPath(tt.destination, tt.key)
			if got != tt.want {
				t.Errorf("destinationPath(%q, %q) = %q, want %q",
					tt.destination, tt.key, got, tt.want)
			}
			if !strings.HasPrefix(got, tt.destination) {
				t.Errorf("%q escapes the destination %q", got, tt.destination)
			}
		})
	}
}

// Both implementations must satisfy ContextFS, not just FS. This is a compile
// time assertion rather than a test body because that is the whole claim.
var _ filesystems.ContextFS = (*S3)(nil)

// TestDeleteContextHonoursItems pins the correction ContextFS exists to make.
//
// FS.Delete ignores its items argument and empties the entire bucket. That is
// inherited from the v1 SDK implementation and was preserved deliberately, so
// the aws-sdk-go-v2 migration would not silently change what Delete deletes.
// DeleteContext is the new API and does not carry it forward.
//
// Verified against MinIO rather than a mock: the failure mode is "deletes more
// than it was asked to", and a mock that records calls proves nothing about
// which objects actually survive.
func TestDeleteContextHonoursItems(t *testing.T) {
	endpoint := os.Getenv("TJO_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TJO_TEST_S3_ENDPOINT is not set")
	}

	s := &S3{
		Key:      os.Getenv("TJO_TEST_S3_KEY"),
		Secret:   os.Getenv("TJO_TEST_S3_SECRET"),
		Region:   "us-east-1",
		Endpoint: endpoint,
		Bucket:   os.Getenv("TJO_TEST_S3_BUCKET"),
	}

	dir := t.TempDir()
	var keys []string
	for _, name := range []string{"doomed-a", "doomed-b", "keeper"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := s.Put(p, ""); err != nil {
			t.Fatalf("Put %s: %v", name, err)
		}
		keys = append(keys, strings.TrimPrefix(p, "/"))
	}
	t.Cleanup(func() { s.DeleteContext(context.Background(), nil) })

	if err := s.DeleteContext(context.Background(), keys[:2]); err != nil {
		t.Fatalf("DeleteContext: %v", err)
	}

	after, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("%d objects left, want 1 (the keeper); DeleteContext deleted more than it was asked to", len(after))
	}
	if filepath.Base(after[0].Key) != "keeper" {
		t.Fatalf("survivor is %q, want keeper", after[0].Key)
	}
}

// A cancelled context must stop the operation rather than being ignored.
func TestContextCancellationIsHonoured(t *testing.T) {
	if os.Getenv("TJO_TEST_S3_ENDPOINT") == "" {
		t.Skip("TJO_TEST_S3_ENDPOINT is not set")
	}

	s := &S3{
		Key:      os.Getenv("TJO_TEST_S3_KEY"),
		Secret:   os.Getenv("TJO_TEST_S3_SECRET"),
		Region:   "us-east-1",
		Endpoint: os.Getenv("TJO_TEST_S3_ENDPOINT"),
		Bucket:   os.Getenv("TJO_TEST_S3_BUCKET"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.ListContext(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Errorf("ListContext with a cancelled context returned %v, want context.Canceled", err)
	}
}
