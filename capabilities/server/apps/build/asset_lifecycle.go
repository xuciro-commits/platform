package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	buildapi "platformserver/internal/host/buildapi"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

func (*Build) AssetKinds() []platform.AssetKind {
	return []platform.AssetKind{
		platform.AssetObject, platform.AssetAction, platform.AssetPage, platform.AssetApp, platform.AssetFlow, platform.AssetPropertyType, platform.AssetLinkType, platform.AssetQuery, platform.AssetFunction, platform.AssetCompute,
		platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetMigration, platform.AssetAgent, platform.AssetEvaluation, platform.AssetSampleSet, platform.AssetObservation,
	}
}
func (b *Build) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	if ref.App != ID {
		return platform.ReleaseAsset{}, fmt.Errorf("asset has another owner")
	}
	if len(contentVersion) > 1 || len(contentVersion) == 1 && ref.Kind != platform.AssetPage {
		return platform.ReleaseAsset{}, fmt.Errorf("content pins only identify one retained page")
	}
	if buildapi.ValueIntegrationKind(ref.Kind) {
		rows, err := b.integrationInventory(ref.Kind)
		if err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, r := range rows {
			name, ordinal, accepted, history := integrationMetadata(r)
			if name != ref.Name {
				continue
			}
			for _, raw := range append(slices.Clone(history), accepted) {
				if raw == "" {
					continue
				}
				var a platform.ReleaseAsset
				if err := buildapi.ValueDecodeIntegration([]byte(raw), &a); err != nil {
					return a, err
				}
				if a.Ref == ref && a.SourceVersion == version {
					return a, nil
				}
			}
			if ordinal == 0 && accepted == "" && version == integrationVersion(1, ref.Kind) {
				return b.integrationAsset(ref.Kind, r, 1)
			}
		}
		return platform.ReleaseAsset{}, fmt.Errorf("integration version %s@%s is not retained", ref, version)
	}
	if len(contentVersion) == 1 {
		if version != b.Manifest().Version {
			return platform.ReleaseAsset{}, fmt.Errorf("unsupported page source version")
		}
		if page, ok := b.PageContent(ref.Name, contentVersion[0]); ok {
			return platform.PageReleaseAsset(ID, version, page)
		}
		return platform.ReleaseAsset{}, fmt.Errorf("page content %s@%s is not retained", ref, contentVersion[0])
	}
	if ref.Kind == platform.AssetSampleSet {
		return b.sampleSetPinned(ref, version)
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
	if ref.Kind == platform.AssetEvaluation {
		return b.evaluationSetPinned(ref, version)
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
		return platform.ReleaseAsset{}, fmt.Errorf("agent version is not installed; use its retained candidate")
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
	assets, err := b.ReleaseAssets()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, asset := range assets {
		if asset.Ref == ref && asset.SourceVersion == version {
			return asset, nil
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("asset version %s@%s is unavailable", ref, version)
}
func (b *Build) AssetEnvironmentRequirements(asset platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	if !buildapi.ValueIntegrationKind(asset.Ref.Kind) {
		return nil, nil
	}
	var req platform.AssetEnvironmentRequirement
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		var d ConnectionDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || !slices.Contains(ConnectionKinds, d.Kind) {
			return nil, fmt.Errorf("invalid connection binding descriptor")
		}
		req = platform.AssetEnvironmentRequirement{Ref: asset.Ref, Profile: d.Kind, Credential: d.Credential}
	case platform.AssetSource:
		var d SourceDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		kind := "http"
		if d.Profile == "odata" {
			kind = "odata"
		}
		if d.Profile == "table" {
			kind = "postgres"
		}
		if d.Connection == "" {
			return nil, fmt.Errorf("source has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: d.Connection}, Profile: kind}
	case platform.AssetObservation:
		var d platform.ObservationDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Connection.Kind != platform.AssetConnection || d.Connection.Name == "" {
			return nil, fmt.Errorf("observation source has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: d.Connection, Profile: "http"}
	case platform.AssetWriteback:
		var d WritebackDefinition
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Connection == "" {
			return nil, fmt.Errorf("writeback has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: d.Connection}}
	default:
		return nil, nil
	}
	return []platform.AssetEnvironmentRequirement{req}, nil
}

func (b *Build) integrationTargets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) (func(), error) {
	saved := b.releaseDraftIDs
	b.releaseDraftIDs = maps.Clone(saved)
	if b.releaseDraftIDs == nil {
		b.releaseDraftIDs = map[platform.AssetRef]string{}
	}
	restore := func() { b.releaseDraftIDs = saved }
	for _, asset := range assets {
		if asset.Ref.App != ID || !buildapi.ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		if asset.Ref.Kind == platform.AssetConnection {
			var target string
			for _, binding := range ctx.Bindings {
				if binding.Ref == asset.Ref {
					if target != "" {
						restore()
						return nil, fmt.Errorf("connection binding is duplicated")
					}
					target = binding.TargetRecord
				}
			}
			conn, ok := platform.Get[Connection](b.host.Automation(platform.Caller{}, ID), target)
			var d ConnectionDefinition
			if target == "" || !ok || buildapi.ValueDecodeIntegration(asset.Body, &d) != nil || conn.Name != asset.Ref.Name || conn.Kind != d.Kind || conn.State != "ready" || conn.Archived || d.Credential && conn.Secret == "" {
				restore()
				return nil, fmt.Errorf("connection %s needs a ready target binding with matching kind and credential", asset.Ref)
			}
			if !reflect.DeepEqual(buildapi.PortableDriverBinding(conn.Protocol), d.Protocol) {
				restore()
				return nil, fmt.Errorf("connection %s needs the same protocol package, profile and logical point shape", asset.Ref)
			}
			if conn.Protocol != nil {
				if err := b.validConnection(conn); err != nil {
					restore()
					return nil, fmt.Errorf("connection %s: %s", asset.Ref, err.Message)
				}
			}
			b.releaseDraftIDs[asset.Ref] = target
		} else {
			rows, err := b.integrationInventory(asset.Ref.Kind)
			if err != nil {
				restore()
				return nil, err
			}
			id := asset.Ref.Name
			for _, r := range rows {
				name, _, _, _ := integrationMetadata(r)
				if name == asset.Ref.Name {
					id = integrationRecord(r).ID
					break
				}
			}
			b.releaseDraftIDs[asset.Ref] = id
		}
	}
	return restore, nil
}
func (b *Build) ReleaseDrafts(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	ordered, err := orderOwnerAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	restore, err := b.integrationTargets(assets, ctx)
	if err != nil {
		return nil, err
	}
	defer restore()
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
		if asset.Ref.App != ID || !buildapi.ValueIntegrationKind(asset.Ref.Kind) || asset.Ref.Kind == platform.AssetConnection {
			continue
		}
		record, err := b.integrationConfiguration(asset)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		typ := integrationType(asset.Ref.Kind)
		out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: typ, Name: asset.Ref.Name, Schema: typ + ".create", Image: raw})
	}
	for _, asset := range assets {
		if asset.Ref.App != ID {
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
	if err := b.validateReleaseMember(assets, ctx.Member); err != nil {
		return nil, err
	}
	restore, err := b.integrationTargets(assets, ctx)
	if err != nil {
		return nil, err
	}
	defer restore()
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
	samples, err := b.prepareSampleSetInstallations(assets)
	if err != nil {
		return nil, err
	}
	out = append(out, samples...)
	for _, p := range publications {
		plan, err := b.DecodeAssetInstallation(p.Schema, p.Image)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	for _, asset := range assets {
		if asset.Ref.App != ID || !buildapi.ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		rows, err := b.integrationInventory(asset.Ref.Kind)
		if err != nil {
			return nil, err
		}
		var current any
		for _, r := range rows {
			name, _, _, _ := integrationMetadata(r)
			if name == asset.Ref.Name {
				current = r
				break
			}
		}
		if current == nil {
			return nil, fmt.Errorf("asset %s has no target draft", asset.Ref)
		}
		record, err := retainIntegration(current, asset)
		if err != nil {
			return nil, err
		}
		switch r := record.(type) {
		case Source:
			prior := current.(Source)
			if prior.Published != r.Published || prior.State != "published" {
				var d SourceDefinition
				if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
					return nil, err
				}
				if !d.Stream {
					r.Requested = true
				}
			}
			r.State, r.Puller = "published", ctx.Member.ID
			record = r
		case Pipeline:
			prior := current.(Pipeline)
			if prior.Published != r.Published || prior.State != "published" {
				r.Requested = true
			}
			r.State, r.Runner = "published", ctx.Member.ID
			record = r
		case Writeback:
			r.State, r.Publisher = "published", ctx.Member.ID
			record = r
		case Observation:
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return nil, err
			}
			fixed := configuration.(Observation)
			fixed.Record, fixed.State, fixed.Publisher = r.Record, "published", ctx.Member.ID
			fixed.PublisherScopes = string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			record = fixed
		case Match:
			r.State, r.Publisher = "published", ctx.Member.ID
			record = r
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if before, _ := json.Marshal(current); bytes.Equal(before, raw) {
			continue
		}
		out = append(out, platform.AssetInstallation{Ref: asset.Ref, Schema: integrationType(asset.Ref.Kind) + ".publish", Record: integrationType(asset.Ref.Kind) + "/" + integrationRecord(record).ID, Image: raw})
	}
	return orderOwnerInstallations(assets, out)
}
func (b *Build) DecodeAssetInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	if schema == SchemaSampleSet {
		return sampleSetInstallation(image)
	}
	if schema == SchemaMigrationPlan {
		return migrationPlanInstallation(image)
	}
	typ, _, _ := strings.Cut(schema, ".publish")
	if schema != typ+".publish" {
		return platform.AssetInstallation{}, fmt.Errorf("invalid publication schema")
	}
	if kind := integrationKindOf(typ); kind != "" {
		var record any
		switch kind {
		case platform.AssetConnection:
			record = new(Connection)
		case platform.AssetSource:
			record = new(Source)
		case platform.AssetDataset:
			record = new(Dataset)
		case platform.AssetPipeline:
			record = new(Pipeline)
		case platform.AssetWriteback:
			record = new(Writeback)
		case platform.AssetObservation:
			record = new(Observation)
		case platform.AssetMatching:
			record = new(Match)
		}
		if err := buildapi.ValueDecodeIntegration(image, record); err != nil {
			return platform.AssetInstallation{}, err
		}
		switch r := record.(type) {
		case *Connection:
			record = *r
		case *Source:
			record = *r
		case *Dataset:
			record = *r
		case *Pipeline:
			record = *r
		case *Writeback:
			record = *r
		case *Observation:
			record = *r
		case *Match:
			record = *r
		}
		name, version, raw, _ := integrationMetadata(record)
		var asset platform.ReleaseAsset
		if integrationRecord(record).ID == "" || version < 1 || buildapi.ValueDecodeIntegration([]byte(raw), &asset) != nil || asset.Ref != (platform.AssetRef{App: ID, Kind: kind, Name: name}) {
			return platform.AssetInstallation{}, fmt.Errorf("invalid integration installation")
		}
		if err := validateIntegrationFamily(record, asset); err != nil {
			return platform.AssetInstallation{}, err
		}
		if err := buildapi.ValueValidateIntegrationDescriptor(asset); err != nil {
			return platform.AssetInstallation{}, err
		}
		return platform.AssetInstallation{Ref: asset.Ref, Schema: schema, Record: typ + "/" + integrationRecord(record).ID, Image: slices.Clone(image)}, nil
	}
	typ, id, err := publicationRecord(ReleasePublication{Schema: schema, Image: image})
	if err != nil {
		return platform.AssetInstallation{}, err
	}
	var ref platform.AssetRef
	switch schema {
	case SchemaPublish:
		var r Object
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: buildapi.TypeOf(r.Name)}
	case SchemaRelease:
		var r Page
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetPage, Name: r.Name}
	case SchemaHandOver:
		var r Application
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetApp, Name: r.Name}
	case SchemaProcess:
		var r Process
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetFlow, Name: buildapi.TypeOf(r.Name)}
	case SchemaPropertyType:
		var r PropertyType
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetPropertyType, Name: r.Name}
	case SchemaLinkType:
		var r LinkType
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetLinkType, Name: r.Name}
	case SchemaQuery:
		var r Query
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: r.Name}
	case SchemaFunction:
		var r Function
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: r.Name}
	case SchemaEvaluationSetPublish:
		r, asset, err := b.evaluationSetImage(image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		ref = asset.Ref
		_ = r
	case SchemaAgentPublish:
		var r Agent
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetAgent, Name: r.Name}
	case SchemaCodePublish:
		var r Code
		_ = json.Unmarshal(image, &r)
		ref = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: r.Name}
	default:
		return platform.AssetInstallation{}, fmt.Errorf("unsupported publication %s", schema)
	}
	return platform.AssetInstallation{Ref: ref, Schema: schema, Record: typ + "/" + id, Image: slices.Clone(image)}, nil
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
	if !buildapi.ValueIntegrationKind(plan.Ref.Kind) {
		return b.ValidateAcceptedPublication(plan.Schema, plan.Image)
	}
	record, err := integrationInstallationRecord(plan.Ref.Kind, plan.Image)
	if err != nil {
		return err
	}
	var actor string
	switch r := record.(type) {
	case Source:
		actor = r.Puller
	case Pipeline:
		actor = r.Runner
	case Writeback:
		actor = r.Publisher
	case Observation:
		actor = r.Publisher
	case Match:
		actor = r.Publisher
	}
	var c platform.Caller
	if actor != "" {
		member, ok := b.host.Member(actor)
		if !ok || !member.Active() || !member.InScope(plan.Schema) || !member.May(ID, plan.Schema, []string{Builder, Integrator}).Allow {
			return fmt.Errorf("integration execution member lacks publication permission")
		}
		c = b.host.Caller(platform.Caller{}, member, ID)
	} else if plan.Ref.Kind != platform.AssetConnection && plan.Ref.Kind != platform.AssetDataset {
		return fmt.Errorf("integration execution member is missing")
	}
	writes := func(object string) error {
		if object == "" {
			return nil
		}
		for _, verb := range []string{"create", "edit"} {
			schema := object + "." + verb
			owner, action, ok := b.host.Action(schema)
			if !ok || !c.Member.Active() || !c.Member.InScope(schema) || !c.May(owner, schema, action.Roles).Allow {
				return fmt.Errorf("integration execution member cannot %s", schema)
			}
		}
		return nil
	}
	switch r := record.(type) {
	case Connection:
		_, err = b.ExecutionConnection(r)
	case Dataset:
		_, err = b.ExecutionDataset(r)
	case Source:
		fixed, e := b.ExecutionSource(r)
		if e != nil {
			return e
		}
		if e := b.checkSource(c, fixed); e != nil {
			return e
		}
		err = writes(fixed.Object)
	case Pipeline:
		fixed, e := b.ExecutionPipeline(r)
		if e != nil {
			return e
		}
		if e := b.checkPipeline(c, fixed); e != nil {
			return e
		}
		err = writes(fixed.OutputObject)
	case Writeback:
		fixed, e := b.ExecutionWriteback(r)
		if e != nil {
			return e
		}
		if e := b.checkWriteback(c, fixed); e != nil {
			return e
		}
		if len(fixed.Result) > 0 {
			schema := fixed.Object + ".edit"
			owner, action, ok := b.host.Action(schema)
			if !ok || !c.Member.InScope(schema) || !c.May(owner, schema, action.Roles).Allow {
				return fmt.Errorf("writeback execution member cannot apply answer fields")
			}
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
	var ptr any
	switch kind {
	case platform.AssetConnection:
		ptr = new(Connection)
	case platform.AssetSource:
		ptr = new(Source)
	case platform.AssetDataset:
		ptr = new(Dataset)
	case platform.AssetPipeline:
		ptr = new(Pipeline)
	case platform.AssetWriteback:
		ptr = new(Writeback)
	case platform.AssetObservation:
		ptr = new(Observation)
	case platform.AssetMatching:
		ptr = new(Match)
	default:
		return nil, fmt.Errorf("unknown integration kind")
	}
	if err := buildapi.ValueDecodeIntegration(raw, ptr); err != nil {
		return nil, err
	}
	return reflect.ValueOf(ptr).Elem().Interface(), nil
}
func (b *Build) ApplyAssetInstallation(plan platform.AssetInstallation, ctx platform.AssetApplyContext) error {
	if ctx.Validate {
		if ctx.Upgrade {
			if err := b.validateIntegrationUpgrade(plan); err != nil {
				return err
			}
		}
	}
	if buildapi.ValueIntegrationKind(plan.Ref.Kind) || (plan.Ref.Kind == platform.AssetMigration || plan.Ref.Kind == platform.AssetSampleSet) {
		_, err := b.DecodeAssetInstallation(plan.Schema, plan.Image)
		return err
	}
	if ctx.Private {
		saved := b.ledger
		b.ledger = platform.NewLedger("private-release", ID, platform.NewCatalog(saved.Catalog.All()...))
		defer func() { b.ledger = saved }()
	}
	return b.ApplyAcceptedPublication(plan.Schema, plan.Image)
}
func (b *Build) validateIntegrationUpgrade(plan platform.AssetInstallation) error {
	if !buildapi.ValueIntegrationKind(plan.Ref.Kind) {
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
	permission := func(schema string) error {
		if !member.Active() || !member.InScope(schema) {
			return fmt.Errorf("release execution member cannot %s", schema)
		}
		return nil
	}
	writes := func(object string, verbs ...string) error {
		for _, verb := range verbs {
			if err := permission(object + "." + verb); err != nil {
				return err
			}
		}
		return nil
	}
	for _, asset := range assets {
		if asset.Ref.App != ID || !buildapi.ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetSource:
			var d SourceDefinition
			if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(SourceType + ".publish"); err != nil {
				return err
			}
			if d.Object != "" {
				if err := writes(d.Object, "create", "edit"); err != nil {
					return err
				}
			}
			if d.Dataset != "" {
				if err := permission(SchemaDatasetLoad); err != nil {
					return err
				}
			}
		case platform.AssetPipeline:
			var d PipelineDefinition
			if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(PipelineType + ".publish"); err != nil {
				return err
			}
			if d.OutputObject != "" {
				if err := writes(d.OutputObject, "create", "edit"); err != nil {
					return err
				}
			}
			if d.OutputDataset != "" {
				if err := permission(SchemaDatasetLoad); err != nil {
					return err
				}
			}
		case platform.AssetWriteback:
			var d WritebackDefinition
			if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(WritebackType + ".publish"); err != nil {
				return err
			}
			if len(d.Result) > 0 {
				if err := writes(d.Object, "edit"); err != nil {
					return err
				}
			}
		case platform.AssetObservation:
			var d platform.ObservationDefinition
			if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(SchemaObservation); err != nil {
				return err
			}
			if d.Decision != nil {
				if err := permission(d.Decision.Action.Name); err != nil {
					return err
				}
				owner, action, ok := b.observationAction(d)
				if !ok || owner != d.Decision.Action.App || !member.May(owner, action.Schema, action.Roles).Allow {
					return fmt.Errorf("release execution member cannot invoke the declared observation decision")
				}
			}
		case platform.AssetMatching:
			if err := permission(MatchType + ".publish"); err != nil {
				return err
			}
		}
	}
	return nil
}

func draftAssetRef(typ, name string) (platform.AssetRef, error) {
	kinds := map[string]platform.AssetKind{ObjectType: platform.AssetObject, PageType: platform.AssetPage, AppType: platform.AssetApp, ProcessType: platform.AssetFlow, MigrationPlanType: platform.AssetMigration, SampleSetType: platform.AssetSampleSet}
	kind := kinds[typ]
	if kind == "" {
		return platform.AssetRef{}, fmt.Errorf("unknown native release draft %s", typ)
	}
	if kind == platform.AssetObject || kind == platform.AssetFlow {
		name = buildapi.TypeOf(name)
	}
	return platform.AssetRef{App: ID, Kind: kind, Name: name}, nil
}
func (b *Build) declarationReleaseDraft(asset platform.ReleaseAsset) (string, any, error) {
	switch asset.Ref.Kind {
	case platform.AssetSampleSet:
		d, _, err := decodeSampleAsset(asset)
		return SampleSetType, SampleSetRecord{Name: d.Name, Title: d.Title, Samples: d.Samples}, err
	case platform.AssetMigration:
		d, _, err := decodeMigrationAsset(asset)
		if err != nil {
			return "", nil, err
		}
		return MigrationPlanType, MigrationPlanRecord{Name: d.Name, Title: d.Title, Plan: d.Plan}, nil
	case platform.AssetPropertyType:
		var d platform.PropertyType
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return PropertyTypeType, propertyTypeFromDeclaration(d), nil
	case platform.AssetLinkType:
		var d platform.LinkType
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return LinkTypeType, linkTypeFromDeclaration(d), nil
	case platform.AssetQuery:
		var d Query
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return QueryType, d, nil
	case platform.AssetFunction:
		var d Function
		if err := buildapi.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return FunctionType, d, nil
	case platform.AssetEvaluation:
		d, err := b.evaluationSetDraft(asset)
		return TestPlanType, d, err
	case platform.AssetAgent:
		d, err := decodeAgentAsset(asset)
		if err != nil {
			return "", nil, err
		}
		d.Version, d.State = 0, "draft"
		return AgentType, d, nil
	case platform.AssetCompute:
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
	}
	return "", nil, nil
}

// Bootstrap shapes are scoped to a private owner. They make the existing
// authoring checks recognize self/forward object references without installing
// a registry, extending the live ledger or validating an unfinished closure.
func (b *Build) BeginAssetBootstrap(assets []platform.ReleaseAsset) (func(), error) {
	saved, savedMode := b.jointEntities, b.jointMode
	savedAssets := b.jointAssets
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
		if err := json.Unmarshal(asset.Body, &object); err != nil || buildapi.TypeOf(object.Name) != asset.Ref.Name {
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
	return func() { b.jointEntities, b.jointMode, b.jointAssets = saved, savedMode, savedAssets }, nil
}
