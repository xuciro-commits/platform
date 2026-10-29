package resources

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"platformserver/journal"
	"platformserver/platform"
	"sort"
	"strings"
	"sync"
	"time"
)

// memoryFiles is the development store: bytes in the process.
type MemoryFiles struct {
	mu       sync.Mutex
	m        map[string][]byte
	modified map[string]time.Time
}

func (s *MemoryFiles) Put(_ context.Context, key string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	if s.modified == nil {
		s.modified = map[string]time.Time{}
	}
	s.m[key] = bytes.Clone(data)
	s.modified[key] = time.Now().UTC()
	return nil
}

func (s *MemoryFiles) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	if !ok {
		return nil, 0, fmt.Errorf("file %s not stored", key)
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

func (s *MemoryFiles) Exists(_ context.Context, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[key]
	return ok
}

func (s *MemoryFiles) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	delete(s.modified, key)
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
func NewS3Files(ctx context.Context, endpoint string) (platform.FileStore, error) {
	return openS3Files(ctx, endpoint, true)
}

// OpenS3Files opens an existing bucket without creating a missing store.
func OpenS3Files(ctx context.Context, endpoint string) (platform.FileStore, error) {
	return openS3Files(ctx, endpoint, false)
}

func openS3Files(ctx context.Context, endpoint string, create bool) (platform.FileStore, error) {
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
		if !create {
			return nil, fmt.Errorf("files: the bucket does not exist")
		}
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

// localFiles keeps file bytes in a directory on the host's own disk: the
// lightweight profile's FileStore. Keys are the same content
// addresses the S3 store takes ("<tenant>/<hash>"), the bytes are the same
// bytes, and the journal still keeps only each file's hash.
type localFiles struct {
	dir string
}

// NewLocalFiles opens (or makes) a directory of file bytes.
func NewLocalFiles(dir string) (platform.FileStore, error) {
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

func checkFileInventory(prefix, after string, limit int) error {
	if limit < 1 || limit > platform.MaxFileInventoryPage || strings.HasPrefix(prefix, "/") {
		return fmt.Errorf("file inventory has an invalid prefix or page bound")
	}
	for _, part := range strings.Split(strings.TrimSuffix(prefix, "/"), "/") {
		if part == "." || part == ".." || part == "" && prefix != "" {
			return fmt.Errorf("file inventory has an invalid prefix")
		}
	}
	if after != "" && (!strings.HasPrefix(after, prefix) || strings.HasSuffix(after, "/")) {
		return fmt.Errorf("file inventory cursor belongs to another prefix")
	}
	return nil
}

// Keep only the smallest limit+1 keys while walking a memory or local store.
// Reading a large prefix never allocates an unbounded second file index.
type inventoryHeap []platform.StoredFile

func (h inventoryHeap) Len() int           { return len(h) }
func (h inventoryHeap) Less(i, j int) bool { return h[i].Key > h[j].Key }
func (h inventoryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *inventoryHeap) Push(value any)    { *h = append(*h, value.(platform.StoredFile)) }
func (h *inventoryHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}
func (h *inventoryHeap) add(value platform.StoredFile, limit int) {
	if h.Len() < limit+1 {
		heap.Push(h, value)
	} else if value.Key < (*h)[0].Key {
		(*h)[0] = value
		heap.Fix(h, 0)
	}
}
func (h inventoryHeap) page(limit int) platform.FileInventoryPage {
	sort.Slice(h, func(i, j int) bool { return h[i].Key < h[j].Key })
	page := platform.FileInventoryPage{Files: []platform.StoredFile(h)}
	if len(h) > limit {
		page.Files = page.Files[:limit]
		page.Next = page.Files[limit-1].Key
	}
	return page
}

func (s *MemoryFiles) Inventory(ctx context.Context, prefix, after string, limit int) (platform.FileInventoryPage, error) {
	if err := checkFileInventory(prefix, after, limit); err != nil {
		return platform.FileInventoryPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var found inventoryHeap
	for key, data := range s.m {
		if err := ctx.Err(); err != nil {
			return platform.FileInventoryPage{}, err
		}
		if key <= after || !strings.HasPrefix(key, prefix) {
			continue
		}
		modified := s.modified[key]
		if modified.IsZero() {
			return platform.FileInventoryPage{}, fmt.Errorf("file inventory has no modification time")
		}
		found.add(platform.StoredFile{Key: key, Size: int64(len(data)), Modified: modified}, limit)
	}
	return found.page(limit), nil
}

func (s *localFiles) Inventory(ctx context.Context, prefix, after string, limit int) (platform.FileInventoryPage, error) {
	if err := checkFileInventory(prefix, after, limit); err != nil {
		return platform.FileInventoryPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return platform.FileInventoryPage{}, err
	}
	var found inventoryHeap
	root := s.dir
	if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
		var err error
		root, err = s.path(prefix[:slash])
		if err != nil {
			return platform.FileInventoryPage{}, err
		}
	}
	// WalkDir does not follow directory links. Verify the store and every
	// prefix ancestor before walking so a skipped link never proves exhaustion.
	info, err := os.Lstat(s.dir)
	if err != nil {
		return platform.FileInventoryPage{}, err
	}
	if !info.IsDir() {
		return platform.FileInventoryPage{}, fmt.Errorf("file inventory store is not a directory")
	}
	relative, err := filepath.Rel(s.dir, root)
	if err != nil {
		return platform.FileInventoryPage{}, err
	}
	current := s.dir
	if relative != "." {
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				return platform.FileInventoryPage{}, nil // the verified parent has no such prefix
			}
			if err != nil {
				return platform.FileInventoryPage{}, err
			}
			if !info.IsDir() {
				return platform.FileInventoryPage{}, fmt.Errorf("file inventory prefix is not a directory")
			}
		}
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		key, err := filepath.Rel(s.dir, path)
		if err != nil {
			return err
		}
		key = filepath.ToSlash(key)
		if key <= after || !strings.HasPrefix(key, prefix) {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("file inventory encountered a symbolic link")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("file inventory encountered a non-file object")
		}
		found.add(platform.StoredFile{Key: key, Size: info.Size(), Modified: info.ModTime().UTC()}, limit)
		return nil
	})
	if err != nil {
		return platform.FileInventoryPage{}, err
	}
	return found.page(limit), nil
}

func (s *s3Files) Inventory(ctx context.Context, prefix, after string, limit int) (platform.FileInventoryPage, error) {
	if err := checkFileInventory(prefix, after, limit); err != nil {
		return platform.FileInventoryPage{}, err
	}
	listing, cancel := context.WithCancel(ctx)
	defer cancel()
	page := platform.FileInventoryPage{}
	previous := after
	for object := range s.client.ListObjects(listing, s.bucket, minio.ListObjectsOptions{Prefix: prefix, StartAfter: after, Recursive: true, MaxKeys: limit + 1}) {
		if object.Err != nil {
			return platform.FileInventoryPage{}, object.Err
		}
		if object.Key <= previous || !strings.HasPrefix(object.Key, prefix) || object.LastModified.IsZero() {
			return platform.FileInventoryPage{}, fmt.Errorf("file inventory returned an invalid object page")
		}
		if len(page.Files) == limit {
			page.Next = previous
			return page, nil
		}
		page.Files = append(page.Files, platform.StoredFile{Key: object.Key, Size: object.Size, Modified: object.LastModified.UTC()})
		previous = object.Key
	}
	if err := ctx.Err(); err != nil {
		return platform.FileInventoryPage{}, err
	}
	return page, nil
}

// FileStoreManifest verifies actual bytes, not object-store ETags or timestamps.
// The deployment owner must stop all writers to these prefixes before calling.
func FileStoreManifest(ctx context.Context, store platform.FileStore) (map[string]string, error) {
	inventory, ok := store.(platform.FileInventory)
	if !ok {
		return nil, fmt.Errorf("file store does not support a complete inventory")
	}
	result := map[string]string{}
	after := ""
	for {
		page, err := inventory.Inventory(ctx, "", after, 1024)
		if err != nil {
			return nil, err
		}
		previous := after
		for _, file := range page.Files {
			if file.Key <= previous {
				return nil, fmt.Errorf("file inventory is not strictly ordered")
			}
			body, size, err := store.Get(ctx, file.Key)
			if err != nil {
				return nil, err
			}
			hash := sha256.New()
			count, readErr := io.Copy(hash, body)
			closeErr := body.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if count != size || count != file.Size {
				return nil, fmt.Errorf("file %s changed during handover", file.Key)
			}
			result[file.Key] = hex.EncodeToString(hash.Sum(nil))
			previous = file.Key
		}
		if page.Next == "" {
			return result, nil
		}
		if page.Next != previous || page.Next <= after {
			return nil, fmt.Errorf("file inventory cannot prove complete traversal")
		}
		after = page.Next
	}
}

// uploads are the file hashes uploaded and when, until a record attaches them
// or the sweep removes them (volatile).
type Uploads struct {
	mu sync.Mutex
	at map[string]time.Time
}

func (u *Uploads) Add(hash string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.at == nil {
		u.at = map[string]time.Time{}
	}
	u.at[hash] = now
}

// olderThan are the hashes uploaded before now minus age.
func (u *Uploads) OlderThan(now time.Time, age time.Duration) []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	var old []string
	for hash, at := range u.at {
		if now.Sub(at) > age {
			old = append(old, hash)
		}
	}
	return old
}

func (u *Uploads) Forget(hash string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.at, hash)
}
