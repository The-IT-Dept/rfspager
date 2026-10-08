// Package blobstore keeps opaque objects (recorded radio audio today) in
// either a local directory or an S3-compatible bucket such as Cloudflare R2.
// Objects are private: callers serve them through the API rather than
// handing out storage URLs.
package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// ErrNotFound is returned by Open for a key that has no object.
var ErrNotFound = errors.New("blob not found")

// Store is somewhere to keep objects by key. Keys are slash-separated
// relative paths ("radio/2026/10/08/123.wav").
type Store interface {
	// Put stores size bytes from r under key, replacing any object already
	// there. The returned URL identifies where it went (file:// or https://)
	// for logs; it is not a public link.
	Put(ctx context.Context, key, contentType string, r io.Reader, size int64) (url string, err error)
	// Open returns the object for reading. The caller must Close it.
	Open(ctx context.Context, key string) (*Object, error)
}

// Object is an open stored object. It is an io.ReadSeeker so it can be
// handed straight to http.ServeContent, which answers Range requests by
// seeking.
type Object struct {
	io.ReadSeekCloser
	Size        int64
	ContentType string // may be empty if the store doesn't record it
	ModTime     time.Time
}

// ValidKey reports whether key is a clean relative path: no leading slash,
// no empty, "." or ".." segments, no backslashes. Both stores require it, so
// a key can never escape the local directory or address another bucket.
func ValidKey(key string) error {
	if key == "" || len(key) > 512 {
		return fmt.Errorf("blobstore: invalid key length")
	}
	if strings.ContainsAny(key, "\\\x00") || strings.HasPrefix(key, "/") || path.Clean(key) != key {
		return fmt.Errorf("blobstore: invalid key %q", key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("blobstore: invalid key %q", key)
		}
	}
	return nil
}
