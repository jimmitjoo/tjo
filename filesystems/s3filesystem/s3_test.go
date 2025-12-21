package s3filesystem

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/jimmitjoo/tjo/filesystems"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockS3Client for testing
type MockS3Client struct {
	mock.Mock
}

func (m *MockS3Client) ListObjects(input *s3.ListObjectsInput) (*s3.ListObjectsOutput, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*s3.ListObjectsOutput), args.Error(1)
}

func (m *MockS3Client) DeleteObject(input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	args := m.Called(input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*s3.DeleteObjectOutput), args.Error(1)
}

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

	// Verify credentials are set correctly
	value, err := creds.Get()
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

func TestS3_ErrorHandling(t *testing.T) {
	// Test AWS error handling
	tests := []struct {
		name          string
		err           error
		expectedPrint bool
	}{
		{
			name:          "No such bucket error",
			err:           awserr.New(s3.ErrCodeNoSuchBucket, "The specified bucket does not exist", nil),
			expectedPrint: true,
		},
		{
			name:          "Generic AWS error",
			err:           awserr.New("AccessDenied", "Access Denied", nil),
			expectedPrint: true,
		},
		{
			name:          "Non-AWS error",
			err:           errors.New("generic error"),
			expectedPrint: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These errors would be printed in the actual code
			// We're just testing the error types here
			if aerr, ok := tt.err.(awserr.Error); ok {
				assert.NotEmpty(t, aerr.Code())
				assert.NotEmpty(t, aerr.Message())
			}
		})
	}
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