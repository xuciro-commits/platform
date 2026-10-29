package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"encoding/json"
	"fmt"

	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/studio/build"

	"platformserver/kernel/records"
	"platformserver/platform"
)

func (t *Tenant) releaseBootstrapLocked(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly ...bool) (*stagedDecision, json.RawMessage, error) {
	return t.bootstrapOwnerDraftsLocked(candidate, context, len(definitionOnly) > 0 && definitionOnly[0], nil)
}

func (t *Tenant) bootstrapOwnerDraftsLocked(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly bool, request *releaseowner.OwnerDraftRequest) (*stagedDecision, json.RawMessage, error) {
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil, nil, err
	}
	drafts, err := registry.ReleaseDrafts(candidate.Assets, context)
	if err != nil {
		return nil, nil, err
	}
	drafts, err = releaseowner.SelectOwnerDrafts(drafts, context, request)
	if err != nil {
		return nil, nil, err
	}
	decision := t.newStagedDecision()
	view, err := t.assetInstallationView(decision.records)
	if err != nil {
		return nil, nil, err
	}
	view.installingSet = true
	defer func() { view.installingSet = false }()
	if definitionOnly {
		shapes, err := t.releaseAssetShapes(candidate.Assets)
		if err != nil {
			return nil, nil, err
		}
		for typ, shape := range shapes {
			if old := view.records.Type(typ); old != nil && old.Info().Go != shape.Go {
				old.SetRows(records.Index[*records.Row]{})
			}
		}
	}
	decision.tenant, decision.records = view, view.records
	decision.records.Current().SelectRelease(candidate.ID)
	for i, app := range view.apps {
		if _, ok := app.(platform.AssetLifecycleOwner); !ok {
			continue
		}
		private := decision.stateApp(app)
		view.apps[i] = private
		(hostView{t: view, app: private}).bindOwner(private)
	}
	for key, app := range view.owner {
		view.owner[key] = view.app(app.Manifest().ID)
	}
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
	declare := func(asset platform.ReleaseAsset) error {
		if context.DraftsOnly {
			return nil
		}
		plans, err := registry.PrepareReleaseAssets([]platform.ReleaseAsset{asset}, context)
		if err != nil {
			return err
		}
		for _, plan := range plans {
			if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{Private: true}); err != nil {
				return err
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
		if !hasDraft[asset.Ref] {
			if err := declare(asset); err != nil {
				return nil, nil, err
			}
		}
	}
	var receipt *pb.ChangeRecord
	for ordinal, draft := range drafts {
		et := decision.records.Type(draft.Type)
		if et == nil {
			return nil, nil, fmt.Errorf("%s does not host %s", t.ID, draft.Type)
		}
		nameField, ok := et.Info().Field("name")
		if !ok {
			return nil, nil, fmt.Errorf("asset draft %s has no owner identity", draft.Type)
		}
		matches := 0
		logicalName := draft.Ref.Name
		if draft.Ref.Kind == platform.AssetObject || draft.Ref.Kind == platform.AssetFlow {
			logicalName = strings.TrimPrefix(logicalName, build.ID+".")
		}
		for _, row := range et.Rows().All() {
			if !row.Value.FieldByName("Record").Interface().(platform.Record).Archived && row.Value.FieldByIndex(nameField.Index).String() == logicalName {
				matches++
			}
		}
		if matches > 1 {
			return nil, nil, fmt.Errorf("asset draft %s/%s is ambiguous", draft.Type, draft.Name)
		}
		if request != nil && (matches > 0 || et.Rows().Get(draft.Name) != nil) {
			return nil, nil, fmt.Errorf("asset %s already has a draft; open that draft instead", draft.Ref)
		}
		app := view.app(et.Info().App)
		if matches == 0 {
			raw, err := releaseowner.AssetDraftPayload(et.Info(), draft.Image)
			if err != nil {
				return nil, nil, err
			}
			key := "release-bootstrap:" + candidate.ID + ":" + context.Member.ID + ":" + draft.Type + "/" + draft.Name
			if request != nil {
				key = request.Key + "#" + strconv.Itoa(ordinal)
			}
			submission := &pb.Submission{TenantId: t.ID, PrincipalId: context.Member.ID, Authority: et.Info().App,
				IdempotencyKey: key,
				Target:         &pb.EntityRef{Type: draft.Type, Id: draft.Name}, Schema: &pb.SchemaRef{Name: draft.Schema, Version: 1}, Payload: raw}
			if request != nil {
				if refusal := t.delegatedBound(context.Member, submission); refusal != nil {
					return nil, nil, refusal
				}
			}
			answer, rejected := platform.Decide(platform.NewCaller(decision, context.Member, et.Info().App, false, false), app, submission, context.At)
			if rejected != nil {
				if rejected.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					action, _ := app.Manifest().Actions.Action(draft.Schema)
					return nil, nil, fmt.Errorf("create target draft %s/%s needs the %s role and matching token scope: %w", draft.Type, draft.Name, strings.Join(action.Roles, " role or "), rejected)
				}
				if rejected.Message != "" {
					return nil, nil, fmt.Errorf("create target draft %s/%s: %s: %w", draft.Type, draft.Name, rejected.Message, rejected)
				}
				return nil, nil, fmt.Errorf("create target draft %s/%s: %w", draft.Type, draft.Name, rejected)
			}
			receipt = answer
		}
		private := decision.stateApp(app)
		for i, registered := range view.apps {
			if registered.Manifest().ID == et.Info().App {
				view.apps[i] = private
			}
		}
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
	if receipt == nil {
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
	if err := checkApplicationProposalFunctions(candidate); err != nil {
		return nil, nil, err
	}
	return t.prepareReleaseInstallationLocked(candidate, member, at, upgrade, true)
}

func (t *Tenant) prepareReleaseInstallationLocked(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade, activating bool) ([]platform.ReleaseInstallation, json.RawMessage, error) {
	return releaseowner.PrepareInstallation(releaseInstallationRuntime{t}, candidate, member, at, upgrade, activating)
}

type releaseInstallationRuntime struct{ t *Tenant }

func (r releaseInstallationRuntime) HasAgentRuntime() bool { return r.t.agents != nil }
func (r releaseInstallationRuntime) Registry() (*platform.AssetRegistry, error) {
	return r.t.assetRegistryLocked()
}
func (r releaseInstallationRuntime) Bootstrap(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly bool) (releaseowner.InstallationRuntime, json.RawMessage, error) {
	decision, raw, err := r.t.releaseBootstrapLocked(candidate, context, definitionOnly)
	if err != nil {
		return nil, nil, err
	}
	view, err := r.t.assetInstallationView(decision.records)
	if err != nil {
		return nil, nil, err
	}
	return releaseInstallationRuntime{view}, raw, nil
}
func (r releaseInstallationRuntime) Predecessor(record string) (string, error) {
	return r.t.installationPredecessor(record)
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
	return r.t.validateAssetStorage(trial.(releaseInstallationRuntime).t, upgrade)
}
func (r releaseInstallationRuntime) Pending(id string, raw []byte) error {
	return r.t.pendingWorkFitsLocked(id, raw)
}

func (t *Tenant) registerInstalledAsset(plan platform.AssetInstallation) error {
	definition, indexed, err := t.ownedAssetDefinition(plan)
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
	return draft.Anchor(t.records)
}

func (t *Tenant) registerOwnedAssetMetadata() error {
	definitions, err := t.ownedAssetDefinitions(t.definitions)
	if err == nil {
		t.setDefinitions(definitions)
	}
	return err
}
