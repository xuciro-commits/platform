package host

import releaseowner "platformserver/services/devops/releases"

import (
	"encoding/json"
	"fmt"

	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/services/resources/authoring"

	"platformserver/kernel/records"
	"platformserver/platform"
)

func (t *Tenant) releaseBootstrapLocked(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly ...bool) (*stagedDecision, json.RawMessage, error) {
	return t.bootstrapOwnerDraftsLocked(candidate, context, len(definitionOnly) > 0 && definitionOnly[0], nil)
}

func (t *Tenant) bootstrapOwnerDraftsLocked(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly bool, request *releaseowner.OwnerDraftRequest, existing ...*stagedDecision) (*stagedDecision, json.RawMessage, error) {
	return t.bootstrapOwnerDraftsLockedWithArchived(candidate, context, definitionOnly, request, false, existing...)
}

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
	var receipt *pb.ChangeRecord
	declare := func(asset platform.ReleaseAsset) error {
		if context.DraftsOnly && asset.Ref.Kind != platform.AssetMediaSet {
			return nil
		}
		plans, err := registry.PrepareReleaseAssets([]platform.ReleaseAsset{asset}, context)
		if err != nil {
			return err
		}
		for _, plan := range plans {
			if context.DraftsOnly && plan.Ref.Kind == platform.AssetMediaSet {
				var set platform.MediaSet
				if err := json.Unmarshal(plan.Image, &set); err != nil {
					return err
				}
				caller := platform.NewCaller(decision, context.Member, plan.Ref.App, false, false)
				if refusal := decision.Put(caller, receipt, set); refusal != nil {
					return refusal
				}
				ref := plan.Ref
				images, err := records.PrepareMediaVersionImages(view.Projection, []platform.ReleaseInstallation{{ReleasePublication: platform.ReleasePublication{Schema: plan.Schema, Image: plan.Image}, Ref: &ref, Record: plan.Record}})
				if err != nil {
					return err
				}
				for _, snapshot := range images {
					if refusal := decision.Put(caller, receipt, snapshot); refusal != nil {
						return refusal
					}
				}
				for i, event := range decision.Events {
					if event.Record == receipt {
						decision.Events[i].Changed = slices.Clone(decision.Changed[event.App+"/"+receipt.GetChangeId()])
					}
				}
			}
			if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{Private: true}); err != nil {
				return err
			}
			if context.DraftsOnly {
				continue
			}
			if err := view.registerInstalledAsset(plan); err != nil {
				return err
			}
		}
		return nil
	}
	assets := map[platform.AssetRef]platform.ReleaseAsset{}
	hasDraft := map[platform.AssetRef]bool{}
	for _, draft := range drafts {
		hasDraft[draft.Ref] = true
	}
	for _, asset := range candidate.Assets {
		assets[asset.Ref] = asset
		if context.ManagedAssets != nil && !context.ManagedAssets[asset.Ref] {
			continue
		}
		if !hasDraft[asset.Ref] && !context.DraftsOnly {
			if err := declare(asset); err != nil {
				return nil, nil, err
			}
		}
	}
	for ordinal, draft := range drafts {
		et := decision.Records.Type(draft.Type)
		if et == nil {
			return nil, nil, fmt.Errorf("%s does not host %s", t.ID, draft.Type)
		}
		matches, err := decision.Records.DraftMatches(draft.Type, releaseowner.DraftLogicalName(draft.Ref), includeArchived, context.ManagedBy != "")
		if err != nil {
			return nil, nil, err
		}
		if matches > 1 {
			return nil, nil, fmt.Errorf("asset draft %s/%s is ambiguous", draft.Type, draft.Name)
		}
		if request != nil && (matches > 0 || et.Rows().Get(draft.Name) != nil) {
			return nil, nil, fmt.Errorf("asset %s already has a draft; open that draft instead", draft.Ref)
		}
		app := view.App(et.Info().App)
		if matches == 0 {
			submission, err := releaseowner.DraftSubmission(t.ID, candidate.ID, context, request, ordinal, draft, et.Info())
			if err != nil {
				return nil, nil, err
			}
			if request != nil {
				if refusal := t.delegatedBound(context.Member, submission); refusal != nil {
					return nil, nil, refusal
				}
			}
			answer, rejected := platform.Decide(platform.NewCaller(decision, context.Member, et.Info().App, false, context.ManagedBy != ""), app, submission, context.At)
			if rejected != nil {
				return nil, nil, releaseowner.DraftRefusal(app, draft, rejected)
			}
			receipt = answer
		}
		private := decision.stateApp(app)
		apps := slices.Clone(view.apps)
		apps[view.appIndex[et.Info().App]] = private
		view.ownerSet = view.with(apps)
		(hostView{t: view, app: private}).bindOwner(private)
		registry, err = view.assetRegistryLocked()
		if err != nil {
			return nil, nil, err
		}
		if err := declare(assets[draft.Ref]); err != nil {
			return nil, nil, err
		}
		decision.attachPrivateHosts()
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
	if err := build.CheckProposalFunctions(candidate); err != nil {
		return nil, nil, err
	}
	return t.prepareReleaseInstallationLocked(candidate, member, at, upgrade, true)
}

func (t *Tenant) prepareReleaseInstallationLocked(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade, activating bool) ([]platform.ReleaseInstallation, json.RawMessage, error) {
	return releaseowner.PrepareInstallation(releaseInstallationRuntime{t}, candidate, member, at, upgrade, activating)
}

type releaseInstallationRuntime struct{ t *Tenant }

func (r releaseInstallationRuntime) HasAgentRuntime() bool { return r.t.agents != nil }

func (r releaseInstallationRuntime) ReleaseProject(candidate platform.ReleaseCandidate) string {
	r.t.Projection.Lock()
	defer r.t.Projection.Unlock()
	resources := r.t.Projection.Type(build.ResourceType)
	if resources == nil {
		return ""
	}
	rows := []build.Resource{}
	for _, row := range resources.Rows().All() {
		rows = append(rows, row.Value.Interface().(build.Resource))
	}
	return releaseowner.ResourceProject(candidate, rows)
}

func (r releaseInstallationRuntime) Registry() (*platform.AssetRegistry, error) {
	return r.t.assetRegistryLocked()
}
func (r releaseInstallationRuntime) Bootstrap(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly bool) (releaseowner.InstallationRuntime, json.RawMessage, error) {
	decision, raw, err := r.t.releaseBootstrapLocked(candidate, context, definitionOnly)
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
	definition, indexed, err := t.definitionOwners(false).Definition(plan)
	if err != nil || !indexed {
		return err
	}
	for i, existing := range t.definitions {
		if existing.Ref == plan.Ref {
			t.definitions[i] = definition
			return nil
		}
	}
	t.setDefinitions(append(t.definitions, definition))
	return nil
}

func (t *Tenant) anchorReleaseRecords(draft *records.Store) error {
	if draft == nil {
		return fmt.Errorf("release has no prepared record view")
	}
	return draft.Anchor(t.Projection)
}

func (t *Tenant) registerOwnedAssetMetadata() error {
	definitions, err := t.ownedAssetDefinitions(t.definitions)
	if err == nil {
		t.setDefinitions(definitions)
	}
	return err
}
