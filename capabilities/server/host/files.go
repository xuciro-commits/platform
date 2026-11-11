package host

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

type Upload struct {
	Hash        string `json:"hash"`
	Size        int    `json:"size"`
	ContentType string `json:"contentType"`
	Name        string `json:"name"`
}

func (t *Tenant) Upload(m platform.Member, name, contentType string, body io.Reader, now time.Time) (Upload, int, error) {
	limit, refusal := recordRead(t, context.Background(), func(t *Tenant) (int, *kernel.Error) {
		if t.App(platform.FileApp) == nil {
			return 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return platform.MaxFileSize(t.automation(platform.FileApp, false)), nil
	})
	if refusal != nil {
		status := http.StatusServiceUnavailable
		if refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			status = http.StatusNotFound
		}
		return Upload{}, status, refusal
	}
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
	if !t.uploads.BeginPut(hash) {
		return Upload{}, http.StatusConflict, fmt.Errorf("the previous unattached upload is being removed; retry the upload")
	}
	stored := false
	defer func() { t.uploads.EndPut(hash, now, stored) }()
	if err := t.files().Put(context.Background(), t.ID+"/"+hash, data, contentType); err != nil {
		return Upload{}, http.StatusServiceUnavailable, err
	}
	stored = true
	return Upload{Hash: hash, Size: len(data), ContentType: contentType, Name: name}, http.StatusOK, nil
}

func (t *Tenant) SweepUploads(now time.Time) {
	old := t.uploads.OlderThan(now, 24*time.Hour)
	if len(old) == 0 {
		return
	}
	pending, refusal := decideOnImage(t, context.Background(), "uploads:reserve", func(t *Tenant) (map[string]bool, *kernel.Error) {
		retained := map[string]bool{}
		if t.App(platform.FileApp) != nil {
			if !retainUploadHashes[platform.File](t.automation(platform.FileApp, false), func(file platform.File) { retained[file.Hash] = true }) {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
		}
		if t.App("build") != nil {
			keep := func(items []platform.MediaItem) {
				for _, item := range items {
					retained[item.Hash] = true
				}
			}
			caller := t.automation("build", false)
			if !retainUploadHashes[platform.MediaSet](caller, func(set platform.MediaSet) { keep(set.Items) }) || !retainUploadHashes[platform.MediaSetVersion](caller, func(version platform.MediaSetVersion) { keep(version.Items) }) {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
		}
		if t.readOrigin == nil {
			return t.reserveUploadDeletes(old, retained, now), nil
		}
		return retained, nil
	}, func(t *Tenant, _ preparedAccepted, retained map[string]bool, refusal *kernel.Error) (map[string]bool, *kernel.Error) {
		if refusal != nil {
			return nil, refusal
		}
		return t.reserveUploadDeletes(old, retained, now), nil
	})
	if refusal != nil {
		return
	}
	for hash := range pending {
		err := t.files().Delete(context.Background(), t.ID+"/"+hash)
		t.uploads.EndDelete(hash, err == nil)
	}
}

func (t *Tenant) reserveUploadDeletes(old []string, retained map[string]bool, now time.Time) map[string]bool {
	pending := map[string]bool{}
	for _, hash := range old {
		if retained[hash] {
			t.uploads.Forget(hash)
		} else if t.uploads.BeginDelete(hash, now, 24*time.Hour) {
			pending[hash] = true
		}
	}
	if len(pending) > 0 {
		t.StateEpoch.Add(1)
	}
	return pending
}

func (t *Tenant) downloadBytes(w http.ResponseWriter, hash, name, contentType string) {
	files := t.files()
	outsideLane(w, func(w http.ResponseWriter) { streamBytes(w, files, t.ID+"/"+hash, name, contentType) })
}

func streamBytes(w http.ResponseWriter, files platform.FileStore, key, name, contentType string) {
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

func retainUploadHashes[T any](caller platform.Caller, keep func(T)) bool {
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

func (t *Tenant) downloadMediaItem(w http.ResponseWriter, r *http.Request, member platform.Member, now time.Time) {
	id := r.PathValue("id")
	view, refusal := t.recordOf(member, platform.MediaSetType, id, now)
	if refusal != nil {
		Reply(w, nil, refusal)
		return
	}
	set := view.Record.(platform.MediaSet)
	items := set.Items
	versions, _, refusal := platform.Find[platform.MediaSetVersion](t.automation("build", false), platform.Query{Domain: platform.Raw([][]any{{"mediaSet", "=", id}}), Sort: []string{"version"}, Limit: 1000})
	if refusal != nil {
		Reply(w, nil, refusal)
		return
	}

	if value := r.URL.Query().Get("version"); value != "" {
		version, err := strconv.Atoi(value)
		if err != nil || version < 1 {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		if version != set.Version {
			items = nil
		}
		for _, snapshot := range versions {
			if snapshot.Version == version {
				items = snapshot.Items
			}
		}
	} else {
		for _, snapshot := range versions {
			items = append(items, snapshot.Items...)
		}
	}
	for _, item := range items {
		if item.Hash == r.PathValue("hash") {
			t.downloadBytes(w, item.Hash, item.Name, item.ContentType)
			return
		}
	}
	Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND})
}
