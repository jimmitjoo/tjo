package filesystems

import (
	"context"
	"time"
)

// FS is an interface that defines the methods that a filesystem must implement
type FS interface {
	Put(fileName, folder string) error
	Get(destination string, items ...string) error
	List(prefix string) ([]Listing, error)
	Delete(items []string) bool
}

// ContextFS is FS with cancellation, deadlines and trace propagation.
//
// The four methods on FS take no context, so every implementation ends up
// calling context.Background() internally. That means an upload whose HTTP
// handler has already gone away keeps running -- for S3, billable transfer for
// a response nobody will read -- a Get of a large object cannot be bounded, and
// anything the otel module traces stops at the filesystem boundary because the
// span context cannot reach it.
//
// This is a second interface rather than a change to FS, because changing FS
// would break every implementation written against it to deliver a benefit
// callers can opt into instead. Implementations satisfy both; FS's methods are
// thin wrappers passing context.Background().
//
// DeleteContext also corrects two things FS.Delete gets wrong and cannot fix
// without breaking callers:
//
//   - It returns an error rather than a bool. After the aws-sdk-go-v2 migration
//     there are three distinct failure modes -- the list failed, the batch call
//     failed, or the response reported per-key errors -- and false discards
//     which one happened.
//   - It honours its items argument. FS.Delete ignores it and empties the whole
//     bucket, inherited from the v1 implementation and preserved deliberately so
//     an SDK migration would not quietly change semantics. A new method is the
//     right place to stop carrying that forward.
type ContextFS interface {
	FS

	PutContext(ctx context.Context, fileName, folder string) error
	GetContext(ctx context.Context, destination string, items ...string) error
	ListContext(ctx context.Context, prefix string) ([]Listing, error)

	// DeleteContext removes the named items. Passing none deletes everything,
	// which is what FS.Delete does for callers that relied on it.
	DeleteContext(ctx context.Context, items []string) error
}

// Listing is a struct that represents a file or directory in a filesystem
type Listing struct {
	Etag         string
	LastModified time.Time
	Key          string
	Size         float64
	IsDir        bool
}
