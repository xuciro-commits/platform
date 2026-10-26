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
		platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetMigration, platform.AssetAgent, platform.AssetEvaluation, platform.AssetTestPlan, platform.AssetSampleSet, platform.AssetObservation, platform.AssetAlert,
	}
}
func (b *Build) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	if ref.App != construction.ID {
		return platform.ReleaseAsset{}, fmt.Errorf("asset has another owner")
	}
	if len(contentVersion) > 1 || len(contentVersion) == 1 && ref.Kind != platform.AssetPage {
		return platform.ReleaseAsset{}, fmt.Errorf("content pins only identify one retained page")
	}
	if construction.ValueIntegrationKind(ref.Kind) {
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
				if err := construction.ValueDecodeIntegration([]byte(raw), &a); err != nil {
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
			return platform.PageReleaseAsset(construction.ID, version, page)
		}
		return platform.ReleaseAsset{}, fmt.Errorf("page content %s@%s is not retained", ref, contentVersion[0])
	}
	if ref.Kind == platform.AssetSampleSet {
		return b.sampleSetPinned(ref, version)
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
	if ref.Kind == platform.AssetEvaluation || ref.Kind == platform.AssetTestPlan {
		return b.testPlanPinned(ref, version)
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
	if !construction.ValueIntegrationKind(asset.Ref.Kind) {
		return nil, nil
	}
	var req platform.AssetEnvironmentRequirement
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		var d construction.ConnectionDefinition
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || !slices.Contains(construction.ConnectionKinds, d.Kind) {
			return nil, fmt.Errorf("invalid connection binding descriptor")
		}
		req = platform.AssetEnvironmentRequirement{Ref: asset.Ref, Profile: d.Kind, Credential: d.Credential}
	case platform.AssetSource:
		var d construction.SourceDefinition
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
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
		req = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetConnection, Name: d.Connection}, Profile: kind}
	case platform.AssetObservation:
		var d platform.ObservationDefinition
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Connection.Kind != platform.AssetConnection || d.Connection.Name == "" {
			return nil, fmt.Errorf("observation source has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: d.Connection, Profile: "http"}
	case platform.AssetWriteback:
		var d construction.WritebackDefinition
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Connection == "" {
			return nil, fmt.Errorf("writeback has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetConnection, Name: d.Connection}}
	default:
		return nil, nil
	}
	return []platform.AssetEnvironmentRequirement{req}, nil
}

func (b *Build) ResolveAssetEnvironment(requirement platform.AssetEnvironmentRequirement) (platform.AssetEnvironmentTarget, error) {
	if requirement.Ref.App != construction.ID || requirement.Ref.Kind != platform.AssetConnection {
		return platform.AssetEnvironmentTarget{}, fmt.Errorf("environment binding %s has no Build connection owner", requirement.Ref)
	}
	connections, _, refusal := platform.Find[construction.Connection](b.host.Automation(platform.Caller{}, construction.ID), platform.Query{})
	if refusal != nil {
		return platform.AssetEnvironmentTarget{}, refusal
	}
	var connection construction.Connection
	for _, candidate := range connections {
		if candidate.Archived || candidate.Name != requirement.Ref.Name {
			continue
		}
		if connection.ID != "" {
			return platform.AssetEnvironmentTarget{}, fmt.Errorf("target environment binding %s is ambiguous", requirement.Ref)
		}
		connection = candidate
	}
	if connection.ID == "" {
		return platform.AssetEnvironmentTarget{}, fmt.Errorf("release needs a target environment binding for %s", requirement.Ref)
	}
	if err := checkEnvironmentConnection(connection, requirement); err != nil {
		return platform.AssetEnvironmentTarget{}, err
	}
	target := platform.AssetEnvironmentTarget{TargetRecord: connection.ID}
	if requirement.Credential {
		target.Credential = connection.Secret
	}
	return target, nil
}

func checkEnvironmentConnection(connection construction.Connection, requirement platform.AssetEnvironmentRequirement) error {
	if connection.Archived || connection.Name != requirement.Ref.Name || connection.State != "ready" || requirement.Profile != "" && connection.Kind != requirement.Profile {
		return fmt.Errorf("target environment binding %s is not ready or has an incompatible connection kind", requirement.Ref)
	}
	if requirement.Credential && connection.Secret == "" {
		return fmt.Errorf("target environment binding %s needs its own credential", requirement.Ref)
	}
	return nil
}

func (b *Build) integrationTargets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) (func(), error) {
	saved := b.releaseDraftIDs
	b.releaseDraftIDs = maps.Clone(saved)
	if b.releaseDraftIDs == nil {
		b.releaseDraftIDs = map[platform.AssetRef]string{}
	}
	restore := func() { b.releaseDraftIDs = saved }
	if ctx.DraftsOnly {
		b.draftIntegrationTargets(assets)
		return restore, nil
	}
	for _, asset := range assets {
		if asset.Ref.App != construction.ID || !construction.ValueIntegrationKind(asset.Ref.Kind) {
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
			conn, ok := platform.Get[construction.Connection](b.host.Automation(platform.Caller{}, construction.ID), target)
			var d construction.ConnectionDefinition
			if target == "" || !ok || construction.ValueDecodeIntegration(asset.Body, &d) != nil {
				restore()
				return nil, fmt.Errorf("connection %s needs a ready target binding with matching kind and credential", asset.Ref)
			}
			if err := checkEnvironmentConnection(conn, platform.AssetEnvironmentRequirement{Ref: asset.Ref, Profile: d.Kind, Credential: d.Credential}); err != nil {
				restore()
				return nil, err
			}
			if !reflect.DeepEqual(construction.PortableDriverBinding(conn.Protocol), d.Protocol) {
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
		if asset.Ref.App != construction.ID || !construction.ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		if asset.Ref.Kind == platform.AssetConnection {
			if !ctx.DraftsOnly {
				continue
			}
			connection, err := portableConnectionDraft(asset)
			if err != nil {
				return nil, err
			}
			out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: construction.ConnectionType, Name: connection.Name, Schema: construction.ConnectionType + ".create", Image: platform.Raw(connection)})
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
		if asset.Ref.App != construction.ID || !construction.ValueIntegrationKind(asset.Ref.Kind) {
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
		case construction.Source:
			prior := current.(construction.Source)
			if prior.Published != r.Published || prior.State != "published" {
				var d construction.SourceDefinition
				if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
					return nil, err
				}
				if !d.Stream {
					r.Requested = true
				}
			}
			r.State, r.Puller = "published", ctx.Member.ID
			record = r
		case construction.Pipeline:
			prior := current.(construction.Pipeline)
			if prior.Published != r.Published || prior.State != "published" {
				r.Requested = true
			}
			r.State, r.Runner = "published", ctx.Member.ID
			record = r
		case construction.Writeback:
			r.State, r.Publisher = "published", ctx.Member.ID
			record = r
		case construction.Observation:
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return nil, err
			}
			fixed := configuration.(construction.Observation)
			fixed.Record, fixed.State, fixed.Publisher = r.Record, "published", ctx.Member.ID
			fixed.PublisherScopes = string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			record = fixed
		case construction.Match:
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
		record, err := integrationInstallationRecord(kind, image)
		if err != nil {
			return platform.AssetInstallation{}, err
		}
		name, version, raw, _ := integrationMetadata(record)
		var asset platform.ReleaseAsset
		if integrationRecord(record).ID == "" || version < 1 || construction.ValueDecodeIntegration([]byte(raw), &asset) != nil || asset.Ref != (platform.AssetRef{App: construction.ID, Kind: kind, Name: name}) {
			return platform.AssetInstallation{}, fmt.Errorf("invalid integration installation")
		}
		if err := validateIntegrationFamily(record, asset); err != nil {
			return platform.AssetInstallation{}, err
		}
		if err := construction.ValueValidateIntegrationDescriptor(asset); err != nil {
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
	if !construction.ValueIntegrationKind(plan.Ref.Kind) {
		return b.ValidateAcceptedPublication(plan.Schema, plan.Image)
	}
	record, err := integrationInstallationRecord(plan.Ref.Kind, plan.Image)
	if err != nil {
		return err
	}
	var actor string
	switch r := record.(type) {
	case construction.Source:
		actor = r.Puller
	case construction.Pipeline:
		actor = r.Runner
	case construction.Writeback:
		actor = r.Publisher
	case construction.Observation:
		actor = r.Publisher
	case construction.Match:
		actor = r.Publisher
	}
	var c platform.Caller
	if actor != "" {
		member, ok := b.host.Member(actor)
		if !ok || !member.Active() || !member.InScope(plan.Schema) || !member.May(construction.ID, plan.Schema, []string{construction.Builder, construction.Integrator}).Allow {
			return fmt.Errorf("integration execution member lacks publication permission")
		}
		c = b.host.Caller(platform.Caller{}, member, construction.ID)
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
	case construction.Connection:
		_, err = b.ExecutionConnection(r)
	case construction.Dataset:
		_, err = b.ExecutionDataset(r)
	case construction.Source:
		fixed, e := b.ExecutionSource(r)
		if e != nil {
			return e
		}
		if e := b.checkSource(c, fixed); e != nil {
			return e
		}
		err = writes(fixed.Object)
	case construction.Pipeline:
		fixed, e := b.ExecutionPipeline(r)
		if e != nil {
			return e
		}
		if e := b.checkPipeline(c, fixed); e != nil {
			return e
		}
		err = writes(fixed.OutputObject)
	case construction.Writeback:
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
	case construction.Observation:
		if _, e := b.ExecutionObservation(r); e != nil {
			return e
		}
		configuration, e := b.acceptedIntegration(r, platform.AssetObservation)
		if e != nil {
			return e
		}
		fixed := configuration.(construction.Observation)
		fixed.Record, fixed.State, fixed.Publisher = r.Record, r.State, r.Publisher
		fixed.PublisherScopes = r.PublisherScopes
		fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
		err = b.checkObservation(c, fixed)
	case construction.Match:
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
	if err := construction.ValueDecodeIntegration(raw, ptr.Interface()); err != nil {
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
	if construction.ValueIntegrationKind(plan.Ref.Kind) || (plan.Ref.Kind == platform.AssetMigration || plan.Ref.Kind == platform.AssetSampleSet) {
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
func (b *Build) validateIntegrationUpgrade(plan platform.AssetInstallation) error {
	if !construction.ValueIntegrationKind(plan.Ref.Kind) {
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
	case construction.Source:
		next, e := b.ExecutionSource(proposed.(construction.Source))
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
	case construction.Pipeline:
		next, e := b.ExecutionPipeline(proposed.(construction.Pipeline))
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
		if asset.Ref.App != construction.ID || !construction.ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetSource:
			var d construction.SourceDefinition
			if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(construction.SourceType + ".publish"); err != nil {
				return err
			}
			if d.Object != "" {
				if err := writes(d.Object, "create", "edit"); err != nil {
					return err
				}
			}
			if d.Dataset != "" {
				if err := permission(construction.SchemaDatasetLoad); err != nil {
					return err
				}
			}
		case platform.AssetPipeline:
			var d construction.PipelineDefinition
			if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(construction.PipelineType + ".publish"); err != nil {
				return err
			}
			if d.OutputObject != "" {
				if err := writes(d.OutputObject, "create", "edit"); err != nil {
					return err
				}
			}
			if d.OutputDataset != "" {
				if err := permission(construction.SchemaDatasetLoad); err != nil {
					return err
				}
			}
		case platform.AssetWriteback:
			var d construction.WritebackDefinition
			if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(construction.WritebackType + ".publish"); err != nil {
				return err
			}
			if len(d.Result) > 0 {
				if err := writes(d.Object, "edit"); err != nil {
					return err
				}
			}
		case platform.AssetObservation:
			var d platform.ObservationDefinition
			if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
				return err
			}
			if err := permission(construction.SchemaObservation); err != nil {
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
	kinds := map[string]platform.AssetKind{construction.ObjectType: platform.AssetObject, construction.PageType: platform.AssetPage, construction.AppType: platform.AssetApp, construction.ProcessType: platform.AssetFlow, MigrationPlanType: platform.AssetMigration, SampleSetType: platform.AssetSampleSet,
		construction.PropertyTypeType: platform.AssetPropertyType, construction.LinkTypeType: platform.AssetLinkType, construction.QueryType: platform.AssetQuery, construction.FunctionType: platform.AssetFunction, AgentType: platform.AssetAgent,
		construction.CodeType: platform.AssetCompute, construction.TableType: platform.AssetCompute, construction.AlertType: platform.AssetAlert, construction.TestPlanType: platform.AssetTestPlan}
	kind := kinds[typ]
	if kind == "" {
		kind = integrationKindOf(typ)
	}
	if kind == "" {
		return platform.AssetRef{}, fmt.Errorf("unknown native release draft %s", typ)
	}
	if kind == platform.AssetObject || kind == platform.AssetFlow {
		name = construction.TypeOf(name)
	}
	return platform.AssetRef{App: construction.ID, Kind: kind, Name: name}, nil
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
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.PropertyTypeType, propertyTypeFromDeclaration(d), nil
	case platform.AssetLinkType:
		var d platform.LinkType
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.LinkTypeType, linkTypeFromDeclaration(d), nil
	case platform.AssetQuery:
		var d Query
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.QueryType, d, nil
	case platform.AssetFunction:
		var d construction.Function
		if err := construction.ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return construction.FunctionType, d, nil
	case platform.AssetEvaluation, platform.AssetTestPlan:
		d, err := b.testPlanDraft(asset)
		return construction.TestPlanType, d, err
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
	b.draftIntegrationTargets(assets)
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
	case platform.AssetSampleSet:
		var samples platform.SampleSet
		if err = json.Unmarshal(asset.Body, &samples); err == nil {
			err = samples.Check()
		}
		definition.SampleSet = &samples
	case platform.AssetEvaluation:
		var descriptor platform.EvaluationSetDescriptor
		descriptor, err = platform.ReadEvaluationSet(asset.Body)
		definition.Evaluation = &descriptor
	case platform.AssetTestPlan:
		var descriptor platform.TestPlanDescriptor
		descriptor, err = platform.ReadTestPlan(asset.Body)
		definition.TestPlan = &descriptor
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
