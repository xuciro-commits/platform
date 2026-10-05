package platformserver

import (
	"fmt"
	"reflect"
	"slices"

	"platformserver/apps/build"
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
func (t *Tenant) releaseUpgradePlanLocked(candidate platform.ReleaseCandidate) (*ReleaseUpgradePlan, error) {
	owner := t.app(build.ID).(*build.Build)
	publications, _, err := owner.PrepareReleasePublications(candidate.Assets)
	if err != nil {
		return nil, err
	}
	plan := &ReleaseUpgradePlan{CandidateID: candidate.ID, ActiveID: t.activeRelease}
	for _, publication := range publications {
		if publication.Schema != build.SchemaPublish {
			continue
		}
		declaration, err := owner.ReleaseDeclaration(publication)
		if err != nil {
			return nil, err
		}
		info, err := platform.Describe(build.ID, declaration.Entity, func(reflect.Type) string { return "" })
		if err != nil {
			return nil, err
		}
		old := t.records.types[info.Type]
		if old == nil || len(old.rows) == 0 || sameStoredFields(old.info, info) {
			continue
		}
		field := optionalStoredAddition(old.info, info)
		if field == nil {
			return nil, fmt.Errorf("object %s needs an unsupported storage migration", info.Type)
		}
		shape, err := canonicalDigest(old.info.Fields)
		if err != nil {
			return nil, err
		}
		plan.Additions = append(plan.Additions, ReleaseFieldAddition{Type: info.Type, Field: field.Name, Kind: field.Type, Records: len(old.rows), SourceShape: shape})
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
