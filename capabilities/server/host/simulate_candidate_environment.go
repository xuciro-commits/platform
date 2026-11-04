package host

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	records "platformserver/kernel/records"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"slices"
)

func candidateTestTenantWithEnvironment(candidate platform.ReleaseCandidate, member platform.Member, environment simulationEnvironment, others ...platform.Member) (*Tenant, error) {
	fresh := func(id string) (platform.App, error) {
		factory := environment.Factories[id]
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
		return nil, err
	}
	b, ok := builderOwner.(build.ConstructionSandbox)
	if !ok {
		return nil, fmt.Errorf("construction owner has no isolated asset installer")
	}
	member.Roles = maps.Clone(member.Roles)
	if environment.Identity == nil {
		return nil, fmt.Errorf("candidate identity factory is unavailable")
	}
	principals := append([]platform.Member{member}, others...)
	identity := environment.Identity.NewSimulationIdentity(member.Tenant, principals)
	if identity == nil || identity.Manifest().ID != controlowner.PlatformApp {
		return nil, fmt.Errorf("candidate identity factory changed its owner")
	}
	apps := []platform.App{identity}
	if environment.Organization != nil {
		if environment.OrganizationFactory == nil {
			return nil, fmt.Errorf("the synthetic organization owner is unavailable")
		}
		seed := *environment.Organization
		seed.Memberships = slices.Clone(seed.Memberships)
		for i := range seed.Memberships {
			if seed.Memberships[i].Party == "" {
				seed.Memberships[i].Party = "member:" + member.ID
			}
		}
		apps = append(apps, environment.OrganizationFactory.NewSimulationOrganization(member.Tenant, seed))
	}
	hasFunctions := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFunction })
	hasFlow := slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFlow })
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
	if (hasProposal || hasApproval) && !hasFlow {
		app, err := fresh(platform.WorkApp)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	if hasFunctions {
		app, err := fresh(platform.AIApp)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetFlow }) {
		for _, id := range []string{platform.WorkApp, platform.FlowApp} {
			app, err := fresh(id)
			if err != nil {
				return nil, err
			}
			apps = append(apps, app)
		}
	}
	if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref.Kind == platform.AssetAgent }) {
		app, err := fresh(platform.AgentApp)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	for _, manifest := range environment.Manifests {
		if slices.ContainsFunc(apps, func(app platform.App) bool { return app.Manifest().ID == manifest.ID }) {
			continue
		}
		apps = append(apps, trialowner.NewSampleOwner(member.Tenant, manifest))
	}
	apps = append(apps, b)
	sandbox, err := NewTenantWithCapabilities(member.Tenant, environment.Services, apps...)
	if err != nil {
		return nil, err
	}
	sandbox.simulation = true
	endBootstrap, err := b.BeginAssetBootstrap(candidate.Assets)
	if err != nil {
		return nil, err
	}
	defer endBootstrap()
	sandbox.installingSet = true
	if err := sandbox.releases.Put(candidate.ID, candidate.Bytes); err != nil {
		return nil, err
	}
	sandbox.releases.SelectPreview(candidate.ID)
	sandbox.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	for digest, raw := range environment.Modules {
		if err := sandbox.files().Put(context.Background(), artifactKey(sandbox.ID, digest), raw, "application/wasm"); err != nil {
			return nil, err
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID && (asset.Ref.Kind == platform.AssetPropertyType || asset.Ref.Kind == platform.AssetValueType) {
			if err := b.InstallDefinitionAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	definitions := map[string][]records.StoredState{build.ObjectType: {}}
	processes := map[string][]records.StoredState{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID {
			continue
		}
		if asset.Ref.Kind == platform.AssetObject && slices.ContainsFunc(b.Manifest().Entities, func(e platform.Entity) bool { return e.Type == asset.Ref.Name }) {
			continue
		}
		if asset.Ref.Kind == platform.AssetAlert {
			alert, _, err := b.AcceptedAlert(asset)
			if err != nil {
				return nil, err
			}
			definitions[build.AlertType] = append(definitions[build.AlertType], records.StoredState{Value: platform.Raw(alert)})
			continue
		}
		if asset.Ref.Kind == platform.AssetPropertyType || asset.Ref.Kind == platform.AssetValueType || asset.Ref.Kind == platform.AssetLinkType || asset.Ref.Kind == platform.AssetQuery || asset.Ref.Kind == platform.AssetObjectTypeGroup || asset.Ref.Kind == platform.AssetFunction || asset.Ref.Kind == platform.AssetCompute || asset.Ref.Kind == platform.AssetAgent || b.DefinitionMetadataKind(asset.Ref.Kind) {
			continue
		}
		if asset.Ref.App != build.ID || asset.SourceVersion != b.Manifest().Version {
			return nil, fmt.Errorf("unsupported test dependency or compiler version %s", asset.Ref)
		}
		if asset.Ref.Kind == platform.AssetFlow {
			process, err := b.ProcessFromAsset(asset)
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(process)
			processes[build.ProcessType] = append(processes[build.ProcessType], records.StoredState{Value: raw})
			continue
		}
		if asset.Ref.Kind == platform.AssetAction || asset.Ref.Kind == platform.AssetPage || asset.Ref.Kind == platform.AssetApp {
			continue
		}
		if asset.Ref.Kind != platform.AssetObject {
			return nil, fmt.Errorf("unsupported test dependency %s", asset.Ref)
		}
		var object build.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return nil, err
		}
		object.ID, object.State, object.Installed = asset.Ref.Name, "published", asset.Ref.Name
		published, err := json.Marshal(object)
		if err != nil {
			return nil, err
		}
		object.Published = string(published)
		raw, _ := json.Marshal(object)
		definitions[build.ObjectType] = append(definitions[build.ObjectType], records.StoredState{Value: raw})
	}
	if _, err := sandbox.restoreRecords(definitions); err != nil {
		return nil, err
	}
	if err := b.Reinstall(); err != nil {
		return nil, err
	}
	if hasFunctions {
		for _, asset := range candidate.Assets {
			if asset.Ref.Kind == platform.AssetFunction && asset.Ref.App == build.ID {
				if err := b.InstallFunctionAsset(asset); err != nil {
					return nil, err
				}
			}
		}
		sandbox.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetCompute && asset.Ref.App == build.ID {
			if err := b.InstallOperationAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetLinkType {
			if err := b.InstallDefinitionAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID && (asset.Ref.Kind == platform.AssetQuery || asset.Ref.Kind == platform.AssetObjectTypeGroup) {
			if err := b.InstallDefinitionAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetAgent && asset.Ref.App == build.ID {
			if err := b.InstallAgentAsset(asset); err != nil {
				return nil, err
			}
		}
	}
	if len(processes) != 0 {
		if _, err := sandbox.restoreRecords(processes); err != nil {
			return nil, err
		}
		if err := b.Reinstall(); err != nil {
			return nil, err
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetPage {
			var page platform.Page
			if err := json.Unmarshal(asset.Body, &page); err != nil {
				return nil, err
			}
			if err := sandbox.InstallPage(b, page); err != nil {
				return nil, err
			}
		}
	}
	for _, asset := range candidate.Assets {
		if b.DefinitionMetadataKind(asset.Ref.Kind) {
			definition, err := b.DefinitionDescriptor(asset)
			if err != nil {
				return nil, err
			}
			sandbox.definitions = append(sandbox.definitions, definition)
		}
	}

	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetApp {
			var application platform.Application
			if err := json.Unmarshal(asset.Body, &application); err != nil {
				return nil, err
			}
			if err := sandbox.InstallApplication(b, application); err != nil {
				return nil, err
			}
		}
	}
	sandbox.installingSet = false
	if err := sandbox.validateAssetSet(sandbox.services.Rules); err != nil {
		return nil, err
	}
	rows := map[string][]records.StoredState{}
	for _, sample := range environment.Samples {
		for _, raw := range sample.Records {
			rows[sample.Type] = append(rows[sample.Type], records.StoredState{Value: raw})
		}
	}
	if len(rows) > 0 {
		held, err := sandbox.restoreRecords(rows)
		if err != nil {
			return nil, err
		}
		if len(held) > 0 {
			return nil, fmt.Errorf("sample records have no candidate schema")
		}
	}
	return sandbox, nil
}
