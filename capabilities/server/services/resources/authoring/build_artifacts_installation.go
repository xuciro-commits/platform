package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
	dataowner "platformserver/services/data"
)

func (b *Build) DecodeAssetInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	descriptor, known := resourceDescriptorOfSchema(schema)
	if !known {
		return platform.AssetInstallation{}, fmt.Errorf("invalid publication schema")
	}
	if descriptor.family == integrationResources {
		return b.data.DecodeInstallation(schema, image)
	}
	if descriptor.kind == platform.AssetSampleSet {
		return sampleSetInstallation(image)
	}
	if descriptor.typ == automationauthoring.ProcessType || descriptor.typ == automationauthoring.AlertType {
		return b.automation.DecodeInstallation(schema, image)
	}
	if b.apps.HandlesPublication(schema) {
		return b.apps.DecodeInstallation(schema, image)
	}
	var record struct {
		platform.Record
		Name string `json:"name"`
	}
	if !descriptor.acceptedPublication || json.Unmarshal(image, &record) != nil || record.ID == "" {
		return platform.AssetInstallation{}, fmt.Errorf("invalid release publication row")
	}
	ref, err := draftAssetRef(descriptor.typ, record.Name)
	if err != nil {
		return platform.AssetInstallation{}, err
	}
	switch schema {
	case SchemaEvaluationSetPublish:
		_, asset, err := b.testPlanImage(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		ref = asset.Ref
	case SchemaTablePublish:
		r, err := tableImage(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		ref = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: r.Name}
	}
	return platform.AssetInstallation{Ref: ref, Schema: schema, Record: descriptor.record(record.ID), Image: slices.Clone(image)}, nil
}

func (b *Build) ValidateAssetInstallation(plan platform.AssetInstallation) error {
	decoded, err := b.DecodeAssetInstallation(plan.Schema, plan.Image)
	if err != nil {
		return err
	}
	if decoded.Ref != plan.Ref || decoded.Record != plan.Record {
		return fmt.Errorf("installation identity differs from its saved image")
	}
	if plan.Ref.Kind == platform.AssetSampleSet {
		return nil
	}
	if !dataowner.ValueIntegrationKind(plan.Ref.Kind) {
		return b.ValidateAcceptedPublication(plan.Schema, plan.Image)
	}
	return b.data.ValidateInstallation(plan)
}

func (b *Build) ApplyAssetInstallation(plan platform.AssetInstallation, ctx platform.AssetApplyContext) error {
	if dataowner.ValueIntegrationKind(plan.Ref.Kind) {
		return b.data.ApplyInstallation(plan, ctx)
	}
	if plan.Ref.Kind == platform.AssetSampleSet {
		_, err := b.DecodeAssetInstallation(plan.Schema, plan.Image)
		return err
	}
	if ctx.Private {
		saved := b.ledger
		b.installationCatalog = platform.NewCatalog(b.Manifest().Actions.All()...)
		b.ledger = platform.NewLedger("private-release", ID, b.installationCatalog)
		defer func() { b.ledger = saved }()
	}
	return b.ApplyAcceptedPublication(plan.Schema, plan.Image)
}

var _ platform.AssetLifecycleOwner = (*Build)(nil)

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
		return nil, fmt.Errorf("installation lacks its candidate descriptor: %v", slices.SortedFunc(maps.Keys(byRef), func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) }))
	}
	return out, nil
}

func draftAssetRef(typ, name string) (platform.AssetRef, error) {
	descriptor, ok := resourceDescriptorOfType(typ)
	if !ok {
		return platform.AssetRef{}, fmt.Errorf("unknown native release draft %s", typ)
	}
	if descriptor.qualifiedName {
		name = TypeOf(name)
	}
	return platform.AssetRef{App: descriptor.app, Kind: descriptor.kind, Name: name}, nil
}

func (b *Build) declarationReleaseDraft(asset platform.ReleaseAsset) (string, any, error) {
	if k := definitionKindOf(asset.Ref.Kind); k != nil {
		return k.draft(asset)
	}
	if asset.Ref.Kind == platform.AssetFunction {
		return b.functions.Draft(asset)
	}
	switch asset.Ref.Kind {
	case platform.AssetSampleSet:
		d, _, err := decodeSampleAsset(asset)
		return SampleSetType, SampleSetRecord{Name: d.Name, Title: d.Title, Samples: d.Samples}, err
	case platform.AssetEvaluation, platform.AssetTestPlan:
		d, err := b.testPlanDraft(asset)
		return TestPlanType, d, err
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
			return TableType, table, err
		}
		rows, err := b.codeInventory()
		if err != nil {
			return "", nil, err
		}
		for _, record := range rows {
			if record.Name == asset.Ref.Name && !record.Archived {
				return CodeType, record, nil
			}
		}
		return "", nil, fmt.Errorf("compute %s needs its original compiled target owner; a module descriptor does not carry source or compilation evidence", asset.Ref)
	case platform.AssetAlert:
		typ, record, handled, err := b.automation.Draft(asset)
		if !handled {
			return "", nil, fmt.Errorf("alert asset has no automation owner")
		}
		return typ, record, err
	}
	return "", nil, nil
}

func (b *Build) BeginAssetBootstrap(assets []platform.ReleaseAsset) (func(), error) {
	saved, savedMode := b.jointEntities, b.jointMode
	savedAssets := b.jointAssets
	savedDeclarations := b.jointDeclarations
	available := map[platform.AssetRef]bool{}
	shapes := maps.Clone(saved)
	if shapes == nil {
		shapes = map[string]platform.EntityInfo{}
	}
	for _, asset := range assets {
		available[asset.Ref] = true
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		code, err := b.retainedCodeObject(asset)
		if err != nil {
			return nil, err
		}
		if code {
			continue
		}
		var object Object
		if err := json.Unmarshal(asset.Body, &object); err != nil || TypeOf(object.Name) != asset.Ref.Name {
			return nil, fmt.Errorf("invalid bootstrap object shape %s", asset.Ref)
		}
		shape, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
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
	restoreTargets, err := b.data.BeginTargets(assets, platform.AssetReleaseContext{})
	if err != nil {
		b.jointEntities, b.jointMode, b.jointAssets, b.jointDeclarations = saved, savedMode, savedAssets, savedDeclarations
		return nil, err
	}
	return func() {
		restoreTargets()
		b.jointEntities, b.jointMode, b.jointAssets, b.jointDeclarations = saved, savedMode, savedAssets, savedDeclarations
	}, nil
}
