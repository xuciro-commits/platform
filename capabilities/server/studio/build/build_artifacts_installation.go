package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

func (b *Build) DecodeAssetInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	if schema == SchemaSampleSet {
		return sampleSetInstallation(image)
	}
	typ, _, _ := strings.Cut(schema, ".publish")
	if schema != typ+".publish" {
		return platform.AssetInstallation{}, fmt.Errorf("invalid publication schema")
	}
	if kind := integrationKindOf(typ); kind != "" {
		record, err := integrationInstallationRecord(kind, image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		name, version, raw, _ := integrationMetadata(record)
		var asset platform.ReleaseAsset
		if integrationRecord(record).ID == "" || version < 1 || ValueDecodeIntegration([]byte(raw), &asset) != nil || asset.Ref != (platform.AssetRef{App: ID, Kind: kind, Name: name}) {
			return platform.AssetInstallation{}, fmt.Errorf("invalid integration installation")
		}
		if err := validateIntegrationFamily(record, asset); err != nil {
			return platform.AssetInstallation{}, err
		}
		if err := ValueValidateIntegrationDescriptor(asset); err != nil {
			return platform.AssetInstallation{}, err
		}
		return platform.AssetInstallation{Ref: asset.Ref, Schema: schema, Record: typ + "/" + integrationRecord(record).ID, Image: slices.Clone(image)}, nil
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
	case SchemaAlertPublish:
		_, asset, err := alertImage(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		ref = asset.Ref
	}
	return platform.AssetInstallation{Ref: ref, Schema: schema, Record: typ + "/" + record.ID, Image: slices.Clone(image)}, nil
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
	if !ValueIntegrationKind(plan.Ref.Kind) {
		return b.ValidateAcceptedPublication(plan.Schema, plan.Image)
	}
	record, err := integrationInstallationRecord(plan.Ref.Kind, plan.Image)
	if err != nil {
		return err
	}
	c := b.host.Automation(platform.Caller{}, ID)
	switch r := record.(type) {
	case Connection:
		_, err = b.ConnectionConfiguration(r)
	case Dataset:
		_, err = b.ExecutionDataset(r)
	case Listener:
		_, err = b.ExecutionListener(r)
	case Source:
		fixed, e := b.ExecutionSource(r)
		if e != nil {
			return e
		}
		if e := b.checkSource(c, fixed); e != nil {
			return e
		}
	case Pipeline:
		fixed, e := b.ExecutionPipeline(r)
		if e != nil {
			return e
		}
		if e := b.checkPipeline(c, fixed); e != nil {
			return e
		}
	case Writeback:
		fixed, e := b.ExecutionWriteback(r)
		if e != nil {
			return e
		}
		if e := b.checkWriteback(c, fixed); e != nil {
			return e
		}

	case Observation:
		if _, e := b.ExecutionObservation(r); e != nil {
			return e
		}
		configuration, e := b.acceptedIntegration(r, platform.AssetObservation)
		if e != nil {
			return e
		}
		fixed := configuration.(Observation)
		fixed.Record, fixed.State, fixed.Publisher = r.Record, r.State, r.Publisher
		fixed.PublisherScopes = r.PublisherScopes
		fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
		err = b.checkObservation(c, fixed)
	case Match:
		fixed, e := b.ExecutionMatching(r)
		if e != nil {
			return e
		}
		if e := b.checkMatch(c, fixed); e != nil {
			return e
		}
	}
	return err
}

func integrationInstallationRecord(kind platform.AssetKind, raw []byte) (any, error) {
	typ := integrationRecordType(kind)
	if typ == nil {
		return nil, fmt.Errorf("unknown integration kind")
	}
	ptr := reflect.New(typ)
	if err := ValueDecodeIntegration(raw, ptr.Interface()); err != nil {
		return nil, err
	}
	return ptr.Elem().Interface(), nil
}

func (b *Build) ApplyAssetInstallation(plan platform.AssetInstallation, ctx platform.AssetApplyContext) error {
	if ctx.Validate {
		if ctx.Upgrade {
			if err := b.validateIntegrationUpgrade(plan); err != nil {
				return err
			}
		}
	}
	if ValueIntegrationKind(plan.Ref.Kind) || plan.Ref.Kind == platform.AssetSampleSet {
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

func (b *Build) validateIntegrationUpgrade(plan platform.AssetInstallation) error {
	if !ValueIntegrationKind(plan.Ref.Kind) {
		return nil
	}
	proposed, err := integrationInstallationRecord(plan.Ref.Kind, plan.Image)
	if err != nil {
		return err
	}
	rows, err := b.integrationInventory(plan.Ref.Kind)
	if err != nil {
		return err
	}
	var current any
	for _, r := range rows {
		if integrationRecord(r).ID == integrationRecord(proposed).ID {
			current = r
			break
		}
	}
	if current == nil {
		return nil
	}
	switch old := current.(type) {
	case Source:
		next, e := b.ExecutionSource(proposed.(Source))
		if e != nil {
			return e
		}
		prior, e := b.ExecutionSource(old)
		if e != nil {
			return e
		}
		if (old.Cursor != "" || old.Last != nil) && (prior.Connection != next.Connection || prior.Profile != next.Profile || prior.URL != next.URL || prior.Filter != next.Filter || prior.Path != next.Path || prior.Stream != next.Stream || prior.Entity != next.Entity || prior.Since != next.Since || prior.Dataset != next.Dataset || prior.Object != next.Object || prior.Key != next.Key || !reflect.DeepEqual(prior.Mapping, next.Mapping)) {
			return fmt.Errorf("source with accepted data or cursor needs an explicit migration before changing its target or identity mapping")
		}
	case Pipeline:
		next, e := b.ExecutionPipeline(proposed.(Pipeline))
		if e != nil {
			return e
		}
		prior, e := b.ExecutionPipeline(old)
		if e != nil {
			return e
		}
		if old.Last != nil && (prior.Input != next.Input || prior.OutputDataset != next.OutputDataset || prior.OutputObject != next.OutputObject || prior.Key != next.Key) {
			return fmt.Errorf("pipeline with accepted data needs an explicit migration before changing its input or output identity")
		}
	}
	return nil
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
		return nil, fmt.Errorf("installation lacks its candidate descriptor")
	}
	return out, nil
}

func draftAssetRef(typ, name string) (platform.AssetRef, error) {
	kinds := map[string]platform.AssetKind{ObjectType: platform.AssetObject, PageType: platform.AssetPage, AppType: platform.AssetApp, ProcessType: platform.AssetFlow, SampleSetType: platform.AssetSampleSet,
		PropertyTypeType: platform.AssetPropertyType, ValueTypeType: platform.AssetValueType, ObjectTypeGroupType: platform.AssetObjectTypeGroup, LinkTypeType: platform.AssetLinkType, QueryType: platform.AssetQuery, FunctionType: platform.AssetFunction, AgentType: platform.AssetAgent,
		CodeType: platform.AssetCompute, TableType: platform.AssetCompute, AlertType: platform.AssetAlert, TestPlanType: platform.AssetTestPlan}
	kind := kinds[typ]
	if kind == "" {
		kind = integrationKindOf(typ)
	}
	if kind == "" {
		return platform.AssetRef{}, fmt.Errorf("unknown native release draft %s", typ)
	}
	if kind == platform.AssetObject || kind == platform.AssetFlow {
		name = TypeOf(name)
	}
	return platform.AssetRef{App: ID, Kind: kind, Name: name}, nil
}

func (b *Build) declarationReleaseDraft(asset platform.ReleaseAsset) (string, any, error) {
	if k := definitionKindOf(asset.Ref.Kind); k != nil {
		return k.draft(asset)
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
		alert, _, err := alertFromAsset(asset)
		alert.State = "draft"
		return AlertType, alert, err
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
	restoreTargets, err := b.integrationTargets(assets, platform.AssetReleaseContext{})
	if err != nil {
		b.jointEntities, b.jointMode, b.jointAssets, b.jointDeclarations = saved, savedMode, savedAssets, savedDeclarations
		return nil, err
	}
	return func() {
		restoreTargets()
		b.jointEntities, b.jointMode, b.jointAssets, b.jointDeclarations = saved, savedMode, savedAssets, savedDeclarations
	}, nil
}
