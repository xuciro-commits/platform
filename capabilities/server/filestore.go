package platformserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// FileStore keeps file bytes outside the journal (ADR-0028 D1): an
// S3-compatible object store in a deployment, memory in development. Keys are
// "<tenant>/<sha256>", so the same bytes are kept once and a key never changes.
type FileStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Exists(ctx context.Context, key string) bool
	Delete(ctx context.Context, key string) error
}

// memoryFiles is the development store: bytes in the process.
type memoryFiles struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memoryFiles) Put(_ context.Context, key string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[key] = bytes.Clone(data)
	return nil
}

func (s *memoryFiles) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	if !ok {
		return nil, 0, fmt.Errorf("file %s not stored", key)
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

func (s *memoryFiles) Exists(_ context.Context, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[key]
	return ok
}

func (s *memoryFiles) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// s3Files is an S3-compatible bucket: RustFS locally, any S3 in production.
type s3Files struct {
	client *minio.Client
	bucket string
}

// NewS3Files connects to endpoint (http(s)://host:port/bucket) with the key
// pair from PLATFORM_S3_ACCESS_KEY and PLATFORM_S3_SECRET_KEY, and makes the
// bucket when it does not exist.
func NewS3Files(ctx context.Context, endpoint string) (FileStore, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return nil, fmt.Errorf("files: %q is not http(s)://host:port/bucket", endpoint)
	}
	client, err := minio.New(u.Host, &minio.Options{Secure: u.Scheme == "https",
		Creds: credentials.NewStaticV4(os.Getenv("PLATFORM_S3_ACCESS_KEY"), os.Getenv("PLATFORM_S3_SECRET_KEY"), "")})
	if err != nil {
		return nil, err
	}
	bucket := strings.Trim(u.Path, "/")
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("files: %v", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("files: %v", err)
		}
	}
	return &s3Files{client: client, bucket: bucket}, nil
}

func (s *s3Files) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *s3Files) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	o, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	info, err := o.Stat()
	if err != nil {
		o.Close()
		return nil, 0, err
	}
	return o, info.Size, nil
}

func (s *s3Files) Exists(ctx context.Context, key string) bool {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	return err == nil
}

func (s *s3Files) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// files is the tenant's file store: the deployment's, or memory.
func (t *Tenant) files() FileStore {
	if t.Files != nil {
		return t.Files
	}
	return &t.memFiles
}
