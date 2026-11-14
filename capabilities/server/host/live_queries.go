package host

import (
	"net/http"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) liveFollow(m platform.Member, mux *http.ServeMux) transport.LiveFollow {
	return transport.LiveFollow{Mux: mux, State: t.Changes, Version: t.liveVersion,
		SharedKey: func(query, version, language string) (string, bool) {
			return build.PublicLiveKey(m, query, version, language, t.Entity)
		},
		Snapshot: func(key string, read func() (int, []byte)) (int, []byte) {
			return t.viewCache().LiveSnapshot(key, read)
		}}
}

func (t *Tenant) liveVersion(query string) string {
	global, meta, seq, owners, ops := t.change.Versions()
	if image := t.readImage.Load(); image != nil {
		return image.state.liveReadVersion(query, global, meta, seq, image.operations, owners, ops)
	}
	t.Lane.Lock()
	defer t.Lane.Unlock()
	return t.liveReadVersion(query, global, meta, seq, seq, owners, ops)
}

func (t *Tenant) liveReadVersion(query string, global, meta, seq, published int64, owners, ops map[string]int64) string {
	return transport.LiveVersions{BranchType: build.BranchType,
		Lock:          func() func() { t.Projection.Lock(); return t.Projection.Unlock },
		Generation:    t.Projection.Generation,
		LiveVersion:   t.Projection.LiveVersion,
		Scoped:        t.Projection.ReadScoped,
		ComputeOwners: func(ref platform.AssetRef) ([]string, error) { return t.applicationContext().ComputeOwners(ref) },
		ReadOwner: func(name string) string {
			if app := t.App(t.owner["read:"+name]); app != nil {
				return app.Manifest().ID
			}
			return ""
		}}.Of(query, global, meta, seq, published, owners, ops)
}
