package blobs

import (
	"bytes"
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
	"platformserver/platform"
)

type Received struct {
	Hash        string
	Size        int
	ContentType string
}

func detectSafeUploadType(declared string, data []byte) (string, int, error) {
	head := bytes.TrimSpace(data[:min(len(data), 1024)])
	lowerHead := bytes.ToLower(head)
	looksActiveWeb := bytes.HasPrefix(lowerHead, []byte("<svg")) ||
		bytes.HasPrefix(lowerHead, []byte("<!doctype html")) ||
		bytes.HasPrefix(lowerHead, []byte("<html")) ||
		bytes.HasPrefix(lowerHead, []byte("<script")) ||
		(bytes.HasPrefix(lowerHead, []byte("<?xml")) && bytes.Contains(lowerHead, []byte("<svg")))
	mediaType := ""
	if strings.TrimSpace(declared) != "" {
		parsed, _, err := mime.ParseMediaType(declared)
		if err != nil {
			return "", http.StatusUnsupportedMediaType, fmt.Errorf("unsupported media type")
		}
		mediaType = strings.ToLower(parsed)
	}
	if mediaType == "image/svg+xml" || mediaType == "text/html" || mediaType == "application/xhtml+xml" || looksActiveWeb {
		return "", http.StatusUnsupportedMediaType, fmt.Errorf("active HTML and SVG uploads are refused")
	}
	actual := detectAvatarType(data)
	switch {
	case actual == "" && len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		actual = "image/gif"
	case actual == "" && len(data) >= 5 && bytes.Equal(data[:5], []byte("%PDF-")):
		actual = "application/pdf"
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "image/avif", "application/pdf":
		if actual != mediaType {
			return "", http.StatusUnsupportedMediaType, fmt.Errorf("file content does not match declared %s", mediaType)
		}
		return mediaType, 0, nil
	case "":
		if actual != "" {
			return actual, 0, nil
		}
		sniffed, _, _ := mime.ParseMediaType(http.DetectContentType(data))
		if sniffed == "text/html" || sniffed == "text/xml" || sniffed == "application/xml" {
			return "application/octet-stream", 0, nil
		}
		return sniffed, 0, nil
	case "application/javascript", "text/javascript", "text/xml", "application/xml":
		return "application/octet-stream", 0, nil
	default:
		return mediaType, 0, nil
	}
}

func Receive(uploads *Uploads, files platform.FileStore, tenant string, limit int, contentType string, body io.Reader, now time.Time) (Received, int, error) {
	if limit <= 0 {
		return Received{}, http.StatusBadRequest, fmt.Errorf("invalid upload limit")
	}
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return Received{}, http.StatusBadRequest, err
	}
	if len(data) == 0 {
		return Received{}, http.StatusBadRequest, fmt.Errorf("empty file")
	}
	if len(data) > limit {
		return Received{}, http.StatusRequestEntityTooLarge, fmt.Errorf("larger than %d MB", limit>>20)
	}
	checkedType, status, typeErr := detectSafeUploadType(contentType, data)
	if typeErr != nil {
		return Received{}, status, typeErr
	}
	contentType = checkedType
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

func (r Retention) Hashes() (map[string]bool, *platform.Error) {
	retained := map[string]bool{}
	if caller, ok := r.File(); ok {
		if !retainHashes[platform.File](caller, func(file platform.File) { retained[file.Hash] = true }) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}
	if caller, ok := r.Build(); ok {
		keep := func(items []platform.MediaItem) {
			for _, item := range items {
				retained[item.Hash] = true
			}
		}
		if !retainHashes[platform.MediaSet](caller, func(set platform.MediaSet) { keep(set.Items) }) || !retainHashes[platform.MediaSetVersion](caller, func(version platform.MediaSetVersion) { keep(version.Items) }) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
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
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	io.Copy(w, body)
}

func MediaItem(set platform.MediaSet, versions []platform.MediaSetVersion, version, hash string) (platform.MediaItem, *platform.Error) {
	items := set.Items
	if version != "" {
		number, err := strconv.Atoi(version)
		if err != nil || number < 1 {
			return platform.MediaItem{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
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
	return platform.MediaItem{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (u *Uploads) Purge(files platform.FileStore, tenant string, pending map[string]bool) {
	for hash := range pending {
		err := files.Delete(context.Background(), tenant+"/"+hash)
		u.EndDelete(hash, err == nil)
	}
}

func StreamMediaItem(w http.ResponseWriter, files platform.FileStore, tenant, id, version, hash string, set platform.MediaSet, caller platform.Caller, outside func(http.ResponseWriter, func(http.ResponseWriter))) *platform.Error {
	versions, _, refusal := platform.Find[platform.MediaSetVersion](caller, platform.Query{Domain: platform.Raw([][]any{{"mediaSet", "=", id}}), Sort: []string{"version"}, Limit: 1000})
	if refusal != nil {
		return refusal
	}
	item, refusal := MediaItem(set, versions, version, hash)
	if refusal != nil {
		return refusal
	}
	outside(w, func(w http.ResponseWriter) { Stream(w, files, tenant+"/"+item.Hash, item.Name, item.ContentType) })
	return nil
}

func RefusalStatus(refusal *platform.Error) int {
	if refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return http.StatusNotFound
	}
	return http.StatusServiceUnavailable
}
