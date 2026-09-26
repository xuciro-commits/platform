package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"platformserver/apps/files"
	"platformserver/platform"
)

// Files (ADR-0028): bytes are uploaded to the tenant's store and answered with
// their SHA-256; a decision of the files app attaches them to a record. Bytes
// no decision attached within a day are removed.

// Upload is what an upload answers: the hash to attach.
type Upload struct {
	Hash        string `json:"hash"`
	Size        int    `json:"size"`
	ContentType string `json:"contentType"`
	Name        string `json:"name"`
}

// Upload keeps the bytes the member sent, when the tenant runs the files app.
func (t *Tenant) Upload(m platform.Member, name, contentType string, body io.Reader, now time.Time) (Upload, int, error) {
	if t.app(files.ID) == nil {
		return Upload{}, http.StatusNotFound, fmt.Errorf("the tenant runs no files app")
	}
	limit := files.Max(t.automation(files.ID, false))
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return Upload{}, http.StatusBadRequest, err
	}
	if len(data) > limit {
		return Upload{}, http.StatusRequestEntityTooLarge, fmt.Errorf("larger than %d MB", limit>>20)
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if err := t.files().Put(context.Background(), t.ID+"/"+hash, data, contentType); err != nil {
		return Upload{}, http.StatusServiceUnavailable, err
	}
	t.opsMu.Lock()
	if t.uploads == nil {
		t.uploads = map[string]time.Time{}
	}
	t.uploads[hash] = now
	t.opsMu.Unlock()
	return Upload{Hash: hash, Size: len(data), ContentType: contentType, Name: name}, http.StatusOK, nil
}

// SweepUploads removes bytes uploaded more than a day ago that no file attaches.
func (t *Tenant) SweepUploads(now time.Time) {
	t.opsMu.Lock()
	var old []string
	for hash, at := range t.uploads {
		if now.Sub(at) > 24*time.Hour {
			old = append(old, hash)
		}
	}
	t.opsMu.Unlock()
	if len(old) == 0 {
		return
	}
	c := t.automation(files.ID, false)
	attached, _, _ := platform.Find[files.File](c, platform.Query{Limit: 100000, Archived: true})
	for _, hash := range old {
		if !slices.ContainsFunc(attached, func(f files.File) bool { return f.Hash == hash }) {
			t.files().Delete(context.Background(), t.ID+"/"+hash)
		}
		t.opsMu.Lock()
		delete(t.uploads, hash)
		t.opsMu.Unlock()
	}
}

// Download writes a file m may read, as an attachment the browser never renders in place.
func (t *Tenant) Download(w http.ResponseWriter, m platform.Member, id string, now time.Time) {
	v, err := t.RecordOf(m, files.FileType, id, now)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f := v.Record.(files.File)
	body, size, gerr := t.files().Get(context.Background(), t.ID+"/"+f.Hash)
	if gerr != nil {
		http.Error(w, "the file's bytes are not in the store", http.StatusServiceUnavailable)
		return
	}
	defer body.Close()
	kind := f.ContentType
	if _, _, perr := mime.ParseMediaType(kind); perr != nil || kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(strings.ReplaceAll(f.Name, "\"", "")))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	io.Copy(w, body)
}
