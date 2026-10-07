package platformserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"platformserver/journal"
	"strings"
)

// localFiles keeps file bytes in a directory on the host's own disk: the
// lightweight profile's FileStore (ADR-0049 D4). Keys are the same content
// addresses the S3 store takes ("<tenant>/<hash>"), the bytes are the same
// bytes, and the journal still keeps only each file's hash.
type localFiles struct {
	dir string
}

// NewLocalFiles opens (or makes) a directory of file bytes.
func NewLocalFiles(dir string) (FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("files: %w", err)
	}
	return &localFiles{dir: dir}, nil
}

// path is where a key's bytes live. A key names nested parts of the store and
// nothing else: a key that would leave the directory is refused, not cleaned up.
func (s *localFiles) path(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") {
		return "", fmt.Errorf("files: %q is not a key", key)
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("files: %q is not a key", key)
		}
	}
	return filepath.Join(s.dir, filepath.FromSlash(key)), nil
}

func (s *localFiles) Put(_ context.Context, key string, data []byte, _ string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("files: %w", err)
	}
	// A reader sees the old bytes or the new ones, never half a file; the same
	// bytes under the same key are the same file (content addressing), so
	// rewriting one is not an error.
	if _, err := os.Stat(path); err == nil {
		current, err := os.ReadFile(path)
		if err == nil && bytes.Equal(current, data) {
			return nil
		}
	}
	return journal.WriteFileAtomic(path, data)
}

func (s *localFiles) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("files: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("files: %w", err)
	}
	return file, info.Size(), nil
}

func (s *localFiles) Exists(_ context.Context, key string) bool {
	path, err := s.path(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func (s *localFiles) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("files: %w", err)
	}
	return nil
}
