package platformserver

import (
	"container/heap"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// FileInventory is an optional file-owner capability. Each page is ordered by
// key, strictly after the previous page's last key. Empty Next proves that this
// prefix has no further objects; an unavailable inventory returns an error.
// Inventory does not prove whether a file is referenced by accepted state.
type FileInventory interface {
	Inventory(context.Context, string, string, int) (FileInventoryPage, error)
}

type StoredFile struct {
	Key      string
	Size     int64
	Modified time.Time
}

type FileInventoryPage struct {
	Files []StoredFile
	Next  string
}

const maxFileInventoryPage = 1024

func checkFileInventory(prefix, after string, limit int) error {
	if limit < 1 || limit > maxFileInventoryPage || strings.HasPrefix(prefix, "/") {
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
type inventoryHeap []StoredFile

func (h inventoryHeap) Len() int           { return len(h) }
func (h inventoryHeap) Less(i, j int) bool { return h[i].Key > h[j].Key }
func (h inventoryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *inventoryHeap) Push(value any)    { *h = append(*h, value.(StoredFile)) }
func (h *inventoryHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}
func (h *inventoryHeap) add(value StoredFile, limit int) {
	if h.Len() < limit+1 {
		heap.Push(h, value)
	} else if value.Key < (*h)[0].Key {
		(*h)[0] = value
		heap.Fix(h, 0)
	}
}
func (h inventoryHeap) page(limit int) FileInventoryPage {
	sort.Slice(h, func(i, j int) bool { return h[i].Key < h[j].Key })
	page := FileInventoryPage{Files: []StoredFile(h)}
	if len(h) > limit {
		page.Files = page.Files[:limit]
		page.Next = page.Files[limit-1].Key
	}
	return page
}

func (s *memoryFiles) Inventory(ctx context.Context, prefix, after string, limit int) (FileInventoryPage, error) {
	if err := checkFileInventory(prefix, after, limit); err != nil {
		return FileInventoryPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var found inventoryHeap
	for key, data := range s.m {
		if err := ctx.Err(); err != nil {
			return FileInventoryPage{}, err
		}
		if key <= after || !strings.HasPrefix(key, prefix) {
			continue
		}
		modified := s.modified[key]
		if modified.IsZero() {
			return FileInventoryPage{}, fmt.Errorf("file inventory has no modification time")
		}
		found.add(StoredFile{Key: key, Size: int64(len(data)), Modified: modified}, limit)
	}
	return found.page(limit), nil
}

func (s *localFiles) Inventory(ctx context.Context, prefix, after string, limit int) (FileInventoryPage, error) {
	if err := checkFileInventory(prefix, after, limit); err != nil {
		return FileInventoryPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileInventoryPage{}, err
	}
	var found inventoryHeap
	root := s.dir
	if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
		var err error
		root, err = s.path(prefix[:slash])
		if err != nil {
			return FileInventoryPage{}, err
		}
	}
	// WalkDir does not follow directory links. Verify the store and every
	// prefix ancestor before walking so a skipped link never proves exhaustion.
	info, err := os.Lstat(s.dir)
	if err != nil {
		return FileInventoryPage{}, err
	}
	if !info.IsDir() {
		return FileInventoryPage{}, fmt.Errorf("file inventory store is not a directory")
	}
	relative, err := filepath.Rel(s.dir, root)
	if err != nil {
		return FileInventoryPage{}, err
	}
	current := s.dir
	if relative != "." {
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				return FileInventoryPage{}, nil // the verified parent has no such prefix
			}
			if err != nil {
				return FileInventoryPage{}, err
			}
			if !info.IsDir() {
				return FileInventoryPage{}, fmt.Errorf("file inventory prefix is not a directory")
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
		found.add(StoredFile{Key: key, Size: info.Size(), Modified: info.ModTime().UTC()}, limit)
		return nil
	})
	if err != nil {
		return FileInventoryPage{}, err
	}
	return found.page(limit), nil
}

func (s *s3Files) Inventory(ctx context.Context, prefix, after string, limit int) (FileInventoryPage, error) {
	if err := checkFileInventory(prefix, after, limit); err != nil {
		return FileInventoryPage{}, err
	}
	listing, cancel := context.WithCancel(ctx)
	defer cancel()
	page := FileInventoryPage{}
	previous := after
	for object := range s.client.ListObjects(listing, s.bucket, minio.ListObjectsOptions{Prefix: prefix, StartAfter: after, Recursive: true, MaxKeys: limit + 1}) {
		if object.Err != nil {
			return FileInventoryPage{}, object.Err
		}
		if object.Key <= previous || !strings.HasPrefix(object.Key, prefix) || object.LastModified.IsZero() {
			return FileInventoryPage{}, fmt.Errorf("file inventory returned an invalid object page")
		}
		if len(page.Files) == limit {
			page.Next = previous
			return page, nil
		}
		page.Files = append(page.Files, StoredFile{Key: object.Key, Size: object.Size, Modified: object.LastModified.UTC()})
		previous = object.Key
	}
	if err := ctx.Err(); err != nil {
		return FileInventoryPage{}, err
	}
	return page, nil
}
