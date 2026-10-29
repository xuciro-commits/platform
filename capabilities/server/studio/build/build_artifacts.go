package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// prepareReleasePublications compiles frozen owner descriptors. It neither
// installs definitions nor edits drafts. Renaming an owning draft is not yet
// supported: the candidate has logical identities rather than editor row IDs.
func (b *Build) prepareReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	var publications []platform.ReleasePublication
	add := func(schema string, record any) error {
		raw, err := json.Marshal(record)
		if err == nil {
			publications = append(publications, platform.ReleasePublication{Schema: schema, Image: raw})
		}
		return err
	}
	for _, asset := range assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			code, err := b.retainedCodeObject(asset)
			if err != nil {
				return nil, err
			}
			if code {
				continue
			}
			var frozen Object
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || TypeOf(frozen.Name) != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved object %s", asset.Ref)
			}
			if err := mapDispatchIntegrations(&frozen, b.integrationID); err != nil {
				return nil, err
			}
			i := slices.IndexFunc(objects, func(o Object) bool { return !o.Archived && o.Name == frozen.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved object %s has no matching draft; renamed or archived definitions need an upgrade plan", asset.Ref)
			}
			frozen.Record = objects[i].Record
			objects[i].State, objects[i].Installed = "published", TypeOf(frozen.Name)
			objects[i].Published = published(frozen)
			objects[i].ExtensionSource = frozen.ExtensionSource
			objects[i].SchemaVersions = frozen.SchemaVersions
			if err := add(SchemaPublish, objects[i]); err != nil {
				return nil, err
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p Page) bool { return !p.Archived && p.Name == asset.Ref.Name })
			if i < 0 { // the object's generated page has no second owner record
				continue
			}
			if pages[i].Published == "" {
				generated, err := generatedReleasePage(asset, assets, b.Manifest().Version)
				if err != nil {
					return nil, err
				}
				if generated {
					continue // An object's default page does not publish a separate saved draft.
				}
			}
			var pageDescriptor platform.Page
			if err := json.Unmarshal(asset.Body, &pageDescriptor); err != nil || pageDescriptor.Name != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			if previous, ok := ReadPublication[Page](pages[i].Published); ok && !maps.Equal(pageFunctionBindings(pageDescriptor), pageFunctionBindings(descriptor(previous))) {
				return nil, fmt.Errorf("page %s changes AI function bindings; publish the binding through its owner before release activation", asset.Ref)
			}
			frozen, err := pageDraft(pageDescriptor)
			if err != nil {
				return nil, fmt.Errorf("%w in page %s", err, asset.Ref)
			}
			frozen.Record = pages[i].Record
			frozen.Versions = slices.Clone(pages[i].Versions)
			frozen.Published = pages[i].Published
			if err := retainPagePublication(&frozen); err != nil {
				return nil, err
			}
			pages[i].State, pages[i].Published, pages[i].Versions = "published", frozen.Published, frozen.Versions
			if err := add(SchemaRelease, pages[i]); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return !a.Archived && a.Name == asset.Ref.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved application %s has no matching draft", asset.Ref)
			}
			var frozen Application
			if err := json.Unmarshal(asset.Body, &frozen); err != nil || frozen.Name != asset.Ref.Name {
				return nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			frozen.Record = apps[i].Record
			apps[i].State, apps[i].Published = "published", published(frozen)
			if err := add(SchemaHandOver, apps[i]); err != nil {
				return nil, err
			}
		case platform.AssetFlow:
			i := slices.IndexFunc(processes, func(p Process) bool { return !p.Archived && TypeOf(p.Name) == asset.Ref.Name })
			if i < 0 {
				return nil, fmt.Errorf("saved flow %s has no matching owner", asset.Ref)
			}
			frozen, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if prior, ok := ReadPublication[Process](processes[i].Published); ok {
				priorAsset, err := processReleaseAsset(prior, asset.SourceVersion)
				if err == nil {
					var a, z any
					_ = json.Unmarshal(priorAsset.Body, &a)
					_ = json.Unmarshal(asset.Body, &z)
					if reflect.DeepEqual(a, z) {
						continue
					}
				}
			}
			if processes[i].Version >= 64 {
				return nil, fmt.Errorf("flow version family is full")
			}
			frozen.Record, frozen.Version, frozen.State = processes[i].Record, processes[i].Version+1, "published"
			frozen.Layout = processes[i].Layout
			processes[i].Version, processes[i].State = frozen.Version, "published"
			processes[i].Published = published(frozen)
			processes[i].Versions = append(slices.Clone(processes[i].Versions), processes[i].Published)
			if err := add(SchemaProcess, processes[i]); err != nil {
				return nil, err
			}
		}
	}

	for _, prepare := range []func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error){b.prepareQueryReleasePublications, b.preparePropertyTypeReleasePublications, b.prepareLinkTypeReleasePublications, b.prepareFunctionReleasePublications, b.prepareAgentReleasePublications, b.prepareTestPlanPublications, b.PrepareCodeReleasePublications, b.prepareTablePublications, b.prepareAlertPublications} {
		images, err := prepare(assets)
		if err != nil {
			return nil, err
		}
		publications = append(publications, images...)
	}
	return publications, nil
}

func generatedReleasePage(page platform.ReleaseAsset, assets []platform.ReleaseAsset, version string) (bool, error) {
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return false, err
		}
		if object.Name != page.Ref.Name {
			continue
		}
		generated, err := platform.PageReleaseAsset(ID, version, CompileObjectPage(object))
		if err != nil {
			return false, err
		}
		var expected, actual any
		if err := json.Unmarshal(generated.Body, &expected); err != nil {
			return false, err
		}
		if err := json.Unmarshal(page.Body, &actual); err != nil {
			return false, err
		}
		return reflect.DeepEqual(expected, actual), nil
	}
	return false, nil
}

func pageFunctionBindings(p platform.Page) map[string]string {
	bindings := map[string]string{}
	safe, err := p.Safety()
	if err != nil {
		return bindings
	}
	for _, trigger := range safe.ActionTriggers {
		if trigger.Function != nil {
			bindings[trigger.Function.Ref.String()] = trigger.Function.SourceVersion
		}
		if trigger.Operation != nil {
			bindings[trigger.Operation.Ref.String()] = trigger.Operation.SourceVersion
		}
	}
	return bindings
}

// pageDraft is the builder's page record a saved page descriptor denotes: what
// activation writes onto the owning draft, and what an environment that never
// authored the page creates before it can activate it.
func pageDraft(pageDescriptor platform.Page) (Page, error) {
	raw, err := json.Marshal(pageDescriptor)
	if err != nil {
		return Page{}, err
	}
	var original map[string]json.RawMessage
	if err = json.Unmarshal(raw, &original); err != nil {
		return Page{}, err
	}
	original["object"] = platform.Raw(pageDescriptor.Object.Name)
	if pageDescriptor.Format != 3 {
		original["list"], original["detail"] = original["listFields"], original["detailFields"]
		delete(original, "listFields")
		delete(original, "detailFields")
		if pageDescriptor.Actions != nil {
			actions := make([]string, 0, len(pageDescriptor.Actions))
			for _, action := range pageDescriptor.Actions {
				actions = append(actions, action.Name)
			}
			original["actions"] = platform.Raw(actions)
		}
		sections := make([]map[string]json.RawMessage, 0, len(pageDescriptor.Sections))
		for _, section := range pageDescriptor.Sections {
			var value map[string]json.RawMessage
			if err = json.Unmarshal(platform.Raw(section), &value); err != nil {
				return Page{}, err
			}
			if section.Object.Name != "" {
				value["object"] = platform.Raw(section.Object.Name)
			} else {
				delete(value, "object")
			}
			if section.Query.Name != "" {
				value["query"] = platform.Raw(section.Query.App + "." + section.Query.Name)
			} else {
				delete(value, "query")
			}
			if section.Actions != nil {
				actions := make([]string, 0, len(section.Actions))
				for _, action := range section.Actions {
					actions = append(actions, action.Name)
				}
				value["actions"] = platform.Raw(actions)
			}
			if section.InlineEdit != nil {
				value["inlineEdit"] = platform.Raw(InlineEdit{Action: section.InlineEdit.Action.Name, Fields: section.InlineEdit.Fields})
			}
			if section.Function != nil {
				var version int
				if _, err = fmt.Sscanf(section.Function.SourceVersion, ValueDefinitionVersion+".function-%d", &version); err != nil || version < 1 {
					return Page{}, fmt.Errorf("invalid saved function binding")
				}
				value["function"] = platform.Raw(platform.FunctionRef{Name: section.Function.Ref.Name, Version: version})
			}
			sections = append(sections, value)
		}
		if pageDescriptor.Sections != nil {
			original["sections"] = platform.Raw(sections)
		}
	}
	var frozen Page
	if err = json.Unmarshal(platform.Raw(original), &frozen); err != nil {
		return Page{}, err
	}
	return frozen, nil
}

// Draft is one builder draft a candidate's assets rest on: the record type,
// the asset name and the draft image.
type Draft struct {
	Type  string
	Name  string
	Image json.RawMessage
}

// releaseDraftsOf lists the builder drafts a candidate's assets are published onto:
// objects, authored pages (an object's generated page has no draft), applications
// and flows. Promotion into an environment that never authored them creates each
// missing one through the builder's own create action.
func (b *Build) releaseDraftsOf(candidate platform.ReleaseCandidate) ([]Draft, error) {
	authored := map[string]bool{}
	if b.host != nil {
		pages, err := readDefinitionInventory[Page](b.host.Automation(platform.Caller{}, ID))
		if err != nil {
			return nil, err
		}
		for _, page := range pages {
			if !page.Archived {
				authored[page.Name] = true
			}
		}
	}
	var out []Draft
	add := func(typ, name string, record any) error {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		out = append(out, Draft{Type: typ, Name: name, Image: raw})
		return nil
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetObject:
			var o Object
			if err := json.Unmarshal(asset.Body, &o); err != nil || o.Name == "" {
				continue // code-owned metadata is not a draft
			}
			if err := mapDispatchIntegrations(&o, b.integrationID); err != nil {
				return nil, err
			}
			if err := add(ObjectType, o.Name, o); err != nil {
				return nil, err
			}
		case platform.AssetPage:
			if !authored[asset.Ref.Name] {
				generated, err := generatedReleasePage(asset, candidate.Assets, b.Manifest().Version)
				if err != nil {
					return nil, err
				}
				if generated {
					continue // Only the identical generated page has no second owner.
				}
			}
			var descriptor platform.Page
			if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
				return nil, fmt.Errorf("invalid saved page %s", asset.Ref)
			}
			p, err := pageDraft(descriptor)
			if err != nil {
				return nil, fmt.Errorf("%w in page %s", err, asset.Ref)
			}
			if err := add(PageType, p.Name, p); err != nil {
				return nil, err
			}
		case platform.AssetApp:
			var a Application
			if err := json.Unmarshal(asset.Body, &a); err != nil {
				return nil, fmt.Errorf("invalid saved application %s", asset.Ref)
			}
			if err := add(AppType, a.Name, a); err != nil {
				return nil, err
			}
		case platform.AssetFlow:
			p, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			if err := add(ProcessType, p.Name, p); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func (*Build) AssetKinds() []platform.AssetKind {
	return []platform.AssetKind{
		platform.AssetObject, platform.AssetAction, platform.AssetPage, platform.AssetApp, platform.AssetFlow, platform.AssetPropertyType, platform.AssetLinkType, platform.AssetQuery, platform.AssetFunction, platform.AssetCompute,
		platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetAgent, platform.AssetEvaluation, platform.AssetTestPlan, platform.AssetSampleSet, platform.AssetObservation, platform.AssetAlert,
	}
}
func (b *Build) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	if ref.App != ID {
		return platform.ReleaseAsset{}, fmt.Errorf("asset has another owner")
	}
	if len(contentVersion) > 1 || len(contentVersion) == 1 && ref.Kind != platform.AssetPage {
		return platform.ReleaseAsset{}, fmt.Errorf("content pins only identify one retained page")
	}
	if ValueIntegrationKind(ref.Kind) {
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
				if err := ValueDecodeIntegration([]byte(raw), &a); err != nil {
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
	if ref.Kind == platform.AssetAlert {
		return b.alertPinned(ref, version)
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
	if !ValueIntegrationKind(asset.Ref.Kind) {
		return nil, nil
	}
	var req platform.AssetEnvironmentRequirement
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		var d ConnectionDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || !slices.Contains(ConnectionKinds, d.Kind) {
			return nil, fmt.Errorf("invalid connection binding descriptor")
		}
		req = platform.AssetEnvironmentRequirement{Ref: asset.Ref, Profile: d.Kind, Credential: d.Credential}
	case platform.AssetSource:
		var d SourceDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
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
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Connection.Kind != platform.AssetConnection || d.Connection.Name == "" {
			return nil, fmt.Errorf("observation source has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: d.Connection, Profile: "http"}
	case platform.AssetWriteback:
		var d WritebackDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
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
		if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		rows, err := b.integrationInventory(asset.Ref.Kind)
		if err != nil {
			restore()
			return nil, err
		}
		id, found := asset.Ref.Name, false
		for _, row := range rows {
			name, _, _, _ := integrationMetadata(row)
			if name != asset.Ref.Name {
				continue
			}
			if found {
				restore()
				return nil, fmt.Errorf("integration name %s is ambiguous", name)
			}
			found, id = true, integrationRecord(row).ID
			if connection, ok := row.(Connection); ok {
				var definition ConnectionDefinition
				if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
					restore()
					return nil, err
				}
				if definition.Kind != connection.Kind {
					restore()
					return nil, fmt.Errorf("connection %s has an incompatible local kind", asset.Ref)
				}
			}
		}
		b.releaseDraftIDs[asset.Ref] = id
	}
	for ref, id := range ctx.DraftIDs {
		b.releaseDraftIDs[ref] = id
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
		if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		if asset.Ref.Kind == platform.AssetConnection {
			connection, err := portableConnectionDraft(asset)
			if err != nil {
				return nil, err
			}
			out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: ConnectionType, Name: connection.Name, Schema: ConnectionType + ".create", Image: platform.Raw(connection)})
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
	for i := range out {
		if id := ctx.DraftIDs[out[i].Ref]; id != "" {
			out[i].Name = id
		}
	}
	return out, nil
}
func (b *Build) PrepareReleaseAssets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetInstallation, error) {
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
	out := []platform.AssetInstallation{}
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
		if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) {
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
			r.State, r.Publisher, r.PublisherScopes = "published", ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			record = r
		case Pipeline:
			r.State = "published"
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
	if !ctx.DraftsOnly {
		for _, plan := range out {
			if slices.Contains([]platform.AssetKind{platform.AssetSource, platform.AssetPipeline, platform.AssetWriteback, platform.AssetObservation, platform.AssetMatching}, plan.Ref.Kind) && (!ctx.Member.Active() || !ctx.Member.InScope(plan.Schema)) {
				return nil, fmt.Errorf("release publication token does not permit %s", plan.Schema)
			}
		}
	}
	return orderOwnerInstallations(assets, out)
}
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
		// Keep the private catalog's earlier object declarations visible to
		// later pages/functions/flows in the same installation. The live ledger
		// remains untouched; this owner belongs to the installation view.
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

func draftAssetRef(typ, name string) (platform.AssetRef, error) {
	kinds := map[string]platform.AssetKind{ObjectType: platform.AssetObject, PageType: platform.AssetPage, AppType: platform.AssetApp, ProcessType: platform.AssetFlow, SampleSetType: platform.AssetSampleSet,
		PropertyTypeType: platform.AssetPropertyType, LinkTypeType: platform.AssetLinkType, QueryType: platform.AssetQuery, FunctionType: platform.AssetFunction, AgentType: platform.AssetAgent,
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
	switch asset.Ref.Kind {
	case platform.AssetSampleSet:
		d, _, err := decodeSampleAsset(asset)
		return SampleSetType, SampleSetRecord{Name: d.Name, Title: d.Title, Samples: d.Samples}, err
	case platform.AssetPropertyType:
		var d platform.PropertyType
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return PropertyTypeType, propertyTypeFromDeclaration(d), nil
	case platform.AssetLinkType:
		var d platform.LinkType
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return LinkTypeType, linkTypeFromDeclaration(d), nil
	case platform.AssetQuery:
		var d Query
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return QueryType, d, nil
	case platform.AssetFunction:
		var d Function
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return "", nil, err
		}
		return FunctionType, d, nil
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

// Bootstrap shapes are scoped to a private owner. They make the existing
// authoring checks recognize self/forward object references without installing
// a registry, extending the live ledger or validating an unfinished closure.
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

// DefinitionMetadataKind identifies ordinary accepted metadata whose discovery
// is governed by its original metadata record. Executable Build definitions
// keep their installed descriptors; agent and observation projection remains
// narrower than this ordinary record visibility check.
func (*Build) DefinitionMetadataKind(kind platform.AssetKind) bool {
	return slices.Contains([]platform.AssetKind{platform.AssetConnection, platform.AssetSource, platform.AssetDataset, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetEvaluation, platform.AssetTestPlan, platform.AssetSampleSet, platform.AssetObservation, platform.AssetAlert}, kind)
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

// definitionFamily contains only the owner metadata shared by these
// retained declaration families. Their declaration checks remain type specific.
type definitionFamily struct {
	platform.Record
	Name, State string
	Version     int
	Published   string
	Versions    []string
}

func definitionFamilyOf(record any) definitionFamily {
	switch r := record.(type) {
	case Query:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case Function:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case PropertyType:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case LinkType:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case SampleSetRecord:
		return definitionFamily{r.Record, r.Name, r.State, r.DefinitionVersion, r.Published, r.Versions}
	}
	panic("unsupported retained definition")
}

func setDefinitionFamily(record any, family definitionFamily) {
	switch r := record.(type) {
	case *Query:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	case *Function:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	case *PropertyType:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	case *LinkType:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	default:
		panic("unsupported retained definition")
	}
}

func portableDefinitionAssets[T, D any](inventory func() ([]T, error), decode func(platform.ReleaseAsset) (D, int, error), label string) ([]platform.ReleaseAsset, error) {
	rows, err := inventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleaseAsset{}
	for _, record := range rows {
		family := definitionFamilyOf(record)
		if family.Archived || family.Published == "" {
			continue
		}
		var asset platform.ReleaseAsset
		if json.Unmarshal([]byte(family.Published), &asset) != nil {
			return nil, fmt.Errorf("Invalid retained %s", label)
		}
		if _, _, err := decode(asset); err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func pinnedDefinitionAsset[T, D any](inventory func() ([]T, error), decode func(platform.ReleaseAsset) (D, int, error), ref platform.AssetRef, version, label string) (platform.ReleaseAsset, error) {
	rows, err := inventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		for _, raw := range definitionFamilyOf(record).Versions {
			var asset platform.ReleaseAsset
			if json.Unmarshal([]byte(raw), &asset) == nil && asset.Ref == ref && asset.SourceVersion == version {
				_, _, err := decode(asset)
				return asset, err
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("The %s version is not retained", label)
}

func portableDefinitionInstallation[T, D any](image json.RawMessage, typ, schema, label string, decode func(platform.ReleaseAsset) (D, int, error)) (platform.AssetInstallation, error) {
	var record T
	var asset platform.ReleaseAsset
	if json.Unmarshal(image, &record) != nil {
		return platform.AssetInstallation{}, fmt.Errorf("Invalid %s installation", label)
	}
	family := definitionFamilyOf(record)
	if family.ID == "" || family.State != "published" || json.Unmarshal([]byte(family.Published), &asset) != nil {
		return platform.AssetInstallation{}, fmt.Errorf("Invalid %s installation", label)
	}
	_, version, err := decode(asset)
	if err != nil || version != family.Version || asset.Ref.Name != family.Name || len(family.Versions) != version || family.Versions[len(family.Versions)-1] != family.Published {
		return platform.AssetInstallation{}, fmt.Errorf("Invalid %s version history", label)
	}
	for i, raw := range family.Versions {
		var prior platform.ReleaseAsset
		if json.Unmarshal([]byte(raw), &prior) != nil {
			return platform.AssetInstallation{}, fmt.Errorf("Invalid %s version history", label)
		}
		if _, ordinal, err := decode(prior); err != nil || ordinal != i+1 || prior.Ref.Name != family.Name {
			return platform.AssetInstallation{}, fmt.Errorf("Invalid %s version history", label)
		}
	}
	return platform.AssetInstallation{Ref: asset.Ref, Schema: schema, Record: typ + "/" + family.ID, Image: slices.Clone(image)}, nil
}

// Type-specific checks have already prepared the declaration. Its original
// installer still owns runtime changes; staged decisions retain only metadata.
func publishRetainedDefinition[T any](c platform.Caller, record *T, limitMessage string, install func(platform.Caller, T) error) *kernel.Error {
	family := definitionFamilyOf(*record)
	if family.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, limitMessage)
	}
	family.Version++
	family.State = "published"
	setDefinitionFamily(record, family)
	family.Published = published(*record)
	family.Versions = append(family.Versions, family.Published)
	setDefinitionFamily(record, family)
	if !c.Staging() {
		if err := install(c, *record); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	return nil
}

func definitionImage[T interface{ Definition() D }, D interface{ Check() error }](image []byte, label string, keeps func(T, T) bool) (selected T, err error) {
	var record T
	if err := json.Unmarshal(image, &record); err != nil {
		return selected, err
	}
	family := definitionFamilyOf(record)
	if !slices.Contains([]string{"draft", "published"}, family.State) || family.Version < 1 || family.Version > 64 || len(family.Versions) != family.Version || !slices.Contains(family.Versions, family.Published) {
		return selected, fmt.Errorf("accepted %s has an invalid version family", label)
	}
	var latest T
	for i, raw := range family.Versions {
		f, ok := ReadPublication[T](raw)
		entry := definitionFamilyOf(f)
		if !ok || entry.State != "published" || entry.ID != family.ID || entry.Version != i+1 || entry.Published != "" || len(entry.Versions) != 0 || f.Definition().Check() != nil || i > 0 && !keeps(latest, f) {
			var zero T
			return zero, fmt.Errorf("accepted %s has a malformed version %d", label, i+1)
		}
		if raw == family.Published {
			selected = f
		}
		latest = f
	}
	return selected, nil
}

func retainedDefinition[T interface{ Definition() D }, D any](installed map[string]T, inventory func() ([]T, error), decode func([]byte) (T, error), name string, version int) (out D, ordinal int, ok bool) {
	if record, found := installed[name]; found {
		family := definitionFamilyOf(record)
		if version == 0 || version == family.Version {
			return record.Definition(), family.Version, true
		}
	}
	if version == 0 {
		return out, 0, false
	}
	records, err := inventory()
	if err != nil {
		return out, 0, false
	}
	for _, record := range records {
		family := definitionFamilyOf(record)
		if version < 1 || version > len(family.Versions) {
			continue
		}
		raw, _ := json.Marshal(record)
		latest, err := decode(raw)
		if err != nil || definitionFamilyOf(latest).Name != name {
			continue
		}
		f, found := ReadPublication[T](family.Versions[version-1])
		if found && definitionFamilyOf(f).Version == version {
			return f.Definition(), version, true
		}
	}
	return out, 0, false
}

func definitionDeclarations[T interface{ Definition() D }, D any](installed map[string]T) []D {
	names := slices.Sorted(maps.Keys(installed))
	out := make([]D, 0, len(names))
	for _, name := range names {
		out = append(out, installed[name].Definition())
	}
	return out
}

func retainedDefinitionAsset[D any](source, prefix, label string, kind platform.AssetKind, name, versionText string, lookup func(string, int) (D, int, bool)) (platform.ReleaseAsset, error) {
	ordinalText, found := strings.CutPrefix(versionText, source+"."+prefix+"-")
	version, err := strconv.Atoi(ordinalText)
	if !found || err != nil || strconv.Itoa(version) != ordinalText {
		return platform.ReleaseAsset{}, fmt.Errorf("invalid retained %s version %s", label, versionText)
	}
	definition, ordinal, ok := lookup(name, version)
	if !ok || ordinal != version || version < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("%s %s version %d is not retained", kind, name, version)
	}
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: kind, Name: name}, versionText, definition)
}

func definitionReleaseAsset(ref platform.AssetRef, version string, definition any) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(definition)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	contract := 1
	if link, ok := any(definition).(platform.LinkType); ok {
		contract = link.Contract()
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: contract, SourceVersion: version, Body: body})
}

func (b *Build) definitionAssetVersion(asset platform.ReleaseAsset, kind platform.AssetKind, prefix string) int {
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+"."+prefix+"-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal || asset.Ref.App != ID || asset.Ref.Kind != kind || platform.CheckAssetDependencies(asset) != nil {
		return 0
	}
	return version
}

func prepareDefinitionPublications[T any](b *Build, assets []platform.ReleaseAsset, inventory func() ([]T, error), kind platform.AssetKind, prefix, schema, label string, resolve func(string, string) (platform.ReleaseAsset, error), freeze func(T, platform.ReleaseAsset, int) (T, error)) ([]platform.ReleasePublication, error) {
	records, err := inventory()
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != kind {
			continue
		}
		text, found := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+"."+prefix+"-")
		version, err := strconv.Atoi(text)
		if !found || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != text {
			return nil, fmt.Errorf("invalid %s version", label)
		}
		index := slices.IndexFunc(records, func(record T) bool {
			family := definitionFamilyOf(record)
			return !family.Archived && family.Name == asset.Ref.Name
		})
		if index < 0 {
			return nil, fmt.Errorf("%s has no source owner", label)
		}
		record := records[index]
		family := definitionFamilyOf(record)
		if version <= family.Version {
			prior, err := resolve(asset.Ref.Name, asset.SourceVersion)
			same := err == nil
			if same {
				if kind == platform.AssetFunction {
					same = equalIntegrationAsset(prior, asset)
				} else {
					var retained, candidate any
					same = json.Unmarshal(prior.Body, &retained) == nil && json.Unmarshal(asset.Body, &candidate) == nil && reflect.DeepEqual(retained, candidate)
				}
			}
			if !same {
				return nil, fmt.Errorf("%s retained bytes differ", label)
			}
			if len(family.Versions) != family.Version {
				return nil, fmt.Errorf("retained %s history is unavailable", label)
			}
			selected := family.Versions[version-1]
			if selected == family.Published {
				continue
			}
			family.Published = selected
			if version < family.Version {
				family.State = "draft"
			}
			setDefinitionFamily(&record, family)
		} else {
			record, err = freeze(record, asset, version)
			if err != nil {
				return nil, err
			}
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ReleasePublication{Schema: schema, Image: raw})
	}
	return out, nil
}

func draftDefinitionAssets[T any](b *Build, id string, kind platform.AssetKind, label string, inventory func() ([]T, error), check func(T) (T, error), release func(T) (platform.ReleaseAsset, error)) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	records, problem := inventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(records, func(record T) bool {
		family := definitionFamilyOf(record)
		return family.ID == id && !family.Archived
	})
	if i < 0 {
		err = fmt.Errorf("draft %s %q not found", kind, id)
		return
	}
	record := records[i]
	family := definitionFamilyOf(record)
	if old, ok := ReadPublication[T](family.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: kind, Name: definitionFamilyOf(old).Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: kind, Name: family.Name}
	record, err = check(record)
	if err != nil {
		return
	}
	if family.Version >= 64 {
		err = fmt.Errorf("%s version family is full", label)
		return
	}
	asset, problem := release(record)
	if problem != nil {
		err = problem
		return
	}
	after = slices.Clone(before)
	if hadPrior {
		after = slices.DeleteFunc(after, func(a platform.ReleaseAsset) bool { return a.Ref == prior })
	}
	after = append(after, asset)
	return
}

func applyDefinitionHistory[T any](image []byte, decode func([]byte) (T, error), install func(platform.Caller, T) error) error {
	selected, err := decode(image)
	if err != nil {
		return err
	}
	var record T
	if err := json.Unmarshal(image, &record); err != nil {
		return err
	}
	for _, raw := range definitionFamilyOf(record).Versions {
		version, _ := ReadPublication[T](raw)
		if err := install(platform.Caller{Replaying: true}, version); err != nil {
			return err
		}
	}
	return install(platform.Caller{Replaying: true}, selected)
}

func restoreDefinitionPublications[T any](b *Build, inventory func() ([]T, error), schema string) error {
	records, err := inventory()
	if err != nil {
		return err
	}
	for _, record := range records {
		if definitionFamilyOf(record).Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		if err := b.ApplyAcceptedPublication(schema, raw); err != nil {
			return err
		}
	}
	return nil
}

// Installed snapshots come from the validated owner cache, never editable rows.
func installedDefinitionAssets[T any](installed map[string]T, release func(T) (platform.ReleaseAsset, error)) ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, name := range slices.Sorted(maps.Keys(installed)) {
		asset, err := release(installed[name])
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

// ReadPublication is the definition that was published, or false when none was.
func ReadPublication[T any](raw string) (T, bool) {
	var out T
	if raw == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return out, false
	}
	return out, true
}
