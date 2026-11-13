package host

import (
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (t *Tenant) decisionDefinition(app string, request *pb.Submission, release string, retained ...[]platform.ReleaseAsset) (*platform.DecisionDefinition, error) {
	if len(retained) > 0 || t.readScope == nil || t.readScope.definitions == nil {
		return t.buildDecisionDefinition(app, request, release, retained...)
	}
	key := releaseowner.DefinitionPlanKey{App: app, Action: request.GetSchema().GetName(), Family: request.GetSchema().GetVersion(), Release: release}
	return t.readScope.definitions.Lookup(t.definitionsFingerprint(), t.Projection.LiveVersion(build.ResourceType), key, func() (*platform.DecisionDefinition, error) {
		return t.buildDecisionDefinition(app, request, release)
	})
}

func (t *Tenant) buildDecisionDefinition(app string, request *pb.Submission, release string, retained ...[]platform.ReleaseAsset) (*platform.DecisionDefinition, error) {
	owner := t.App(app)
	if owner == nil {
		return nil, nil
	}
	manifest := owner.Manifest()
	action, exists := manifest.Actions.Action(request.GetSchema().GetName())
	if !exists {
		return nil, nil
	}
	contract, err := t.actionSchemaVersion(owner, action)
	if err != nil {
		return nil, err
	}
	if action.SchemaVersion == nil {
		contract.Family = request.GetSchema().GetVersion()
	}
	registry, _ := t.assetRegistryLocked()
	return releaseowner.DecisionDefinition(manifest.ID, manifest.Version, action, release, contract, registry, t.releases, retained...)
}
