package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/construction"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

func (*Build) AssetKinds() []platform.AssetKind {
	return []platform.AssetKind{
		platform.AssetObject, platform.AssetAction, platform.AssetPage, platform.AssetApp, platform.AssetFlow, platform.AssetPropertyType, platform.AssetLinkType, platform.AssetQuery, platform.AssetFunction, platform.AssetCompute,
		platform.AssetAlert,
	}
}
func (b *Build) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	if ref.App != construction.ID {
		return platform.ReleaseAsset{}, fmt.Errorf("asset has another owner")
	}
	if len(contentVersion) > 1 || len(contentVersion) == 1 && ref.Kind != platform.AssetPage {
		return platform.ReleaseAsset{}, fmt.Errorf("content pins only identify one retained page")
	}
	if len(contentVersion) == 1 {
		if version != b.Manifest().Version {
			return platform.ReleaseAsset{}, fmt.Errorf("unsupported page source version")
		}
		if page, ok := b.PageContent(ref.Name, contentVersion[0]); ok {
			return platform.PageReleaseAsset(construction.ID, version, page)
		}
		return platform.ReleaseAsset{}, fmt.Errorf("page content %s@%s is not retained", ref, contentVersion[0])
	}
	if ref.Kind == platform.AssetAlert {
		return b.alertPinned(ref, version)
	}
	if ref.Kind == platform.AssetMigration {
		return b.migrationPlanPinned(ref, version)
	}
	if ref.Kind == platform.AssetQuery {
		return b.QueryReleaseAsset(ref.Name, version)
	}
	if ref.Kind == platform.AssetFunction {
		return b.FunctionReleaseAsset(ref.Name, version)
	}
	if ref.Kind == platform.AssetAgent {
		for _, record := range b.agents {
			asset, err := agentReleaseAsset(record, b.Manifest().Version)
			if err != nil {
				return platform.ReleaseAsset{}, err
			}
			if asset.Ref == ref && asset.SourceVersion == version {
				return asset, nil
			}
		}
		return b.retainedAgentAsset(ref, version)
	}
	if ref.Kind == platform.AssetCompute {
		return b.OperationReleaseAsset(ref.Name, version)
	}
	if ref.Kind == platform.AssetPropertyType {
		return b.PropertyTypeReleaseAsset(ref.Name, version)
	}
	if ref.Kind == platform.AssetLinkType {
		return b.LinkTypeReleaseAsset(ref.Name, version)
	}
	return platform.ReleaseAsset{}, fmt.Errorf("asset version %s@%s is unavailable", ref, version)
}



func (b *Build) ReleaseDrafts(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	ordered, err := orderOwnerAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	old, err := b.releaseDraftsOf(platform.ReleaseCandidate{Assets: assets})
	if err != nil {
		return nil, err
	}
	var out []platform.AssetDraft
	for _, draft := range old {
		ref, err := draftAssetRef(draft.Type, draft.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.AssetDraft{Ref: ref, Type: draft.Type, Name: draft.Name, Schema: draft.Type + ".create", Image: draft.Image})
	}
	for _, asset := range assets {
		if asset.Ref.App != construction.ID {
			continue
		}
		typ, record, err := b.declarationReleaseDraft(asset)
		if err != nil {
			return nil, err
		}
		if typ == "" {
			continue
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: typ, Name: asset.Ref.Name, Schema: typ + ".create", Image: raw})
	}
	byRef := map[platform.AssetRef]platform.AssetDraft{}
	for _, draft := range out {
		byRef[draft.Ref] = draft
	}
	out = nil
	for _, asset := range assets {
		if draft, ok := byRef[asset.Ref]; ok {
			out = append(out, draft)
			delete(byRef, asset.Ref)
		}
	}
	if len(byRef) != 0 {
		return nil, fmt.Errorf("draft has no candidate owner identity")
	}
	return out, nil
}
func (b *Build) PrepareReleaseAssets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetInstallation, error) {
	if err := b.validateReleaseMember(assets, ctx.Member); err != nil && !ctx.DraftsOnly {
		return nil, err
	}
	ordered, err := orderOwnerAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	publications, err := b.prepareReleasePublications(assets)
	if err != nil {
		return nil, err
	}
	out, err := b.prepareMigrationPlanInstallations(assets)
	if err != nil {
		return nil, err
	}
	for _, p := range publications {
		plan, err := b.DecodeAssetInstallation(p.Schema, p.Image)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return orderOwnerInstallations(assets, out)
}
func (b *Build) DecodeAssetInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	if schema == SchemaMigrationPlan {
		return migrationPlanInstallation(image)
	}
	typ, _, _ := strings.Cut(schema, ".publish")
	if schema != typ+".publish" {
		return platform.AssetInstallation{}, fmt.Errorf("invalid publication schema")
	}
	var record struct {
		platform.Record
		Name string `json:"name"`
	}
	if !slices.Contains(b.AcceptedPublicationSchemas(), schema) || json.Unmarshal(image, &record) != nil || record.ID == "" {
		return platform.AssetInstallation{}, fmt.Errorf("invalid release publication row")
	}
	ref, err := draftAssetRef(typ, record.Name)
	if err != nil {
		return platform.AssetInstallation{}, err
	}
	switch schema {
	case SchemaTablePublish:
		r, err := tableImage(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		ref = platform.AssetRef{App: construction.ID, Kind: platform.AssetCompute, Name: r.Name}
	case SchemaAlertPublish:
		_, asset, err := alertImage(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		ref = asset.Ref
	}
	return platform.AssetInstallation{Ref: ref, Schema: schema, Record: typ + "/" + record.ID, Image: slices.Clone(image)}, nil
}

// Validation is performed against the private installation host, after the
// candidate dependencies have been installed in their saved order.
func (b *Build) ValidateAssetInstallation(plan platform.AssetInstallation) error {
	decoded, err := b.DecodeAssetInstallation(plan.Schema, plan.Image)
	if err != nil {
		return err
	}
	if decoded.Ref != plan.Ref || decoded.Record != plan.Record {
		return fmt.Errorf("installation identity differs from its saved image")
	}
	if plan.Ref.Kind == platform.AssetMigration || plan.Ref.Kind == platform.AssetSampleSet {
		return nil
	}
	return b.ValidateAcceptedPublication(plan.Schema, plan.Image)
}
func (b *Build) ApplyAssetInstallation(plan platform.AssetInstallation, ctx platform.AssetApplyContext) error {
	if ctx.Validate && (plan.Ref.Kind == platform.AssetMigration || plan.Ref.Kind == platform.AssetSampleSet) {
		_, err := b.DecodeAssetInstallation(plan.Schema, plan.Image)
		return err
	}
	if ctx.Private {
		// Keep the private catalog's earlier object declarations visible to
		// later pages/functions/flows in the same installation. The live ledger
		// remains untouched; this owner belongs to the installation view.
		saved := b.ledger
		b.installationCatalog = platform.NewCatalog(b.Manifest().Actions.All()...)
		b.ledger = platform.NewLedger("private-release", construction.ID, b.installationCatalog)
		defer func() { b.ledger = saved }()
	}
	return b.ApplyAcceptedPublication(plan.Schema, plan.Image)
}

func (b *Build) AssetEnvironmentRequirements(platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	return nil, nil
}

func (b *Build) ResolveAssetEnvironment(r platform.AssetEnvironmentRequirement) (platform.AssetEnvironmentTarget, error) {
	return platform.AssetEnvironmentTarget{}, fmt.Errorf("asset %s needs no environment binding", r.Ref)
}

var _ platform.AssetLifecycleOwner = (*Build)(nil)

// Candidate dependencies determine installation order. Object references can
// be mutually recursive; all other execution/publication cycles are refused.
func orderOwnerAssets(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	graph, err := platform.NewReleaseDependencyGraph(assets)
	if err != nil {
		return nil, err
	}
	refs, err := graph.InstallationOrder()
	if err != nil {
		return nil, err
	}
	byRef := make(map[platform.AssetRef]platform.ReleaseAsset, len(assets))
	for _, asset := range assets {
		byRef[asset.Ref] = asset
	}
	out := make([]platform.ReleaseAsset, 0, len(assets))
	for _, ref := range refs {
		out = append(out, byRef[ref])
	}
	return out, nil
}

func orderOwnerInstallations(assets []platform.ReleaseAsset, plans []platform.AssetInstallation) ([]platform.AssetInstallation, error) {
	ordered, err := orderOwnerAssets(assets)
	if err != nil {
		return nil, err
	}
	byRef := map[platform.AssetRef]platform.AssetInstallation{}
	for _, plan := range plans {
		if _, ok := byRef[plan.Ref]; ok {
			return nil, fmt.Errorf("duplicate installation %s", plan.Ref)
		}
		byRef[plan.Ref] = plan
	}
	var out []platform.AssetInstallation
	for _, asset := range ordered {
		if plan, ok := byRef[asset.Ref]; ok {
			out = append(out, plan)
			delete(byRef, asset.Ref)
		}
	}
	if len(byRef) != 0 {
		return nil, fmt.Errorf("installation lacks its candidate descriptor")
	}
	return out, nil
}

// Preparation retains the caller's token scopes, which are not directory
// fields. Dependency installation later validates current target role grants.
func (b *Build) validateReleaseMember(assets []platform.ReleaseAsset, member platform.Member) error {
	return nil
}

func draftAssetRef(typ, name string) (platform.AssetRef, error) {
	kinds := map[string]platform.AssetKind{construction.ObjectType: platform.AssetObject, construction.PageType: platform.AssetPage, construction.AppType: platform.AssetApp, MigrationPlanType: platform.AssetMigration,
		construction.PropertyTypeType: platform.AssetPropertyType, construction.LinkTypeType: platform.AssetLinkType, construction.QueryType: platform.AssetQuery, construction.FunctionType: platform.AssetFunction, AgentType: platform.AssetAgent,
		construction.CodeType: platform.AssetCompute, construction.TableType: platform.AssetCompute, construction.AlertType: platform.AssetAlert}
	kind := kinds[typ]
	if kind == "" {
		return platform.AssetRef{}, fmt.Errorf("unknown native release draft %s", typ)
	}
	if kind == platform.AssetObject {
		name = construction.TypeOf(name)
	}
	return platform.AssetRef{App: construction.ID, Kind: kind, Name: name}, nil
}
func (b *Build) declarationReleaseDraft(asset platform.ReleaseAsset) (string, any, error) {
	switch asset.Ref.Kind {
	case platform.AssetMigration:
		d, _, err := decodeMigrationAsset(asset)
		if err != nil {
			return "", nil, err
		}
		return MigrationPlanType, MigrationPlanRecord{Name: d.Name, Title: d.Title, Plan: d.Plan}, nil
	case platform.AssetPropertyType:
		var d platform.PropertyType
		if err := json.Unmarshal(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.PropertyTypeType, propertyTypeFromDeclaration(d), nil
	case platform.AssetLinkType:
		var d platform.LinkType
		if err := json.Unmarshal(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.LinkTypeType, linkTypeFromDeclaration(d), nil
	case platform.AssetQuery:
		var d Query
		if err := json.Unmarshal(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.QueryType, d, nil
	case platform.AssetFunction:
		var d construction.Function
		if err := json.Unmarshal(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.FunctionType, d, nil
	case platform.AssetAgent:
		d, err := decodeAgentAsset(asset)
		if err != nil {
			return "", nil, err
		}
		d.Version, d.State = 0, "draft"
		return AgentType, d, nil
	case platform.AssetCompute:
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) == nil && operation.Binding.Kind == "native" {
			table, err := tableFromAsset(asset)
			table.State, table.Version = "draft", 0
			return construction.TableType, table, err
		}
		rows, err := b.codeInventory()
		if err != nil {
			return "", nil, err
		}
		for _, record := range rows {
			if record.Name == asset.Ref.Name && !record.Archived {
				return construction.CodeType, record, nil
			}
		}
		return "", nil, fmt.Errorf("compute %s needs its original compiled target owner; a module descriptor does not carry source or compilation evidence", asset.Ref)
	case platform.AssetAlert:
		alert, _, err := alertFromAsset(asset)
		alert.State = "draft"
		return construction.AlertType, alert, err
	}
	return "", nil, nil
}

// Bootstrap shapes are scoped to a private owner. They make the existing
// authoring checks recognize self/forward object references without installing
// a registry, extending the live ledger or validating an unfinished closure.
func (b *Build) BeginAssetBootstrap(assets []platform.ReleaseAsset) (func(), error) {
	saved, savedMode := b.jointEntities, b.jointMode
	savedAssets := b.jointAssets
	savedDeclarations := b.jointDeclarations
	savedIDs := b.releaseDraftIDs
	available := map[platform.AssetRef]bool{}
	shapes := maps.Clone(saved)
	if shapes == nil {
		shapes = map[string]platform.EntityInfo{}
	}
	for _, asset := range assets {
		available[asset.Ref] = true
		if asset.Ref.App != construction.ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		code, err := b.retainedCodeObject(asset)
		if err != nil {
			return nil, err
		}
		if code {
			continue
		}
		var object construction.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil || construction.TypeOf(object.Name) != asset.Ref.Name {
			return nil, fmt.Errorf("invalid bootstrap object shape %s", asset.Ref)
		}
		shape, err := platform.Describe(construction.ID, Entity(object), func(reflect.Type) string { return "" })
		if err != nil {
			return nil, err
		}
		shapes[asset.Ref.Name] = shape
	}
	b.jointEntities, b.jointMode = shapes, true
	b.jointAssets = available
	b.jointDeclarations = map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range assets {
		b.jointDeclarations[asset.Ref] = asset
	}
	b.releaseDraftIDs = maps.Clone(savedIDs)
	if b.releaseDraftIDs == nil {
		b.releaseDraftIDs = map[platform.AssetRef]string{}
	}
	return func() {
		b.jointEntities, b.jointMode, b.jointAssets, b.releaseDraftIDs, b.jointDeclarations = saved, savedMode, savedAssets, savedIDs, savedDeclarations
	}, nil
}

// DefinitionMetadataKind identifies ordinary accepted metadata whose discovery
// is governed by its original metadata record. Executable Build definitions
// keep their installed descriptors; agent and observation projection remains
// narrower than this ordinary record visibility check.
func (*Build) DefinitionMetadataKind(kind platform.AssetKind) bool {
	return slices.Contains([]platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetMigration, platform.AssetEvaluation, platform.AssetTestPlan, platform.AssetSampleSet, platform.AssetObservation, platform.AssetAlert}, kind)
}

func (*Build) DefinitionDescriptor(asset platform.ReleaseAsset) (platform.Definition, error) {
	definition := platform.Definition{Ref: asset.Ref, Source: "builder", Version: asset.SourceVersion, ContractVersion: asset.ContractVersion, Requires: slices.Clone(asset.Requires), Provides: slices.Clone(asset.Provides)}
	var err error
	switch asset.Ref.Kind {
	case platform.AssetAlert:
		var alert platform.AlertRule
		if json.Unmarshal(asset.Body, &alert) != nil || alert.Check() != nil {
			return definition, fmt.Errorf("invalid accepted alert rule")
		}
		definition.Alert = &alert
	case platform.AssetObservation:
		var observation platform.ObservationDefinition
		if err = json.Unmarshal(asset.Body, &observation); err == nil {
			err = observation.Check()
		}
		definition.Observation = &observation
	case platform.AssetAgent:
		var descriptor platform.AgentReleaseDescriptor
		if json.Unmarshal(asset.Body, &descriptor) != nil || descriptor.Check() != nil {
			return definition, fmt.Errorf("agent descriptor is unavailable")
		}
		definition.Agent = &descriptor
	}
	if err != nil {
		return definition, err
	}
	if definition.Provides == nil {
		descriptor, err := platform.DescribeAsset(asset)
		if err != nil {
			return definition, err
		}
		definition.Provides = descriptor.Provides
	}
	return definition, nil
}

// equalAsset reports whether two frozen assets carry the same bytes.
func equalAsset(a, b platform.ReleaseAsset) bool {
	return a.Ref == b.Ref && a.SourceVersion == b.SourceVersion && bytes.Equal(a.Body, b.Body)
}
