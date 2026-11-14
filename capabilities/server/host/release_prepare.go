package host

import releaseowner "platformserver/services/devops/releases"

import (
	"encoding/json"

	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/services/resources/authoring"

	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (t *Tenant) bootstrapOwnerDraftsLockedWithArchived(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly bool, request *releaseowner.OwnerDraftRequest, includeArchived bool, existing ...*stagedDecision) (*stagedDecision, json.RawMessage, error) {
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil, nil, err
	}
	drafts, err := releaseowner.BootstrapDrafts(registry, candidate, context, request)
	if err != nil {
		return nil, nil, err
	}
	decision := t.newStagedDecision()
	if len(existing) > 0 {
		decision = existing[0]
	}
	view, err := t.assetInstallationView(decision.Records, includeArchived)
	if err != nil {
		return nil, nil, err
	}
	view.installingSet = true
	defer func() { view.installingSet = false }()
	if definitionOnly {
		shapes, err := releaseowner.AssetShapes(t.apps, candidate.Assets)
		if err != nil {
			return nil, nil, err
		}
		view.Projection.ResetReshaped(shapes)
	}
	decision.tenant, decision.Records = view, view.Projection
	decision.Records.Current().SelectRelease(candidate.ID)
	apps := slices.Clone(view.apps)
	for i, app := range apps {
		if view.assets[app.Manifest().ID] == nil {
			continue
		}
		apps[i] = decision.stateApp(app)
		(hostView{t: view, app: apps[i]}).bindOwner(apps[i])
	}
	view.ownerSet = view.with(apps)
	registry, err = view.assetRegistryLocked()
	if err != nil {
		return nil, nil, err
	}
	endBootstrap, err := registry.BeginBootstrap(candidate.Assets)
	if err != nil {
		return nil, nil, err
	}
	defer endBootstrap()
	decision.attachPrivateHosts()
	receipt, err := releaseowner.BootstrapStage{
		Tenant: t.ID, Candidate: candidate, Context: context, Request: request, IncludeArchived: includeArchived, Registry: registry, Drafts: drafts,
		Put: func(app string, receipt *pb.ChangeRecord, value any) *kernel.Error {
			return decision.Put(platform.NewCaller(decision, context.Member, app, false, false), receipt, value)
		},
		Records:     decision.Records,
		KeepChanges: decision.KeepChanges,
		Register:    view.registerInstalledAsset,
		Decide: func(info platform.EntityInfo, draft platform.AssetDraft, submission *pb.Submission) (*pb.ChangeRecord, error) {
			app := view.App(info.App)
			if request != nil {
				if refusal := t.delegatedBound(context.Member, submission); refusal != nil {
					return nil, refusal
				}
			}
			answer, rejected := platform.Decide(platform.NewCaller(decision, context.Member, info.App, false, context.ManagedBy != ""), app, submission, context.At)
			if rejected != nil {
				return nil, releaseowner.DraftRefusal(app, draft, rejected)
			}
			return answer, nil
		},
		Rebind: func(appID string) (*platform.AssetRegistry, error) {
			private := decision.stateApp(view.App(appID))
			apps := slices.Clone(view.apps)
			apps[view.appIndex[appID]] = private
			view.ownerSet = view.with(apps)
			(hostView{t: view, app: private}).bindOwner(private)
			return view.assetRegistryLocked()
		},
		AttachHosts: decision.attachPrivateHosts,
	}.Run()
	if err != nil {
		return nil, nil, err
	}
	if !context.DraftsOnly {
		view.installingSet = false
		if err := view.validateAssetSet(view.services.Rules); err != nil {
			return nil, nil, err
		}
	}
	if receipt == nil || len(existing) > 0 {
		return decision, nil, nil
	}
	if refusal := decision.answerRequests(context.At); refusal != nil {
		return nil, nil, refusal
	}
	if refusal := decision.stageObservers(); refusal != nil {
		return nil, nil, refusal
	}
	raw, err := decision.batchResult(receipt.GetSubmission().GetAuthority(), receipt.GetSubmission(), receipt, context.At)
	return decision, raw, err
}

func (t *Tenant) prepareReleaseLifecycleLocked(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade bool) ([]platform.ReleaseInstallation, json.RawMessage, error) {
	return releaseowner.PrepareLifecycle(releaseInstallationRuntime{t}, candidate, member, at, upgrade)
}

type releaseInstallationRuntime struct{ t *Tenant }

func (r releaseInstallationRuntime) HasAgentRuntime() bool { return r.t.agents != nil }

func (r releaseInstallationRuntime) ReleaseProject(candidate platform.ReleaseCandidate) string {
	return releaseowner.ResourceProject(candidate, records.LockedRows[build.Resource](r.t.Projection, build.ResourceType))
}

func (r releaseInstallationRuntime) Registry() (*platform.AssetRegistry, error) {
	return r.t.assetRegistryLocked()
}
func (r releaseInstallationRuntime) Bootstrap(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly bool) (releaseowner.InstallationRuntime, json.RawMessage, error) {
	decision, raw, err := r.t.bootstrapOwnerDraftsLockedWithArchived(candidate, context, definitionOnly, nil, false)
	if err != nil {
		return nil, nil, err
	}
	view, err := r.t.assetInstallationView(decision.Records)
	if err != nil {
		return nil, nil, err
	}
	return releaseInstallationRuntime{view}, raw, nil
}
func (r releaseInstallationRuntime) Predecessor(record string) (string, error) {
	return r.t.Projection.RowDigest(record)
}
func (r releaseInstallationRuntime) Stage(plans []platform.ReleaseInstallation, upgrade bool) (releaseowner.InstallationRuntime, error) {
	trial, err := r.t.stageReleaseInstallationLocked(plans, true, upgrade)
	if err != nil {
		return nil, err
	}
	return releaseInstallationRuntime{trial}, nil
}
func (r releaseInstallationRuntime) CheckExtensions() error { return r.t.checkExtensionSet() }
func (r releaseInstallationRuntime) CheckStorage(trial releaseowner.InstallationRuntime, upgrade bool) error {
	return r.t.validateAssetStorage(trial.(releaseInstallationRuntime).t, upgrade, false)
}
func (r releaseInstallationRuntime) Pending(id string, raw []byte) error {
	return r.t.pendingWorkFitsLocked(id, raw)
}

func (t *Tenant) registerInstalledAsset(plan platform.AssetInstallation) error {
	return releaseowner.RegisterInstalledDefinition(t.definitions, plan, t.definitionOwners(false).Definition, t.setDefinitions)
}

func (t *Tenant) registerOwnedAssetMetadata() error {
	return releaseowner.RegisterOwnedAssetMetadata(t.definitions, t.ownedAssetDefinitions, t.setDefinitions)
}
