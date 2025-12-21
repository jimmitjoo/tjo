# Extending Tjo

Tjo is designed for extensibility through Go interfaces. This guide shows how to create custom implementations for each extension point.

## Extension Points Overview

| Interface | Package | Purpose |
|-----------|---------|---------|
| `Cache` | `cache` | Custom caching backends |
| `FS` | `filesystems` | Custom file storage |
| `SMSProvider` | `sms` | Custom SMS providers |
| `JobHandler` | `jobs` | Custom background job handlers |
| `Seeder` | `database` | Custom database seeders |
| `RateLimiter` | `api` | Custom rate limiting strategies |

---

## Custom Cache Implementation

Implement the `cache.Cache` interface to add a new caching backend.

```go
package mycache

import "github.com/jimmitjoo/tjo/cache"

type MyCache struct {
    // Your cache connection
}

func (c *MyCache) Has(key string) (bool, error) {
    // Return true if key exists
}

func (c *MyCache) Get(key string) (interface{}, error) {
    // Return cached value
}

func (c *MyCache) Set(key string, value interface{}, ttl ...int) error {
    // Store value with optional TTL in seconds
}

func (c *MyCache) Forget(key string) error {
    // Delete a single key
}

func (c *MyCache) EmptyByMatch(pattern string) error {
    // Delete keys matching pattern
}

func (c *MyCache) Flush() error {
    // Clear entire cache
}
```

**Usage:**
```go
app := tjo.New()
app.Data.Cache = &mycache.MyCache{}
```

---

## Custom Filesystem

Implement the `filesystems.FS` interface for custom file storage.

```go
package mystorage

import (
    "github.com/jimmitjoo/tjo/filesystems"
)

type MyStorage struct {
    // Your storage connection
}

func (s *MyStorage) Put(fileName, folder string) error {
    // Upload file to storage
}

func (s *MyStorage) Get(destination string, items ...string) error {
    // Download files to destination
}

func (s *MyStorage) List(prefix string) ([]filesystems.Listing, error) {
    // List files with given prefix
}

func (s *MyStorage) Delete(items []string) bool {
    // Delete files, return success
}
```

**Usage:**
```go
app := tjo.New()
app.Data.Files.Register("mystorage", &mystorage.MyStorage{})

// Access later
fs, ok := app.Data.Files.Get("mystorage")
```

---

## Custom SMS Provider

Implement the `sms.SMSProvider` interface for custom SMS providers.

```go
package mysms

type MyProvider struct {
    APIKey string
}

func (p *MyProvider) Send(to string, message string, unicode bool) error {
    // Send SMS via your provider
}
```

**Usage:**
```go
app := tjo.New()
app.Background.SMS = &mysms.MyProvider{
    APIKey: os.Getenv("MY_SMS_API_KEY"),
}
```

---

## Custom Job Handler

Implement the `jobs.JobHandler` interface for background job processing.

```go
package myjobs

import (
    "context"
    "github.com/jimmitjoo/tjo/jobs"
)

type EmailHandler struct {
    // Dependencies
}

func (h *EmailHandler) Handle(ctx context.Context, job *jobs.Job) error {
    // Process the job
    email := job.Payload["email"].(string)
    subject := job.Payload["subject"].(string)

    // Send email...
    return nil
}
```

**Usage:**
```go
app := tjo.New()
app.Background.Jobs.RegisterHandler("send_email", &myjobs.EmailHandler{})

// Or use a function:
app.Background.Jobs.RegisterHandlerFunc("notify_user", func(ctx context.Context, job *jobs.Job) error {
    // Handle job
    return nil
})
```

**Dispatching jobs:**
```go
job := jobs.NewJob("send_email", map[string]interface{}{
    "email":   "user@example.com",
    "subject": "Welcome!",
})
app.Background.Jobs.Dispatch(job)
```

---

## Custom Database Seeder

Implement the `database.Seeder` interface for database seeding.

```go
package seeders

import "database/sql"

type UserSeeder struct{}

func (s *UserSeeder) Run(db *sql.DB) error {
    _, err := db.Exec(`
        INSERT INTO users (email, name) VALUES
        ('admin@example.com', 'Admin'),
        ('user@example.com', 'User')
    `)
    return err
}
```

**Usage:**
```go
import "github.com/jimmitjoo/tjo/database"

registry := database.NewSeederRegistry()
registry.Register("users", &seeders.UserSeeder{})

// Run all seeders
registry.RunAll(db)

// Or run specific seeder
registry.Run(db, "users")
```

---

## Custom Rate Limiter

Implement the `api.RateLimiter` interface for custom rate limiting.

```go
package mylimiter

import (
    "time"
    "github.com/jimmitjoo/tjo/api"
)

type RedisLimiter struct {
    // Redis connection
}

func (l *RedisLimiter) Allow(key string) (bool, *api.RateLimitInfo) {
    // Check if request is allowed
    return true, &api.RateLimitInfo{
        Limit:     100,
        Remaining: 99,
        Reset:     time.Now().Add(time.Minute),
    }
}

func (l *RedisLimiter) Reset(key string) {
    // Reset rate limit for key
}
```

**Usage:**
```go
apiHandler := api.New(&api.APIConfig{
    Version: "v1",
})
apiHandler.RateLimiter = &mylimiter.RedisLimiter{}
```

---

## Built-in Implementations

Tjo includes these implementations out of the box:

| Interface | Implementations |
|-----------|----------------|
| Cache | `RedisCache`, `BadgerCache` |
| FS | `S3`, `MinIO` |
| SMSProvider | `Vonage`, `Twilio` |
| RateLimiter | `TokenBucket` |

---

## Best Practices

1. **Use interfaces for testing** - Create mock implementations for unit tests
2. **Handle errors gracefully** - Return meaningful errors from your implementations
3. **Follow naming conventions** - Use descriptive names that match the interface purpose
4. **Document your extensions** - Add godoc comments to exported types and methods
