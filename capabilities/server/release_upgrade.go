package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"fmt"

	"time"

	"platformserver/platform"
)

// The plan is derived from immutable candidate bytes and current storage;
// callers cannot supply migration instructions or replace its source binding.
func (t *Tenant) releaseUpgradePlanLocked(candidate platform.ReleaseCandidate, members ...platform.Member) (*releaseowner.ReleaseUpgradePlan, error) {
	var member platform.Member
	if len(members) > 0 {
		member = members[0]
	}
	installations, bootstrap, err := t.prepareReleaseLifecycleLocked(candidate, member, time.Now().UTC(), true)
	if err != nil {
		return nil, err
	}
	records := t.records
	if len(bootstrap) > 0 {
		prepared, err := t.prepareAcceptedBatch(t.app("build").AcceptedLedger(), bootstrap)
		if err != nil || prepared == nil {
			return nil, fmt.Errorf("upgrade bootstrap cannot prepare: %v", err)
		}
		records = prepared.records
	}
	view, err := t.assetInstallationView(records)
	if err != nil {
		return nil, err
	}
	trial, err := view.stageReleaseInstallationLocked(installations, true, true)
	if err != nil {
		return nil, err
	}
	shapes := []releaseowner.StoredShape{}
	for typ, old := range t.records.types {
		if next := trial.records.types[typ]; next != nil {
			shapes = append(shapes, releaseowner.StoredShape{Before: old.info, After: next.info, Records: old.rows.Len()})
		}
	}
	return releaseowner.UpgradePlan(candidate.ID, t.releases.Active(), shapes)
}
