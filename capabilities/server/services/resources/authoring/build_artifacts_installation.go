package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/services/ontology"
	"reflect"
	"slices"

	"platformserver/platform"
	automationauthoring "platformserver/services/automate/authoring"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/publication"
)

func (b *Build) DecodeAssetInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	descriptor, known := resourceDescriptorOfSchema(schema)
	if !known {
		return platform.AssetInstallation{}, fmt.Errorf("invalid publication schema")
	}
	if descriptor.family == integrationResources {
		return b.data.DecodeInstallation(schema, image)
	}
	if hooks, found := publication.Kind(b.publications, descriptor.kind); found && hooks.DirectInstallation != nil {
		return hooks.DirectInstallation(image)
	}
	if descriptor.typ == automationauthoring.ProcessType {
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
	if hooks, found := publication.Schema(b.publications, schema); found && hooks.Decode != nil {
		ref, err = hooks.Decode(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
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
	if hooks, found := publication.Kind(b.publications, plan.Ref.Kind); found && hooks.DirectInstallation != nil {
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
	if hooks, found := publication.Kind(b.publications, plan.Ref.Kind); found && hooks.DirectInstallation != nil {
		_, err := b.DecodeAssetInstallation(plan.Schema, plan.Image)
		return err
	}
	if ctx.Private {
		saved := b.ledger
		b.installationCatalog = platform.NewCatalog(b.Manifest().Actions.All()...)
		b.ledger = b.authorities("private-release", ID, b.installationCatalog)
		defer func() { b.ledger = saved }()
	}
	return b.ApplyAcceptedPublication(plan.Schema, plan.Image)
}

var _ platform.AssetLifecycleOwner = (*Build)(nil)

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
		d, err := b.sampleSets.Draft(asset)
		return SampleSetType, d, err
	case platform.AssetEvaluation, platform.AssetTestPlan:
		d, err := b.testPlans.Draft(asset)
		return TestPlanType, d, err
	case platform.AssetAgent:
		d, err := b.agent.DraftFromAsset(asset)
		if err != nil {
			return "", nil, err
		}
		return AgentType, d, nil
	case platform.AssetCompute:
		return b.functions.OperationDraft(asset)
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
		var object ontology.Object
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
