package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/host"
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

func (t *Tenant) assetReleaseContextLocked(registry *platform.AssetRegistry, assets []platform.ReleaseAsset, member platform.Member, at time.Time, upgrade bool) (platform.AssetReleaseContext, error) {
	context := platform.AssetReleaseContext{Member: member, At: at, Upgrade: upgrade}
	requirements, err := registry.EnvironmentRequirements(assets)
	if err != nil {
		return context, err
	}
	for _, requirement := range requirements {
		if requirement.Ref.App != build.ID {
			return context, fmt.Errorf("environment binding %s has no supported owner", requirement.Ref)
		}
		records, _, refusal := platform.Find[build.Connection](t.automation(build.ID, false), platform.Query{})
		if refusal != nil {
			return context, refusal
		}
		var matched []build.Connection
		for _, record := range records {
			if !record.Archived && record.Name == requirement.Ref.Name {
				matched = append(matched, record)
			}
		}
		if len(matched) == 0 {
			return context, fmt.Errorf("release needs a target environment binding for %s", requirement.Ref)
		}
		if len(matched) != 1 {
			return context, fmt.Errorf("target environment binding %s is ambiguous", requirement.Ref)
		}
		connection := matched[0]
		if connection.State != "ready" || requirement.Profile != "" && connection.Kind != requirement.Profile {
			return context, fmt.Errorf("target environment binding %s is not ready or has an incompatible connection kind", requirement.Ref)
		}
		if requirement.Credential {
			if connection.Secret == "" || t.Secrets == nil {
				return context, fmt.Errorf("target environment binding %s needs its own credential", requirement.Ref)
			}
			if _, ok := t.Secrets(connection.Secret); !ok {
				return context, fmt.Errorf("target environment binding %s has no credential", requirement.Ref)
			}
		}
		context.Bindings = append(context.Bindings, platform.AssetEnvironmentBinding{Ref: requirement.Ref, TargetRecord: connection.ID})
	}
	return context, nil
}

// Missing drafts are real owner create decisions, but remain private until
// their enclosing release and definition images have all been accepted.
func (t *Tenant) releaseBootstrapLocked(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext, definitionOnly ...bool) (*stagedDecision, json.RawMessage, error) {
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil, nil, err
	}
	drafts, err := registry.ReleaseDrafts(candidate.Assets, context)
	if err != nil {
		return nil, nil, err
	}
	decision := t.newStagedDecision()
	view, err := t.assetInstallationView(decision.records)
	if err != nil {
		return nil, nil, err
	}
	if len(definitionOnly) > 0 && definitionOnly[0] {
		shapes, err := t.releaseAssetShapes(candidate.Assets)
		if err != nil {
			return nil, nil, err
		}
		for typ, shape := range shapes {
			if old := view.records.types[typ]; old != nil && old.info.Go != shape.Go {
				old.setRows(recordIndex[*row]{})
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
	for _, draft := range drafts {
		et := decision.records.types[draft.Type]
		if et == nil {
			return nil, nil, fmt.Errorf("%s does not host %s", t.ID, draft.Type)
		}
		nameField, ok := et.info.Field("name")
		if !ok {
			return nil, nil, fmt.Errorf("asset draft %s has no owner identity", draft.Type)
		}
		matches := 0
		for _, row := range et.rows.all() {
			if !row.value.FieldByName("Record").Interface().(platform.Record).Archived && row.value.FieldByIndex(nameField.Index).String() == draft.Name {
				matches++
			}
		}
		if matches > 1 {
			return nil, nil, fmt.Errorf("asset draft %s/%s is ambiguous", draft.Type, draft.Name)
		}
		app := view.app(et.info.App)
		if matches == 0 {
			var image map[string]json.RawMessage
			if err := json.Unmarshal(draft.Image, &image); err != nil {
				return nil, nil, err
			}
			payload := map[string]json.RawMessage{}
			for _, field := range et.info.Fields {
				if value, ok := image[field.Name]; ok && !field.ReadOnly && string(value) != "null" {
					payload[field.Name] = value
				}
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				return nil, nil, err
			}
			submission := &pb.Submission{TenantId: t.ID, PrincipalId: context.Member.ID, Authority: et.info.App,
				IdempotencyKey: "release-bootstrap:" + candidate.ID + ":" + context.Member.ID + ":" + draft.Type + "/" + draft.Name,
				Target:         &pb.EntityRef{Type: draft.Type, Id: draft.Name}, Schema: &pb.SchemaRef{Name: draft.Schema, Version: 1}, Payload: raw}
			answer, rejected := platform.Decide(platform.NewCaller(decision, context.Member, et.info.App, false, false), app, submission, context.At)
			if rejected != nil {
				if rejected.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					action, _ := app.Manifest().Actions.Action(draft.Schema)
					return nil, nil, fmt.Errorf("create target draft %s/%s needs the %s role and matching token scope: %w", draft.Type, draft.Name, strings.Join(action.Roles, " role or "), rejected)
				}
				return nil, nil, fmt.Errorf("create target draft %s/%s: %w", draft.Type, draft.Name, rejected)
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

func (t *Tenant) prepareReleaseLifecycleLocked(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade bool) ([]releaseInstallation, json.RawMessage, error) {
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

func (t *Tenant) prepareReleaseInstallationLocked(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade, activating bool) ([]releaseInstallation, json.RawMessage, error) {
	if t.agents == nil && slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool {
		return asset.Ref.App == build.ID && asset.Ref.Kind == platform.AssetAgent
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
	installations := make([]releaseInstallation, 0, len(plans))
	for _, plan := range plans {
		before, err := view.installationPredecessor(plan.Record)
		if err != nil {
			return nil, nil, err
		}
		ref := plan.Ref
		installations = append(installations, releaseInstallation{ReleasePublication: build.ReleasePublication{Schema: plan.Schema, Image: plan.Image}, Before: before, Ref: &ref, Record: plan.Record})
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
	definition, indexed, err := ownedAssetDefinition(plan)
	if err != nil || !indexed {
		return err
	}
	for i, existing := range t.definitions {
		if existing.Ref == plan.Ref {
			t.definitions[i] = definition
			return nil
		}
	}
	t.definitions = append(t.definitions, definition)
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
		t.definitions = definitions
	}
	return err
}
