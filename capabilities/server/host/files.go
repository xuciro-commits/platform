package host

import (
	"context"
	"io"
	"net/http"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	resources "platformserver/services/data/blobs"
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
		return Upload{}, resources.RefusalStatus(refusal), refusal
	}
	received, status, err := resources.Receive(&t.uploads, t.files(), t.ID, limit, contentType, body, now)
	if err != nil {
		return Upload{}, status, err
	}
	return Upload{Hash: received.Hash, Size: received.Size, ContentType: received.ContentType, Name: name}, status, nil
}

func (t *Tenant) SweepUploads(now time.Time) {
	old := t.uploads.OlderThan(now, 24*time.Hour)
	if len(old) == 0 {
		return
	}
	reserve := func(t *Tenant, retained map[string]bool) map[string]bool {
		pending := t.uploads.ReserveDeletes(old, retained, now)
		if len(pending) > 0 {
			t.StateEpoch.Add(1)
		}
		return pending
	}
	pending, refusal := decideOnImage(t, context.Background(), "uploads:reserve", func(t *Tenant) (map[string]bool, *kernel.Error) {
		retained, refusal := resources.Retention{
			File: func() (platform.Caller, bool) {
				return t.automation(platform.FileApp, false), t.App(platform.FileApp) != nil
			},
			Build: func() (platform.Caller, bool) { return t.automation("build", false), t.App("build") != nil },
		}.Hashes()
		if refusal != nil {
			return nil, refusal
		}
		if t.readOrigin == nil {
			return reserve(t, retained), nil
		}
		return retained, nil
	}, func(t *Tenant, _ preparedAccepted, retained map[string]bool, refusal *kernel.Error) (map[string]bool, *kernel.Error) {
		if refusal != nil {
			return nil, refusal
		}
		return reserve(t, retained), nil
	})
	if refusal != nil {
		return
	}
	t.uploads.Purge(t.files(), t.ID, pending)
}

func (t *Tenant) downloadBytes(w http.ResponseWriter, hash, name, contentType string) {
	files := t.files()
	transport.OutsideLane(w, func(w http.ResponseWriter) { resources.Stream(w, files, t.ID+"/"+hash, name, contentType) })
}

func (t *Tenant) downloadMediaItem(w http.ResponseWriter, r *http.Request, member platform.Member, now time.Time) {
	id := r.PathValue("id")
	view, refusal := t.recordOf(member, platform.MediaSetType, id, now)
	if refusal != nil {
		Reply(w, nil, refusal)
		return
	}
	set := view.Record.(platform.MediaSet)
	versions, _, refusal := platform.Find[platform.MediaSetVersion](t.automation("build", false), platform.Query{Domain: platform.Raw([][]any{{"mediaSet", "=", id}}), Sort: []string{"version"}, Limit: 1000})
	if refusal != nil {
		Reply(w, nil, refusal)
		return
	}
	item, refusal := resources.MediaItem(set, versions, r.URL.Query().Get("version"), r.PathValue("hash"))
	if refusal != nil {
		Reply(w, nil, refusal)
		return
	}
	t.downloadBytes(w, item.Hash, item.Name, item.ContentType)
}
