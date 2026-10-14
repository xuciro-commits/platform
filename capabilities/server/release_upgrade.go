package platformserver

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"platformserver/platform"
)

// The first storage upgrade profile adds exactly one optional scalar per
// existing object. It has no defaults, transformations, removals or renames.
type ReleaseFieldAddition struct {
	Type        string `json:"type"`
	Field       string `json:"field"`
	Kind        string `json:"kind"`
	Records     int    `json:"records"`
	SourceShape string `json:"sourceShape"`
}

type ReleaseUpgradePlan struct {
	ID          string                 `json:"id"`
	CandidateID string                 `json:"candidateId"`
	ActiveID    string                 `json:"activeId"`
	Additions   []ReleaseFieldAddition `json:"additions"`
}

func optionalStoredAddition(old, next platform.EntityInfo) *platform.FieldInfo {
	if len(next.Fields) != len(old.Fields)+1 {
		return nil
	}
	for _, field := range old.Fields {
		other, ok := next.Field(field.Name)
		if !ok || field.Type != other.Type || field.Ref != other.Ref || field.Required != other.Required || !slices.Equal(field.Choices, other.Choices) || !reflect.DeepEqual(field.Property, other.Property) {
			return nil
		}
	}
	for _, field := range next.Fields {
		if _, exists := old.Field(field.Name); exists {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || len(field.Choices) != 0 || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return nil
		}
		return &field
	}
	return nil
}

// The plan is derived from immutable candidate bytes and current storage;
// callers cannot supply migration instructions or replace its source binding.
func (t *Tenant) releaseUpgradePlanLocked(candidate platform.ReleaseCandidate, members ...platform.Member) (*ReleaseUpgradePlan, error) {
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
	plan := &ReleaseUpgradePlan{CandidateID: candidate.ID, ActiveID: t.releases.active}
	for typ, old := range t.records.types {
		next := trial.records.types[typ]
		if next == nil || old.rows.len() == 0 || sameStoredFields(old.info, next.info) {
			continue
		}
		field := optionalStoredAddition(old.info, next.info)
		if field == nil {
			return nil, fmt.Errorf("object %s needs an unsupported storage migration", typ)
		}
		shape, err := canonicalDigest(old.info.Fields)
		if err != nil {
			return nil, err
		}
		plan.Additions = append(plan.Additions, ReleaseFieldAddition{Type: typ, Field: field.Name, Kind: field.Type, Records: old.rows.len(), SourceShape: shape})
	}
	if len(plan.Additions) == 0 {
		return nil, nil
	}
	slices.SortFunc(plan.Additions, func(a, b ReleaseFieldAddition) int {
		if a.Type < b.Type {
			return -1
		}
		if a.Type > b.Type {
			return 1
		}
		return 0
	})
	plan.ID, err = canonicalDigest(plan)
	return plan, err
}
