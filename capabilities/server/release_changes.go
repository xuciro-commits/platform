package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"fmt"

	"platformserver/studio/build"

	"platformserver/platform"
)

func (t *Tenant) SavedDraftChanges(m platform.Member, application, typ, id string) (releaseowner.ReleaseDraftChanges, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleaseDraftChanges{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/releases/drafts/changes", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleaseDraftChanges{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return releaseowner.ReleaseDraftChanges{}, fmt.Errorf("tenant has no builder")
	}
	changes, err := owner.SavedDraftChanges(c, application, typ, id)
	return releaseowner.ReleaseDraftChanges{Changes: changes}, err
}
