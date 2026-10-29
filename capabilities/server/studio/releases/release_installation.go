package releases

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
	"platformserver/studio/build"
	"reflect"
	"slices"
)

type OwnerDraftRequest struct {
	Key         string
	From, Until int
	Refs        map[platform.AssetRef]bool
}

func AssetDraftPayload(info platform.EntityInfo, image json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(image, &fields); err != nil {
		return nil, err
	}
	payload := map[string]json.RawMessage{}
	for _, field := range info.Fields {
		if value, ok := fields[field.Name]; ok && !field.ReadOnly && string(value) != "null" {
			payload[field.Name] = value
		}
	}
	return json.Marshal(payload)
}

func SameStoredFields(a, b platform.EntityInfo) bool {
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for _, old := range a.Fields {
		next, ok := b.Field(old.Name)
		if !ok || old.Type != next.Type || old.Ref != next.Ref || old.Required != next.Required || !slices.Equal(old.Choices, next.Choices) {
			return false
		}
	}
	return true
}

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

func OptionalStoredAddition(old, next platform.EntityInfo) *platform.FieldInfo {
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

type ReleaseExtensionReview struct {
	Type          string                `json:"type"`
	Base          platform.AssetBinding `json:"base"`
	TargetVersion string                `json:"targetVersion"`
	Fields        []string              `json:"fields"`
	Issue         string                `json:"issue,omitempty"`
}

func InstallationPlan(registry *platform.AssetRegistry, installation platform.ReleaseInstallation) (platform.AssetInstallation, error) {
	app := build.ID
	if installation.Ref != nil {
		app = installation.Ref.App
	}
	plan, err := registry.DecodeAssetInstallation(app, installation.Schema, installation.Image)
	if err != nil {
		return plan, err
	}
	if installation.Ref != nil && (*installation.Ref != plan.Ref || installation.Record != plan.Record) {
		return plan, fmt.Errorf("release installation identity differs")
	}
	return plan, nil
}

func BindInstallationPredecessors(plans []platform.AssetInstallation, read func(string) (string, error)) ([]platform.ReleaseInstallation, error) {
	installations := make([]platform.ReleaseInstallation, 0, len(plans))
	for _, plan := range plans {
		before, err := read(plan.Record)
		if err != nil {
			return nil, err
		}
		ref := plan.Ref
		installations = append(installations, platform.ReleaseInstallation{ReleasePublication: platform.ReleasePublication{Schema: plan.Schema, Image: plan.Image}, Before: before, Ref: &ref, Record: plan.Record})
	}
	return installations, nil
}
func SelectOwnerDrafts(drafts []platform.AssetDraft, context platform.AssetReleaseContext, request *OwnerDraftRequest) ([]platform.AssetDraft, error) {
	if request != nil && request.Refs != nil {
		drafts = slices.DeleteFunc(drafts, func(draft platform.AssetDraft) bool { return !request.Refs[draft.Ref] })
	}
	if request != nil && request.Until != 0 {
		if !context.DraftsOnly || request.From < 0 || request.Until > len(drafts) || request.From >= request.Until || request.Until-request.From > 64 {
			return nil, fmt.Errorf("draft batch must contain one to 64 selected owner drafts")
		}
		drafts = drafts[request.From:request.Until]
	}
	return drafts, nil
}

type StoredShape struct {
	Before, After platform.EntityInfo
	Records       int
}

func UpgradePlan(candidate, active string, shapes []StoredShape) (*ReleaseUpgradePlan, error) {
	plan := &ReleaseUpgradePlan{CandidateID: candidate, ActiveID: active}
	for _, shape := range shapes {
		if shape.Records == 0 || SameStoredFields(shape.Before, shape.After) {
			continue
		}
		field := OptionalStoredAddition(shape.Before, shape.After)
		if field == nil {
			return nil, fmt.Errorf("object %s needs an unsupported storage migration", shape.Before.Type)
		}
		digest, err := platform.CanonicalDigest(shape.Before.Fields)
		if err != nil {
			return nil, err
		}
		plan.Additions = append(plan.Additions, ReleaseFieldAddition{Type: shape.Before.Type, Field: field.Name, Kind: field.Type, Records: shape.Records, SourceShape: digest})
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
	var err error
	plan.ID, err = platform.CanonicalDigest(plan)
	return plan, err
}
