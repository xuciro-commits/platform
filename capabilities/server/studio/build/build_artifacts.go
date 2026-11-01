package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"platformserver/platform"
)

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
			if i < 0 {
				continue
			}
			if pages[i].Published == "" {
				generated, err := generatedReleasePage(asset, assets, b.Manifest().Version)
				if err != nil {
					return nil, err
				}
				if generated {
					continue
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
					a, _ := upgradedProcessBody(priorAsset.Body)
					z, _ := upgradedProcessBody(asset.Body)
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

	for _, k := range definitionKinds {
		images, err := k.prepare(b, assets)
		if err != nil {
			return nil, err
		}
		publications = append(publications, images...)
	}
	for _, prepare := range []func([]platform.ReleaseAsset) ([]platform.ReleasePublication, error){b.prepareAgentReleasePublications, b.prepareTestPlanPublications, b.PrepareCodeReleasePublications, b.prepareTablePublications, b.prepareAlertPublications} {
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

type Draft struct {
	Type  string
	Name  string
	Image json.RawMessage
}

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
				continue
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
					continue
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
		platform.AssetObject, platform.AssetAction, platform.AssetPage, platform.AssetApp, platform.AssetFlow, platform.AssetPropertyType, platform.AssetValueType, platform.AssetLinkType, platform.AssetQuery, platform.AssetFunction, platform.AssetCompute,
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
	if k := definitionKindOf(ref.Kind); k != nil {
		return k.resolve(b, ref.Name, version)
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
