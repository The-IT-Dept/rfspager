package blobstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
)

// Local keeps objects as files under a directory. Content types are not
// recorded; Open infers one from the key's extension.
type Local struct {
	dir string
}

// NewLocal uses dir (created if missing) as the root of the store.
func NewLocal(dir string) (*Local, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("blobstore: create %s: %w", abs, err)
	}
	return &Local{dir: abs}, nil
}

func (l *Local) path(key string) (string, error) {
	if err := ValidKey(key); err != nil {
		return "", err
	}
	return filepath.Join(l.dir, filepath.FromSlash(key)), nil
}

// Put writes to a temporary file beside the target and renames it into
// place, so a reader never sees a half-written object.
func (l *Local) Put(_ context.Context, key, _ string, r io.Reader, size int64) (string, error) {
	p, err := l.path(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	n, err := io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if size >= 0 && n != size {
		return "", fmt.Errorf("blobstore: wrote %d bytes, expected %d", n, size)
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return "", err
	}
	return "file://" + filepath.ToSlash(p), nil
}

func (l *Local) Open(_ context.Context, key string) (*Object, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, ErrNotFound
	}
	return &Object{
		ReadSeekCloser: f,
		Size:           st.Size(),
		ContentType:    mime.TypeByExtension(filepath.Ext(p)),
		ModTime:        st.ModTime(),
	}, nil
}
