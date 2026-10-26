package platformserver

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestFileInventoryPagesKeepActualModificationTimes(t *testing.T) {
	ctx := context.Background()
	local, err := NewLocalFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]FileStore{"memory": &memoryFiles{}, "local": local} {
		t.Run(name, func(t *testing.T) {
			before := time.Now().UTC()
			for _, key := range []string{"tenant/a/file", "tenant/a-other", "tenant/z/file", "other/file"} {
				if err := store.Put(ctx, key, []byte(key), "text/plain"); err != nil {
					t.Fatal(err)
				}
			}
			after := time.Now().UTC()
			if memory, ok := store.(*memoryFiles); ok {
				memory.mu.Lock()
				for _, at := range memory.modified {
					if at.Before(before) || at.After(after) {
						t.Fatal("memory modification time was fabricated", at)
					}
				}
				memory.mu.Unlock()
			}
			inventory := store.(FileInventory)
			var keys []string
			cursor := ""
			for {
				page, err := inventory.Inventory(ctx, "tenant/", cursor, 1)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Files) != 1 {
					t.Fatal("page bound was not respected", page)
				}
				file := page.Files[0]
				if file.Size != int64(len(file.Key)) || file.Modified.IsZero() {
					t.Fatal("inventory metadata differs", file)
				}
				if disk, ok := store.(*localFiles); ok {
					path, _ := disk.path(file.Key)
					info, err := os.Stat(path)
					if err != nil || !file.Modified.Equal(info.ModTime()) {
						t.Fatal("local modification time differs", err)
					}
				}
				keys = append(keys, file.Key)
				if page.Next == "" {
					break
				}
				if page.Next != file.Key {
					t.Fatal("cursor does not identify its last key")
				}
				cursor = page.Next
			}
			if strings.Join(keys, ",") != "tenant/a-other,tenant/a/file,tenant/z/file" {
				t.Fatal("lexical pagination skipped nested keys", keys)
			}
			if _, err := inventory.Inventory(ctx, "../", "", 1); err == nil {
				t.Fatal("foreign prefix accepted")
			}
			if _, err := inventory.Inventory(ctx, "tenant/", "other/file", 1); err == nil {
				t.Fatal("foreign cursor accepted")
			}
		})
	}
}

func TestLocalFileInventoryRefusesIncompleteDirectoryReads(t *testing.T) {
	ctx := context.Background()
	for _, ancestor := range []string{"tenant", "tenant/artifacts", "tenant/artifacts/flow"} {
		t.Run("symlink-"+ancestor, func(t *testing.T) {
			dir, target := t.TempDir(), t.TempDir()
			path := filepath.Join(dir, filepath.FromSlash(ancestor))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if _, err := (&localFiles{dir: dir}).Inventory(ctx, "tenant/artifacts/flow/", "", 1); err == nil {
				t.Fatal("a skipped directory link was reported as an exhausted prefix")
			}
		})
	}
	t.Run("missing-store", func(t *testing.T) {
		if _, err := (&localFiles{dir: filepath.Join(t.TempDir(), "missing")}).Inventory(ctx, "tenant/", "", 1); err == nil {
			t.Fatal("an unavailable store was reported as an exhausted prefix")
		}
	})
	t.Run("unreadable-prefix", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can read directories without permission")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "tenant")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(path, 0o700)
		if _, err := (&localFiles{dir: dir}).Inventory(ctx, "tenant/", "", 1); err == nil {
			t.Fatal("a failed directory read was reported as an exhausted prefix")
		}
	})
	t.Run("absent-prefix", func(t *testing.T) {
		page, err := (&localFiles{dir: t.TempDir()}).Inventory(ctx, "tenant/artifacts/flow/", "", 1)
		if err != nil || len(page.Files) != 0 || page.Next != "" {
			t.Fatal("a verified absent prefix did not prove exhaustion", err)
		}
	})
}

func TestS3FileInventoryUsesBoundedSortedPages(t *testing.T) {
	modified := time.Now().UTC().Truncate(time.Second)
	keys := []string{"tenant/a-other", "tenant/a/file", "tenant/z/file"}
	type content struct {
		Key          string
		LastModified string
		Size         int64
	}
	type result struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		Name                  string
		IsTruncated           bool
		NextContinuationToken string `xml:",omitempty"`
		Contents              []content
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("max-keys"))
		if limit < 1 || limit > maxFileInventoryPage+1 {
			t.Errorf("unbounded S3 list request: %d", limit)
		}
		cursor, prefix := r.URL.Query().Get("start-after"), r.URL.Query().Get("prefix")
		if next := r.URL.Query().Get("continuation-token"); next != "" {
			cursor = next
		}
		answer := result{Name: "files"}
		for _, key := range keys {
			if key > cursor && strings.HasPrefix(key, prefix) {
				answer.Contents = append(answer.Contents, content{Key: key, Size: int64(len(key)), LastModified: modified.Format(time.RFC3339)})
			}
		}
		sort.Slice(answer.Contents, func(i, j int) bool { return answer.Contents[i].Key < answer.Contents[j].Key })
		if len(answer.Contents) > limit {
			answer.Contents = answer.Contents[:limit]
			answer.IsTruncated = true
			answer.NextContinuationToken = answer.Contents[len(answer.Contents)-1].Key
		}
		w.Header().Set("Content-Type", "application/xml")
		if err := xml.NewEncoder(w).Encode(answer); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	store := &s3Files{client: client, bucket: "files"}
	page, err := store.Inventory(context.Background(), "tenant/", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Files) != 1 || page.Files[0].Key != keys[0] || !page.Files[0].Modified.Equal(modified) || page.Next != keys[0] {
		t.Fatal("S3 page differs", page)
	}
	final, err := store.Inventory(context.Background(), "tenant/", keys[1], 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Files) != 1 || final.Files[0].Key != keys[2] || final.Next != "" {
		t.Fatal("S3 final page did not prove exhaustion", final)
	}
}

type unavailableInventoryFiles struct{ *memoryFiles }

func (s unavailableInventoryFiles) Inventory(context.Context, string, string, int) (FileInventoryPage, error) {
	return FileInventoryPage{}, fmt.Errorf("inventory unavailable")
}

// A partial prefix cannot pretend to be an exhausted complete inventory.
type badCursorInventoryFiles struct {
	*memoryFiles
	key string
}

func (s badCursorInventoryFiles) Inventory(context.Context, string, string, int) (FileInventoryPage, error) {
	return FileInventoryPage{Files: []StoredFile{{Key: s.key, Size: 2, Modified: time.Now().Add(-48 * time.Hour)}}, Next: "truncated"}, nil
}
