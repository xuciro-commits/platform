package trials

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"slices"
)

type CandidateSandbox struct {
	PutModule          func(digest string, raw []byte) error
	RestoreRecords     func(map[string][]json.RawMessage) (held bool, err error)
	InstallPage        func(platform.Page) error
	InstallApplication func(platform.Application) error
	AddDefinition      func(platform.Definition)
	FinishInstall      func() error
}

func (e Environment) CandidateApps(candidate platform.ReleaseCandidate, member platform.Member, others ...platform.Member) ([]platform.App, build.ConstructionSandbox, error) {
	fresh := func(id string) (platform.App, error) {
		factory := e.Factories[id]
		if factory == nil {
			return nil, fmt.Errorf("simulation owner %s is not available", id)
		}
		app := factory.NewSimulationApp(member.Tenant)
		if app == nil || app.Manifest().ID != id {
			return nil, fmt.Errorf("simulation owner identity differs")
		}
		return app, nil
	}
	builderOwner, err := fresh(build.ID)
	if err != nil {
		return nil, nil, err
	}
	b, ok := builderOwner.(build.ConstructionSandbox)
	if !ok {
		return nil, nil, fmt.Errorf("construction owner has no isolated asset installer")
	}
	member.Roles = maps.Clone(member.Roles)
	if e.Identity == nil {
		return nil, nil, fmt.Errorf("candidate identity factory is unavailable")
	}
	identity := e.Identity.NewSimulationIdentity(member.Tenant, append([]platform.Member{member}, others...))
	if identity == nil {
		return nil, nil, fmt.Errorf("candidate identity factory changed its owner")
	}
	apps := []platform.App{identity}
	if e.Organization != nil {
		if e.OrganizationFactory == nil {
			return nil, nil, fmt.Errorf("the synthetic organization owner is unavailable")
		}
		seed := *e.Organization
		seed.Memberships = slices.Clone(seed.Memberships)
		for i := range seed.Memberships {
			if seed.Memberships[i].Party == "" {
				seed.Memberships[i].Party = "member:" + member.ID
			}
		}
		apps = append(apps, e.OrganizationFactory.NewSimulationOrganization(member.Tenant, seed))
	}
	has := func(kind platform.AssetKind) bool {
		return slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == kind })
	}
	hasProposal := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool {
		if a.Ref.Kind != platform.AssetFunction {
			return false
		}
		var definition platform.AIFunction
		return json.Unmarshal(a.Body, &definition) == nil && definition.Proposal != nil
	})
	hasApproval := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool {
		if a.Ref.Kind != platform.AssetObject || a.Ref.App != build.ID {
			return false
		}
		var object build.Object
		return json.Unmarshal(a.Body, &object) == nil && slices.ContainsFunc(object.Actions, func(action build.Action) bool { return action.Approval != nil })
	})
	var owners []string
	if (hasProposal || hasApproval) && !has(platform.AssetFlow) {
		owners = append(owners, platform.WorkApp)
	}
	if has(platform.AssetFunction) {
		owners = append(owners, platform.AIApp)
	}
	if has(platform.AssetFlow) {
		owners = append(owners, platform.WorkApp, platform.FlowApp)
	}
	if has(platform.AssetAgent) {
		owners = append(owners, platform.AgentApp)
	}
	for _, id := range owners {
		app, err := fresh(id)
		if err != nil {
			return nil, nil, err
		}
		apps = append(apps, app)
	}
	for _, manifest := range e.Manifests {
		if slices.ContainsFunc(apps, func(app platform.App) bool { return app.Manifest().ID == manifest.ID }) {
			continue
		}
		apps = append(apps, NewSampleOwner(member.Tenant, manifest))
	}
	return append(apps, b), b, nil
}

func (e Environment) InstallCandidate(candidate platform.ReleaseCandidate, b build.ConstructionSandbox, sandbox CandidateSandbox) error {
	for digest, raw := range e.Modules {
		if err := sandbox.PutModule(digest, raw); err != nil {
			return err
		}
	}
	install := func(kinds func(platform.AssetKind) bool, apply func(platform.ReleaseAsset) error) error {
		for _, asset := range candidate.Assets {
			if asset.Ref.App == build.ID && kinds(asset.Ref.Kind) {
				if err := apply(asset); err != nil {
					return err
				}
			}
		}
		return nil
	}
	kind := func(names ...platform.AssetKind) func(platform.AssetKind) bool {
		return func(k platform.AssetKind) bool { return slices.Contains(names, k) }
	}
	if err := install(kind(platform.AssetPropertyType, platform.AssetValueType), b.InstallDefinitionAsset); err != nil {
		return err
	}
	definitions := map[string][]json.RawMessage{build.ObjectType: {}}
	processes := map[string][]json.RawMessage{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID {
			continue
		}
		if asset.Ref.Kind == platform.AssetObject && slices.ContainsFunc(b.Manifest().Entities, func(e platform.Entity) bool { return e.Type == asset.Ref.Name }) {
			continue
		}
		if kind(platform.AssetPropertyType, platform.AssetValueType, platform.AssetLinkType, platform.AssetQuery, platform.AssetObjectTypeGroup, platform.AssetFunction, platform.AssetCompute, platform.AssetAgent)(asset.Ref.Kind) || b.DefinitionMetadataKind(asset.Ref.Kind) {
			continue
		}
		if asset.SourceVersion != b.Manifest().Version {
			return fmt.Errorf("unsupported test dependency or compiler version %s", asset.Ref)
		}
		if asset.Ref.Kind == platform.AssetFlow {
			process, err := b.ProcessFromAsset(asset)
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(process)
			processes[build.ProcessType] = append(processes[build.ProcessType], raw)
			continue
		}
		if kind(platform.AssetAction, platform.AssetPage, platform.AssetApp)(asset.Ref.Kind) {
			continue
		}
		if asset.Ref.Kind != platform.AssetObject {
			return fmt.Errorf("unsupported test dependency %s", asset.Ref)
		}
		var object build.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return err
		}
		object.ID, object.State, object.Installed = asset.Ref.Name, "published", asset.Ref.Name
		published, err := json.Marshal(object)
		if err != nil {
			return err
		}
		object.Published = string(published)
		raw, _ := json.Marshal(object)
		definitions[build.ObjectType] = append(definitions[build.ObjectType], raw)
	}
	if _, err := sandbox.RestoreRecords(definitions); err != nil {
		return err
	}
	if err := b.Reinstall(); err != nil {
		return err
	}
	for _, step := range []struct {
		kinds func(platform.AssetKind) bool
		apply func(platform.ReleaseAsset) error
	}{
		{kind(platform.AssetFunction), b.InstallFunctionAsset},
		{kind(platform.AssetCompute), b.InstallOperationAsset},
		{kind(platform.AssetLinkType), b.InstallDefinitionAsset},
		{kind(platform.AssetQuery, platform.AssetObjectTypeGroup), b.InstallDefinitionAsset},
		{kind(platform.AssetAgent), b.InstallAgentAsset},
	} {
		if err := install(step.kinds, step.apply); err != nil {
			return err
		}
	}
	if len(processes) != 0 {
		if _, err := sandbox.RestoreRecords(processes); err != nil {
			return err
		}
		if err := b.Reinstall(); err != nil {
			return err
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetPage {
			var page platform.Page
			if err := json.Unmarshal(asset.Body, &page); err != nil {
				return err
			}
			if err := sandbox.InstallPage(page); err != nil {
				return err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if b.DefinitionMetadataKind(asset.Ref.Kind) {
			definition, err := b.DefinitionDescriptor(asset)
			if err != nil {
				return err
			}
			sandbox.AddDefinition(definition)
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetApp {
			var application platform.Application
			if err := json.Unmarshal(asset.Body, &application); err != nil {
				return err
			}
			if err := sandbox.InstallApplication(application); err != nil {
				return err
			}
		}
	}
	if err := sandbox.FinishInstall(); err != nil {
		return err
	}
	rows := map[string][]json.RawMessage{}
	for _, sample := range e.Samples {
		for _, raw := range sample.Records {
			rows[sample.Type] = append(rows[sample.Type], raw)
		}
	}
	if len(rows) > 0 {
		held, err := sandbox.RestoreRecords(rows)
		if err != nil {
			return err
		}
		if held {
			return fmt.Errorf("sample records have no candidate schema")
		}
	}
	return nil
}
