package releases

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"platformserver/platform"
	"platformserver/studio/build"
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

// All installed extensions participate in upgrade admission, including those
// outside the selected application closure. Nothing republishes or replaces an
// extension merely because its base package changed.
func CheckExtensions(entities []platform.EntityInfo, lookup func(string) (platform.EntityInfo, bool)) error {
	for _, info := range entities {
		if info.Extends == "" {
			continue
		}
		base, exists := lookup(info.Extends)
		if !exists {
			return fmt.Errorf("extension %s base %s is unavailable", info.Type, info.Extends)
		}
		if err := platform.CheckExtension(info, base); err != nil {
			return err
		}
	}
	return nil
}

func ExtensionReview(candidate platform.ReleaseCandidate, entities []platform.EntityInfo, shapes map[string]platform.EntityInfo, lookup func(string) (platform.EntityInfo, bool), source func(string) (platform.AssetBinding, bool)) ([]ReleaseExtensionReview, error) {
	versions := map[string]string{}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetObject {
			versions[asset.Ref.Name] = asset.SourceVersion
		}
	}
	var result []ReleaseExtensionReview
	for _, info := range entities {
		extension := info
		if next, exists := shapes[extension.Type]; exists {
			extension = next
		}
		if extension.Extends == "" {
			continue
		}
		base, exists := shapes[extension.Extends]
		if !exists {
			base, exists = lookup(extension.Extends)
		}
		row := ReleaseExtensionReview{Type: extension.Type, Fields: []string{}, TargetVersion: versions[extension.Extends]}
		if extension.ExtensionSource != nil {
			row.Base = extension.ExtensionSource.Base
		}
		if row.Base.Ref.Name == "" {
			row.Base.Ref = platform.AssetRef{App: base.App, Kind: platform.AssetObject, Name: extension.Extends}
		}
		if row.TargetVersion == "" {
			if binding, ok := source(extension.Extends); ok {
				row.TargetVersion = binding.SourceVersion
			}
		}
		for _, field := range extension.Fields {
			if field.Name != "base" {
				row.Fields = append(row.Fields, field.Name)
			}
		}
		if !exists {
			row.Issue = "extension base is unavailable"
		} else if err := platform.CheckExtension(extension, base); err != nil {
			row.Issue = err.Error()
		}
		result = append(result, row)
	}
	return result, nil
}

// releaseAssetShapes describes the object entities a set of release assets
// declares, per owning app.
func AssetShapes(apps []platform.App, available []platform.ReleaseAsset) (map[string]platform.EntityInfo, error) {
	shapes := map[string]platform.EntityInfo{}
	for _, owner := range apps {
		compiler, ok := owner.(platform.ReleaseExecutionOwner)
		if !ok {
			continue
		}
		assets := []platform.ReleaseAsset{}
		for _, asset := range available {
			if asset.Ref.App == owner.Manifest().ID {
				assets = append(assets, asset)
			}
		}
		if len(assets) == 0 {
			continue
		}
		bound, err := compiler.ForkReleaseExecution(assets)
		if err != nil {
			return nil, err
		}
		for _, entity := range bound.Manifest().Entities {
			if !slices.ContainsFunc(assets, func(a platform.ReleaseAsset) bool {
				return a.Ref.Kind == platform.AssetObject && a.Ref.Name == entity.Type
			}) {
				continue
			}
			info, err := platform.Describe(owner.Manifest().ID, entity, func(reflect.Type) string { return "" })
			if err != nil {
				return nil, err
			}
			shapes[info.Type] = info
		}
	}
	return shapes, nil
}

func PrepareInstallation(runtime InstallationRuntime, candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade, activating bool) ([]platform.ReleaseInstallation, json.RawMessage, error) {
	if !runtime.HasAgentRuntime() && slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool {
		return asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetAgent
	}) {
		return nil, nil, fmt.Errorf("agent artifacts require the target's original agent runtime")
	}
	registry, err := runtime.Registry()
	if err != nil {
		return nil, nil, err
	}
	context := platform.AssetReleaseContext{Member: member, At: at, Upgrade: upgrade}
	view, bootstrap, err := runtime.Bootstrap(candidate, context, !activating)
	if err != nil {
		return nil, nil, err
	}
	registry, err = view.Registry()
	if err != nil {
		return nil, nil, err
	}
	end, err := registry.BeginBootstrap(candidate.Assets)
	if err != nil {
		return nil, nil, err
	}
	defer end()
	plans, err := registry.PrepareReleaseAssets(candidate.Assets, context)
	if err != nil {
		return nil, nil, err
	}
	installations, err := BindInstallationPredecessors(plans, view.Predecessor)
	if err != nil {
		return nil, nil, err
	}
	trial, err := view.Stage(installations, upgrade)
	if err != nil {
		return nil, nil, err
	}
	if err := trial.CheckExtensions(); err != nil {
		return nil, nil, err
	}
	if activating {
		if err := runtime.CheckStorage(trial, upgrade); err != nil {
			return nil, nil, err
		}
	}
	if err := runtime.Pending(candidate.ID, candidate.Bytes); err != nil {
		return nil, nil, err
	}
	return installations, bootstrap, nil
}

// InstallationRuntime keeps private rows and accepted create decisions in their
// original runtime. Studio owns ordering and retained installation planning.
type InstallationRuntime interface {
	HasAgentRuntime() bool
	Registry() (*platform.AssetRegistry, error)
	Bootstrap(platform.ReleaseCandidate, platform.AssetReleaseContext, bool) (InstallationRuntime, json.RawMessage, error)
	Predecessor(string) (string, error)
	Stage([]platform.ReleaseInstallation, bool) (InstallationRuntime, error)
	CheckExtensions() error
	CheckStorage(InstallationRuntime, bool) error
	Pending(string, []byte) error
}

func CheckStorage(shapes []StoredShape, before, after []platform.Definition, upgrade bool) error {
	for _, shape := range shapes {
		if shape.Records == 0 {
			continue
		}
		if !SameStoredFields(shape.Before, shape.After) && (!upgrade || OptionalStoredAddition(shape.Before, shape.After) == nil) {
			return fmt.Errorf("object %s has records; changing their storage shape needs a supported upgrade plan", shape.Before.Type)
		}
	}
	for _, definition := range before {
		if definition.Action != nil && definition.Source != "code" && !slices.ContainsFunc(after, func(next platform.Definition) bool { return next.Ref == definition.Ref }) {
			action := definition.Ref.Name
			if definition.Action.Automation {
				for _, suffix := range []string{platform.ApprovalHeld, platform.ApprovalRejected, platform.ApprovalReturned} {
					action = strings.TrimSuffix(action, suffix)
				}
			}
			return fmt.Errorf("retiring action %s is not part of a supported upgrade; keep the action in the new release", definition.Ref.Name)
		}
	}
	return nil
}

func ValidateSet(context ValidationContext, engines ...platform.RuleEngine) error {
	for _, info := range context.Entities {
		if err := context.Properties(info); err != nil {
			return err
		}
		if err := context.RecordSets(info, engines...); err != nil {
			return err
		}
		if err := platform.CheckConstraints(info, context.Lookup, true, engines...); err != nil {
			return err
		}
	}
	pages := map[platform.AssetRef]platform.Page{}
	for _, definition := range context.Definitions {
		if definition.LinkType != nil {
			if err := context.Link(definition.Ref.App, *definition.LinkType); err != nil {
				return err
			}
			for _, link := range definition.LinkVersions {
				if err := context.Link(definition.Ref.App, link); err != nil {
					return err
				}
			}
		}
		if definition.Page != nil {
			pages[definition.Ref] = *definition.Page
		}
	}
	for _, definition := range context.Definitions {
		if definition.Application != nil {
			if err := context.Application(definition.Ref, *definition.Application); err != nil {
				return err
			}
			if err := context.PageQueries(definition.Application.QueryPage()); err != nil {
				return err
			}
			for _, name := range definition.Application.Pages {
				ref := platform.AssetRef{App: definition.Ref.App, Kind: platform.AssetPage, Name: name}
				page, ok := pages[ref]
				if !ok {
					return fmt.Errorf("application page %s is unavailable", ref)
				}
				if err := definition.Application.CheckPageVariables(page); err != nil {
					return err
				}
			}
		}
		if definition.Page != nil {
			if err := context.Navigation(*definition.Page, definition.Ref.App); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidationContext captures one private installation's existing declaration
// compilers; queries and authorization remain their original host entries.
type ValidationContext struct {
	Entities    []platform.EntityInfo
	Definitions []platform.Definition
	Lookup      func(string) (platform.EntityInfo, bool)
	Properties  func(platform.EntityInfo) error
	RecordSets  func(platform.EntityInfo, ...platform.RuleEngine) error
	Link        func(string, platform.LinkType) error
	Application func(platform.AssetRef, platform.Application) error
	PageQueries func(platform.Page) error
	Navigation  func(platform.Page, string) error
}
