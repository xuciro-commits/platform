package platformserver

import (
	"fmt"

	build "platformserver/apps/build"
	"platformserver/platform"
)

type ReleaseDraftChanges struct {
	Changes []build.DraftChange `json:"changes"`
}

func (t *Tenant) SavedDraftChanges(m platform.Member, application, typ, id string) (ReleaseDraftChanges, error) {
	if err := t.admits(m); err != nil {
		return ReleaseDraftChanges{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/releases/drafts/changes", "read", []string{build.Builder}, true)); err != nil {
		return ReleaseDraftChanges{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(ConstructionDrafts)
	if !ok {
		return ReleaseDraftChanges{}, fmt.Errorf("tenant has no builder")
	}
	changes, err := owner.SavedDraftChanges(c, application, typ, id)
	return ReleaseDraftChanges{Changes: changes}, err
}
