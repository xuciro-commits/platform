package platformserver

import (
	"encoding/json"
	"fmt"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/internal/records"
	"platformserver/platform"
)

func (t *Tenant) assetReleaseContextLocked(registry *platform.AssetRegistry, assets []platform.ReleaseAsset, member platform.Member, at time.Time, upgrade bool) (platform.AssetReleaseContext, error) {
	context := platform.AssetReleaseContext{Member: member, At: at, Upgrade: upgrade}
	requirements, err := registry.EnvironmentRequirements(assets)
	if err != nil {
		return context, err
	}
	for _, requirement := range requirements {
		owner, err := registry.Owner(requirement.Ref)
		if err != nil {
			return context, err
		}
		target, err := owner.ResolveAssetEnvironment(requirement)
		if err != nil {
			return context, err
		}
		if requirement.Credential {
			connection, found := platform.Get[construction.Connection](t.automation(requirement.Ref.App, false), target.TargetRecord)
			valid := false
			if found {
				_, valid = t.credentialValue(target.Credential, "connection/"+connection.ID, connection.Address)
			} else {
				_, valid = t.secret(target.Credential)
			}
			if !valid {
				return context, fmt.Errorf("target environment binding %s has no credential", requirement.Ref)
			}
		}
		context.Bindings = append(context.Bindings, platform.AssetEnvironmentBinding{Ref: requirement.Ref, TargetRecord: target.TargetRecord})
	}
	return context, nil
}

// Missing drafts are real owner create decisions, but remain private until
// their enclosing release and definition images have all been accepted.
func (t *Tenant) releaseBootstrapLocked(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly ...bool) (*stagedDecision, json.RawMessage, error) {
	return t.bootstrapOwnerDraftsLocked(candidate, context, len(definitionOnly) > 0 && definitionOnly[0], nil)
}

type ownerDraftRequest struct {
	Key         string
	From, Until int
}

func assetDraftPayload(info platform.EntityInfo, image json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(image, &fields); err != nil {
		return nil, err
	}
	payload := map[string]json.RawMessage{}
	for _, field := range info.Fields {
		if value, ok := fields[field.Name]; ok && !field.ReadOnly && string(value) != "null" {
			payload[field.Name] = value
		}
	}
	return json.Marshal(payload)
}

func (t *Tenant) bootstrapOwnerDraftsLocked(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly bool, request *ownerDraftRequest) (*stagedDecision, json.RawMessage, error) {
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil, nil, err
	}
	drafts, err := registry.ReleaseDrafts(candidate.Assets, context)
	if err != nil {
		return nil, nil, err
	}
	if request != nil && request.Until != 0 {
		if !context.DraftsOnly || request.From < 0 || request.Until > len(drafts) || request.From >= request.Until || request.Until-request.From > 64 {
			return nil, nil, fmt.Errorf("draft batch must contain one to 64 selected owner drafts")
		}
		drafts = drafts[request.From:request.Until]
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
			if old := view.records.types[typ]; old != nil && old.info.Go != shape.Go {
				old.setRows(records.Index[*records.Row]{})
			}
		}
	}
	decision.tenant, decision.records = view, view.records
	decision.records.readRelease = candidate.ID
	for i, app := range view.apps {
		if _, ok := app.(platform.AssetLifecycleOwner); !ok {
			continue
		}
		private := decision.stateApp(app)
		view.apps[i] = private
		host.Attach(private, hostView{t: view, app: private})
	}
	for key, app := range view.owner {
		view.owner[key] = view.app(app.Manifest().ID)
	}
	// The validation owner and create decisions share one private host. Owner
	// declarations are visible here without publishing or changing live rows.
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
		et := decision.records.types[draft.Type]
		if et == nil {
			return nil, nil, fmt.Errorf("%s does not host %s", t.ID, draft.Type)
		}
		nameField, ok := et.info.Field("name")
		if !ok {
			return nil, nil, fmt.Errorf("asset draft %s has no owner identity", draft.Type)
		}
		matches := 0
		for _, row := range et.rows.All() {
			if !row.Value.FieldByName("Record").Interface().(platform.Record).Archived && row.Value.FieldByIndex(nameField.Index).String() == draft.Name {
				matches++
			}
		}
		if matches > 1 {
			return nil, nil, fmt.Errorf("asset draft %s/%s is ambiguous", draft.Type, draft.Name)
		}
		if request != nil && (matches > 0 || et.rows.Get(draft.Name) != nil) {
			return nil, nil, fmt.Errorf("asset %s already has a draft; open that draft instead", draft.Ref)
		}
		app := view.app(et.info.App)
		if matches == 0 {
			raw, err := assetDraftPayload(et.info, draft.Image)
			if err != nil {
				return nil, nil, err
			}
			key := "release-bootstrap:" + candidate.ID + ":" + context.Member.ID + ":" + draft.Type + "/" + draft.Name
			if request != nil {
				key = request.Key + "#" + strconv.Itoa(ordinal)
			}
			submission := &pb.Submission{TenantId: t.ID, PrincipalId: context.Member.ID, Authority: et.info.App,
				IdempotencyKey: key,
				Target:         &pb.EntityRef{Type: draft.Type, Id: draft.Name}, Schema: &pb.SchemaRef{Name: draft.Schema, Version: 1}, Payload: raw}
			if request != nil {
				if refusal := t.delegatedBound(context.Member, submission); refusal != nil {
					return nil, nil, refusal
				}
			}
			answer, rejected := platform.Decide(platform.NewCaller(decision, context.Member, et.info.App, false, false), app, submission, context.At)
			if rejected != nil {
				if rejected.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					action, _ := app.Manifest().Actions.Action(draft.Schema)
					return nil, nil, fmt.Errorf("create target draft %s/%s needs the %s role and matching token scope: %w", draft.Type, draft.Name, strings.Join(action.Roles, " role or "), rejected)
				}
				return nil, nil, fmt.Errorf("create target draft %s/%s: %s: %w", draft.Type, draft.Name, rejected.Message, rejected)
			}
			receipt = answer
		}
		private := decision.stateApp(app)
		for i, registered := range view.apps {
			if registered.Manifest().ID == et.info.App {
				view.apps[i] = private
			}
		}
		host.Attach(private, hostView{t: view, app: private})
		registry, err = view.assetRegistryLocked()
		if err != nil {
			return nil, nil, err
		}
		if err := declare(assets[draft.Ref]); err != nil {
			return nil, nil, err
		}
		// The decision fork reads these same installed declarations on its next
		// create; the accepted owner ledger remains untouched.
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

func (t *Tenant) prepareReleaseLifecycleLocked(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade bool) ([]resultformat.ReleaseInstallation, json.RawMessage, error) {
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetEvaluation {
			continue
		}
		descriptor, err := platform.ReadEvaluationSet(asset.Body)
		if err != nil {
			return nil, nil, err
		}
		if t.ai == nil {
			return nil, nil, fmt.Errorf("evaluation set %s needs the target AI owner", asset.Ref)
		}
		if _, _, refusal := t.ai.Callable(member, descriptor.Model); refusal != nil {
			return nil, nil, fmt.Errorf("evaluation set %s needs an enabled target model permitted to this member", asset.Ref)
		}
	}
	return t.prepareReleaseInstallationLocked(candidate, member, at, upgrade, true)
}

func (t *Tenant) prepareReleaseInstallationLocked(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade, activating bool) ([]resultformat.ReleaseInstallation, json.RawMessage, error) {
	if t.agents == nil && slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool {
		return asset.Ref.App == construction.ID && asset.Ref.Kind == platform.AssetAgent
	}) {
		return nil, nil, fmt.Errorf("agent artifacts require the target's original agent runtime")
	}
	migrated := false
	if activating {
		var err error
		migrated, err = t.migrationActivationLocked(candidate.ID)
		if err != nil {
			return nil, nil, err
		}
	}
	upgrade = upgrade || migrated
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil, nil, err
	}
	context, err := t.assetReleaseContextLocked(registry, candidate.Assets, member, at, upgrade)
	if err != nil {
		return nil, nil, err
	}
	decision, bootstrap, err := t.releaseBootstrapLocked(candidate, context, !activating)
	if err != nil {
		return nil, nil, err
	}
	view, err := t.assetInstallationView(decision.records)
	if err != nil {
		return nil, nil, err
	}
	registry, err = view.assetRegistryLocked()
	if err != nil {
		return nil, nil, err
	}
	end, err := registry.BeginBootstrap(candidate.Assets)
	if err != nil {
		return nil, nil, err
	}
	defer end()
	plans, err := registry.PrepareReleaseAssets(candidate.Assets, context)
	if err != nil {
		return nil, nil, err
	}
	installations := make([]resultformat.ReleaseInstallation, 0, len(plans))
	for _, plan := range plans {
		before, err := view.installationPredecessor(plan.Record)
		if err != nil {
			return nil, nil, err
		}
		ref := plan.Ref
		installations = append(installations, resultformat.ReleaseInstallation{ReleasePublication: construction.ReleasePublication{Schema: plan.Schema, Image: plan.Image}, Before: before, Ref: &ref, Record: plan.Record})
	}
	trial, err := view.stageReleaseInstallationLocked(installations, true, upgrade)
	if err != nil {
		return nil, nil, err
	}
	if err := trial.checkExtensionSet(); err != nil {
		return nil, nil, err
	}
	if activating {
		if err := t.validateAssetStorage(trial, upgrade); err != nil {
			return nil, nil, err
		}
	}
	if migrated {
		if err := t.validateMigratedRecordsLocked(trial, member, at); err != nil {
			return nil, nil, err
		}
	}
	if err := t.pendingWorkFitsLocked(candidate.ID, candidate.Bytes); err != nil {
		return nil, nil, err
	}
	return installations, bootstrap, nil
}

// Integration identity discovery contains no executable body or environment
// parameters. Permission filtering remains the member-facing projection.
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

// A release may prepare owner creates and definitions on successive private
// views. Collapse only that proven descendant chain; the normal store gate
// still requires the exact live predecessor and unchanged generation.
func (t *Tenant) anchorReleaseRecords(draft *recordStore) error {
	if draft == nil {
		return fmt.Errorf("release has no prepared record view")
	}
	writes := map[string]bool{}
	cursor := draft
	var generation uint64
	for cursor != t.records {
		if cursor == nil || cursor.parent == nil || cursor.sealed {
			return fmt.Errorf("release record view is not an unpromoted descendant")
		}
		parent := cursor.parent
		if parent.generation != cursor.baseGeneration {
			return fmt.Errorf("release record predecessor changed")
		}
		for ref := range cursor.writes {
			writes[ref] = true
		}
		generation = cursor.baseGeneration
		cursor = parent
	}
	draft.parent = t.records
	draft.baseGeneration = generation
	draft.writes = writes
	return nil
}

// Rebuild only the existing owners' publication index from their own stored
// images. No portable body or environment configuration becomes discovery data.
func (t *Tenant) registerOwnedAssetMetadata() error {
	definitions, err := t.ownedAssetDefinitions(t.definitions)
	if err == nil {
		t.setDefinitions(definitions)
	}
	return err
}
