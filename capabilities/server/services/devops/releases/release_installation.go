package releases

import (
	"encoding/json"
	"fmt"
	ruleprotocol "platformserver/kernel/protocol"
	"reflect"
	"strings"
	"time"

	"platformserver/kernel/records"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/publication"
	"slices"
)

type OwnerDraftRequest struct {
	Key         string
	From, Until int
	Refs        map[platform.AssetRef]bool
}

func AssetDraftPayload(info platform.RecordTypeInfo, image json.RawMessage) (json.RawMessage, error) {
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

func SameStoredFields(a, b platform.RecordTypeInfo) bool {
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

func OptionalStoredAddition(old, next platform.RecordTypeInfo) *platform.FieldInfo {
	if len(next.Fields) != len(old.Fields)+1 {
		return nil
	}
	for _, field := range old.Fields {
		other, ok := next.Field(field.Name)
		if !ok || field.Type != other.Type || field.Ref != other.Ref || field.Required != other.Required || !slices.Equal(field.Choices, other.Choices) || !reflect.DeepEqual(field.Property, other.Property) || !reflect.DeepEqual(field.ValueType, other.ValueType) {
			return nil
		}
	}
	for _, field := range next.Fields {
		if _, exists := old.Field(field.Name); exists {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.ValueType != nil || len(field.Choices) != 0 || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return nil
		}
		return &field
	}
	return nil
}

func CompatibleStoredRemoval(old, next platform.RecordTypeInfo) bool {
	if len(next.Fields) >= len(old.Fields) {
		return false
	}
	for _, field := range next.Fields {
		previous, ok := old.Field(field.Name)
		if !ok || previous.Type != field.Type || previous.Ref != field.Ref || previous.Required != field.Required ||
			!slices.Equal(previous.Choices, field.Choices) || !reflect.DeepEqual(previous.Property, field.Property) || !reflect.DeepEqual(previous.ValueType, field.ValueType) {
			return false
		}
	}
	return true
}

type ReleaseExtensionReview = platform.PackageImpactExtension

func InstallationPlan(registry *publication.AssetRegistry, installation platform.ReleaseInstallation) (ruleprotocol.AssetInstallation, error) {
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

func BindInstallationPredecessors(plans []ruleprotocol.AssetInstallation, read func(string) (string, error)) ([]platform.ReleaseInstallation, error) {
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
func SelectOwnerDrafts(drafts []ruleprotocol.AssetDraft, context ruleprotocol.AssetReleaseContext, request *OwnerDraftRequest) ([]ruleprotocol.AssetDraft, error) {
	if request != nil && request.Refs != nil {
		drafts = slices.DeleteFunc(drafts, func(draft ruleprotocol.AssetDraft) bool { return !request.Refs[draft.Ref] })
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
	Before, After platform.RecordTypeInfo
	Records       int
}

func StoredShapes(before, after *records.Store) []StoredShape {
	shapes := []StoredShape{}
	for typ, old := range before.Types() {
		if next := after.Type(typ); next != nil {
			shapes = append(shapes, StoredShape{Before: old.Info(), After: next.Info(), Records: old.Rows().Len()})
		}
	}
	return shapes
}

func StoredEntities(store *records.Store) []platform.RecordTypeInfo {
	entities := []platform.RecordTypeInfo{}
	for _, et := range store.SortedTypes() {
		entities = append(entities, et.Info())
	}
	return entities
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

func CheckExtensions(entities []platform.RecordTypeInfo, lookup func(string) (platform.RecordTypeInfo, bool)) error {
	for _, info := range entities {
		if info.Extends == "" {
			continue
		}
		base, exists := lookup(info.Extends)
		if !exists {
			return fmt.Errorf("extension %s base %s is unavailable", info.Type, info.Extends)
		}
		if err := ruleprotocol.CheckExtension(info, base); err != nil {
			return err
		}
	}
	return nil
}

func ExtensionReview(candidate ruleprotocol.ReleaseCandidate, entities []platform.RecordTypeInfo, shapes map[string]platform.RecordTypeInfo, lookup func(string) (platform.RecordTypeInfo, bool), source func(string) (platform.AssetBinding, bool)) ([]ReleaseExtensionReview, error) {
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
		} else if err := ruleprotocol.CheckExtension(extension, base); err != nil {
			row.Issue = err.Error()
		}
		result = append(result, row)
	}
	return result, nil
}

func ReviewExtensions(candidate ruleprotocol.ReleaseCandidate, apps []platform.Service, entities []platform.RecordTypeInfo,
	lookup func(string) (platform.RecordTypeInfo, bool), source func(string) (platform.AssetBinding, bool)) ([]ReleaseExtensionReview, error) {
	shapes, err := AssetShapes(apps, candidate.Assets)
	if err != nil {
		return nil, err
	}
	return ExtensionReview(candidate, entities, shapes, lookup, source)
}

func AssetShapes(apps []platform.Service, available []platform.ReleaseAsset) (map[string]platform.RecordTypeInfo, error) {
	shapes := map[string]platform.RecordTypeInfo{}
	i := slices.IndexFunc(apps, func(app platform.Service) bool { return app.Manifest().ID == build.ID })
	if i < 0 {
		return shapes, nil
	}
	owner := apps[i]
	assets := []platform.ReleaseAsset{}
	for _, asset := range available {
		if asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetObject {
			assets = append(assets, asset)
		}
	}
	if len(assets) == 0 {
		return shapes, nil
	}
	bound, err := owner.(ruleprotocol.ReleaseExecutionOwner).ForkReleaseExecution(assets)
	if err != nil {
		return nil, err
	}
	for _, entity := range bound.Manifest().Entities {
		if !slices.ContainsFunc(assets, func(a platform.ReleaseAsset) bool {
			return a.Ref.Kind == platform.AssetObject && a.Ref.Name == entity.Type
		}) {
			continue
		}
		info, err := records.Describe(build.ID, entity, func(reflect.Type) string { return "" })
		if err != nil {
			return nil, err
		}
		shapes[info.Type] = info
	}
	return shapes, nil
}

func PrepareInstallation(runtime InstallationRuntime, candidate ruleprotocol.ReleaseCandidate, member platform.Member, at time.Time, upgrade, activating bool) ([]platform.ReleaseInstallation, json.RawMessage, error) {
	if !runtime.HasAgentRuntime() && slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool {
		return asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetAgent
	}) {
		return nil, nil, fmt.Errorf("agent artifacts require the target's original agent runtime")
	}
	registry, err := runtime.Registry()
	if err != nil {
		return nil, nil, err
	}
	context := ruleprotocol.AssetReleaseContext{Member: member, At: at, Upgrade: upgrade}
	context.Project = runtime.ReleaseProject(candidate)
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

func PrepareLifecycle(runtime InstallationRuntime, candidate ruleprotocol.ReleaseCandidate, member platform.Member, at time.Time, upgrade bool) ([]platform.ReleaseInstallation, json.RawMessage, error) {
	if err := CheckProposalFunctions(candidate); err != nil {
		return nil, nil, err
	}
	return PrepareInstallation(runtime, candidate, member, at, upgrade, true)
}

type InstallationRuntime interface {
	HasAgentRuntime() bool
	Registry() (*publication.AssetRegistry, error)
	Bootstrap(ruleprotocol.ReleaseCandidate, ruleprotocol.AssetReleaseContext, bool) (InstallationRuntime, json.RawMessage, error)
	Predecessor(string) (string, error)
	Stage([]platform.ReleaseInstallation, bool) (InstallationRuntime, error)
	ReleaseProject(ruleprotocol.ReleaseCandidate) string
	CheckExtensions() error
	CheckStorage(InstallationRuntime, bool) error
	Pending(string, []byte) error
}

func CheckStorage(shapes []StoredShape, before, after []platform.Definition, upgrade, allowContraction bool) error {
	for _, shape := range shapes {
		if shape.Records == 0 {
			continue
		}
		if SameStoredFields(shape.Before, shape.After) {
			continue
		}
		if upgrade && OptionalStoredAddition(shape.Before, shape.After) != nil {
			continue
		}
		if allowContraction && CompatibleStoredRemoval(shape.Before, shape.After) {
			continue
		}
		return fmt.Errorf("object %s has records; changing their storage shape needs a supported upgrade plan", shape.Before.Type)
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
		if err := ruleprotocol.CheckConstraints(info, context.Lookup, true, engines...); err != nil {
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
			if err := context.CheckApplicationProfile(definition.Application.Profile); err != nil {
				return err
			}
			if err := context.Application(definition.Ref, *definition.Application); err != nil {
				return err
			}
			if err := context.PageQueries(context.ApplicationPage(*definition.Application)); err != nil {
				return err
			}
			for _, name := range definition.Application.Pages {
				ref := platform.AssetRef{App: definition.Ref.App, Kind: platform.AssetPage, Name: name}
				page, ok := pages[ref]
				if !ok {
					return fmt.Errorf("application page %s is unavailable", ref)
				}
				if err := pageowner.CheckPageVariables(*definition.Application, page); err != nil {
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

type ValidationContext struct {
	Entities                []platform.RecordTypeInfo
	Definitions             []platform.Definition
	Lookup                  func(string) (platform.RecordTypeInfo, bool)
	Properties              func(platform.RecordTypeInfo) error
	RecordSets              func(platform.RecordTypeInfo, ...platform.RuleEngine) error
	Link                    func(string, platform.LinkType) error
	Application             func(platform.AssetRef, platform.Application) error
	ApplicationPage         func(platform.Application) platform.Page
	CheckApplicationProfile func(*platform.ApplicationProfile) error
	PageQueries             func(platform.Page) error
	Navigation              func(platform.Page, string) error
}

func StageInstallations(registry *publication.AssetRegistry, installations []platform.ReleaseInstallation, apply ruleprotocol.AssetApplyContext, check func(ruleprotocol.AssetInstallation, platform.ReleaseInstallation) error, record func(ruleprotocol.AssetInstallation) error) error {
	for _, installation := range installations {
		plan, err := InstallationPlan(registry, installation)
		if err != nil {
			return err
		}
		if err := check(plan, installation); err != nil {
			return err
		}
		if err := registry.ApplyInstallation(plan, apply); err != nil {
			return err
		}
		if err := record(plan); err != nil {
			return err
		}
	}
	return nil
}

func StageReleaseInstallation(registry *publication.AssetRegistry, installations []platform.ReleaseInstallation, projection *records.Store,
	apply ruleprotocol.AssetApplyContext, register func(ruleprotocol.AssetInstallation) error) error {
	for _, installation := range installations {
		plan, err := InstallationPlan(registry, installation)
		if err != nil {
			return err
		}
		before, err := projection.RowDigest(plan.Record)
		if err != nil || before != installation.Before {
			return fmt.Errorf("release metadata predecessor differs for %s: %v", plan.Record, err)
		}
		if err := registry.ApplyInstallation(plan, apply); err != nil {
			return err
		}
		if err := projection.InstallImage(plan.Record, plan.Image); err != nil {
			return err
		}
		if err := register(plan); err != nil {
			return err
		}
	}
	return nil
}

func RegisterInstalledDefinition(definitions []platform.Definition, plan ruleprotocol.AssetInstallation,
	resolve func(ruleprotocol.AssetInstallation) (platform.Definition, bool, error), set func([]platform.Definition)) error {
	definition, indexed, err := resolve(plan)
	if err != nil || !indexed {
		return err
	}
	for i, existing := range definitions {
		if existing.Ref == plan.Ref {
			definitions[i] = definition
			return nil
		}
	}
	set(append(definitions, definition))
	return nil
}

func RegisterOwnedAssetMetadata(definitions []platform.Definition, owned func([]platform.Definition) ([]platform.Definition, error), set func([]platform.Definition)) error {
	next, err := owned(definitions)
	if err == nil {
		set(next)
	}
	return err
}
