package admin

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jimmitjoo/tjo/filesystems"
)

// Uploader stores a file from a file field and returns the value to write into
// the column.
//
// What that value means is the application's business -- a key, a path, a URL.
// This package writes back whatever it is given.
type Uploader interface {
	Upload(ctx Context, field string, file multipart.File, header *multipart.FileHeader) (string, error)
}

// UploaderFunc adapts a function.
type UploaderFunc func(ctx Context, field string, file multipart.File, header *multipart.FileHeader) (string, error)

func (f UploaderFunc) Upload(ctx Context, field string, file multipart.File, header *multipart.FileHeader) (string, error) {
	return f(ctx, field, file, header)
}

// MaxUploadBytes bounds a single file. Without a cap, an upload field is a way
// to fill a disk from a browser.
const MaxUploadBytes = 32 << 20 // 32 MiB

// FileStore stores uploads through the framework's filesystems package, so a
// panel gets S3 or MinIO by being handed the filesystem the application
// already configured.
type FileStore struct {
	// FS is the destination.
	FS filesystems.FS

	// Folder is the prefix within it.
	Folder string

	// MaxBytes overrides MaxUploadBytes.
	MaxBytes int64

	// AllowedExtensions restricts what may be uploaded, lower case and with
	// the dot: {".png", ".jpg"}. Empty allows anything, which is a decision to
	// make deliberately -- an admin panel that accepts .html is a way to host
	// an attacker's page on your origin, and one that accepts .svg is the same
	// thing with a friendlier extension.
	AllowedExtensions []string
}

// Upload writes the file and returns the path it was stored under.
func (s FileStore) Upload(ctx Context, field string, file multipart.File, header *multipart.FileHeader) (string, error) {
	if s.FS == nil {
		return "", errors.New("admin: no filesystem configured for uploads")
	}

	limit := s.MaxBytes
	if limit <= 0 {
		limit = MaxUploadBytes
	}
	if header.Size > limit {
		return "", fmt.Errorf("admin: %s is larger than the %d byte limit", header.Filename, limit)
	}

	name := safeFilename(header.Filename)
	if name == "" {
		return "", errors.New("admin: the uploaded file has no usable name")
	}

	if len(s.AllowedExtensions) > 0 {
		ext := strings.ToLower(filepath.Ext(name))
		if !contains(s.AllowedExtensions, ext) {
			return "", fmt.Errorf("admin: %s files are not accepted here", ext)
		}
	}

	// filesystems.FS.Put takes a path on disk rather than a reader, so the
	// upload is staged in a temporary file. It is removed on the way out
	// whether the store succeeded or not.
	staged, err := os.CreateTemp("", "tjo-admin-upload-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()

	if _, err := io.Copy(staged, io.LimitReader(file, limit+1)); err != nil {
		return "", err
	}
	if err := staged.Close(); err != nil {
		return "", err
	}

	// The staged file keeps its temporary name, and Put uses the file's own
	// name as the key -- so it is renamed first, or every upload lands under a
	// name nothing can find again.
	final := filepath.Join(filepath.Dir(staged.Name()), name)
	if err := os.Rename(staged.Name(), final); err != nil {
		return "", err
	}
	defer os.Remove(final)

	if err := s.FS.Put(final, s.Folder); err != nil {
		return "", err
	}

	return path.Join(s.Folder, name), nil
}

// safeFilename strips everything that could make a name mean a path.
//
// A multipart filename is attacker-controlled: "../../etc/cron.d/x" is a valid
// one to send, and a store that joins it onto a folder writes where it was
// told to.
func safeFilename(name string) string {
	// Both separators, on every platform. filepath.Base only knows the local
	// one, so a Windows path uploaded to a Linux server keeps its backslashes
	// and the "directory" ends up as part of the filename.
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = strings.TrimSpace(name)

	if name == "." || name == ".." || name == string(filepath.Separator) {
		return ""
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	out := strings.Trim(b.String(), "-.")
	if len(out) > 128 {
		out = out[len(out)-128:]
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
