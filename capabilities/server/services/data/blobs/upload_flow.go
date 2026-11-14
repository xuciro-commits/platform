package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type Received struct {
	Hash        string
	Size        int
	ContentType string
}

func Receive(uploads *Uploads, files platform.FileStore, tenant string, limit int, contentType string, body io.Reader, now time.Time) (Received, int, error) {
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return Received{}, http.StatusBadRequest, err
	}
	if len(data) > limit {
		return Received{}, http.StatusRequestEntityTooLarge, fmt.Errorf("larger than %d MB", limit>>20)
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if !uploads.BeginPut(hash) {
		return Received{}, http.StatusConflict, fmt.Errorf("the previous unattached upload is being removed; retry the upload")
	}
	stored := false
	defer func() { uploads.EndPut(hash, now, stored) }()
	if err := files.Put(context.Background(), tenant+"/"+hash, data, contentType); err != nil {
		return Received{}, http.StatusServiceUnavailable, err
	}
	stored = true
	return Received{Hash: hash, Size: len(data), ContentType: contentType}, http.StatusOK, nil
}

type Retention struct {
	File  func() (platform.Caller, bool)
	Build func() (platform.Caller, bool)
}

func (r Retention) Hashes() (map[string]bool, *kernel.Error) {
	retained := map[string]bool{}
	if caller, ok := r.File(); ok {
		if !retainHashes[platform.File](caller, func(file platform.File) { retained[file.Hash] = true }) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}
	if caller, ok := r.Build(); ok {
		keep := func(items []platform.MediaItem) {
			for _, item := range items {
				retained[item.Hash] = true
			}
		}
		if !retainHashes[platform.MediaSet](caller, func(set platform.MediaSet) { keep(set.Items) }) || !retainHashes[platform.MediaSetVersion](caller, func(version platform.MediaSetVersion) { keep(version.Items) }) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}
	return retained, nil
}

func (u *Uploads) ReserveDeletes(old []string, retained map[string]bool, now time.Time) map[string]bool {
	pending := map[string]bool{}
	for _, hash := range old {
		if retained[hash] {
			u.Forget(hash)
		} else if u.BeginDelete(hash, now, 24*time.Hour) {
			pending[hash] = true
		}
	}
	return pending
}

func retainHashes[T any](caller platform.Caller, keep func(T)) bool {
	for offset := 0; ; {
		values, total, err := platform.Find[T](caller, platform.Query{Offset: offset, Limit: 1000, Archived: true, Sort: []string{"id"}})
		if err != nil {
			return false
		}
		for _, value := range values {
			keep(value)
		}
		offset += len(values)
		if offset >= total {
			return true
		}
		if len(values) == 0 {
			return false
		}
	}
}

func Stream(w http.ResponseWriter, files platform.FileStore, key, name, contentType string) {
	body, size, gerr := files.Get(context.Background(), key)
	if gerr != nil {
		http.Error(w, "the file's bytes are not in the store", http.StatusServiceUnavailable)
		return
	}
	defer body.Close()
	kind := contentType
	if _, _, perr := mime.ParseMediaType(kind); perr != nil || kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(strings.ReplaceAll(name, "\"", "")))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	io.Copy(w, body)
}

func MediaItem(set platform.MediaSet, versions []platform.MediaSetVersion, version, hash string) (platform.MediaItem, *kernel.Error) {
	items := set.Items
	if version != "" {
		number, err := strconv.Atoi(version)
		if err != nil || number < 1 {
			return platform.MediaItem{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		if number != set.Version {
			items = nil
		}
		for _, snapshot := range versions {
			if snapshot.Version == number {
				items = snapshot.Items
			}
		}
	} else {
		for _, snapshot := range versions {
			items = append(items, snapshot.Items...)
		}
	}
	for _, item := range items {
		if item.Hash == hash {
			return item, nil
		}
	}
	return platform.MediaItem{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (u *Uploads) Purge(files platform.FileStore, tenant string, pending map[string]bool) {
	for hash := range pending {
		err := files.Delete(context.Background(), tenant+"/"+hash)
		u.EndDelete(hash, err == nil)
	}
}

func RefusalStatus(refusal *kernel.Error) int {
	if refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return http.StatusNotFound
	}
	return http.StatusServiceUnavailable
}
