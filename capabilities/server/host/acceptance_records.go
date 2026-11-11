package host

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"slices"
	"strings"
	"time"
)

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a, ok := d.tenant.App(app).(platform.App)
	if !ok || c.Automation || c.Tenant != d.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if d.probing {
		return d.Decide(c, a, s, now)
	}
	return d.requestApproval(a, c.Member, s, now)
}

func (d *stagedDecision) requestApproval(a platform.App, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	c := platform.NewCaller(d, m, a.Manifest().ID, false, false)
	if err := d.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, err := platform.Decide(probe, a, s, now)
		return err
	}); err != nil {
		return nil, err
	}
	owner, ok := d.tenant.App(platform.WorkApp).(platform.ApprovalOwner)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	return owner.RequestApproval(c, s, now)
}

func (d *stagedDecision) batchResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.checkFinalConstraints(); err != nil {
		return nil, fmt.Errorf("batch constraints: %w", err)
	}
	if receipt == nil || len(d.events) == 0 || d.failure != nil {
		return nil, fmt.Errorf("batch needs accepted decisions and all written records")
	}
	if err := d.records.ValidateChangedLinkConstraints(); err != nil {
		return nil, fmt.Errorf("batch links: %w", err)
	}
	result := acceptedformat.Batch{Version: 2, Kind: "record-batch", Tenant: d.tenant.ID,
		Definitions: slices.Clone(d.installations),
		App:         app, At: at.UTC(), Sequences: maps.Clone(d.allocated), SequenceBases: maps.Clone(d.sequenceBases),
		Notices: d.notices.Saved(), Intents: slices.Clone(d.intents), Observations: slices.Clone(d.observations)}
	var err error
	result.Authorizations, err = d.savedAuthorizations()
	if err != nil {
		return nil, fmt.Errorf("batch authorizations: %w", err)
	}
	result.Control, err = d.savedControl()
	if err != nil {
		return nil, fmt.Errorf("batch control: %w", err)
	}
	if result.Control != nil {
		result.Version = 3
	}
	result.Operations, err = d.savedOperationCancellations()
	if err != nil {
		return nil, fmt.Errorf("batch operations: %w", err)
	}
	result.States, err = runtimekernel.SavedStates(d.states, d.stateBases)
	if err != nil {
		return nil, fmt.Errorf("batch states: %w", err)
	}
	result.Receipt, err = protojson.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("batch receipt: %w", err)
	}
	if !proto.Equal(request, receipt.GetSubmission()) {
		result.Submission, err = protojson.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("batch submission: %w", err)
		}
	}
	result.SchemaVersion = d.schemaVersion
	result.RequestHash, err = acceptedformat.SubmissionHash(request, result.SchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("batch request hash: %w", err)
	}
	seen := map[string]bool{}
	for _, event := range d.events {
		owner, ok := d.tenant.App(event.App).(platform.App)
		if !ok || d.logs[owner.AcceptedLedger()] == nil {
			return nil, fmt.Errorf("batch decision has no staged ledger for %s", event.App)
		}
		logged := false
		for _, record := range d.logs[owner.AcceptedLedger()].Records(d.tenant.ID) {
			if proto.Equal(record, event.Record) {
				logged = true
				break
			}
		}
		if !logged {
			return nil, fmt.Errorf("batch decision is not in the staged ledger")
		}
		raw, err := protojson.Marshal(event.Record)
		if err != nil {
			return nil, fmt.Errorf("batch event %s: %w", event.App, err)
		}
		plan, refusal := d.routingTenant().planAcceptedEvent(event)
		if refusal != nil {
			return nil, fmt.Errorf("batch event plan %s: %w", event.App, refusal)
		}
		plan.Observed = d.observed
		plan.Hops = d.hops
		if plan.Hops > acceptedformat.MaxHops {
			plan.Stopped, plan.Subscribers = plan.Subscribers, nil
		}
		result.Decisions = append(result.Decisions, acceptedformat.Decision{Receipt: raw, Event: plan, Definition: d.definitions[event.App+"/"+event.Record.GetChangeId()],
			Publication: d.publications[event.App+"/"+event.Record.GetChangeId()]})
		if publication := result.Decisions[len(result.Decisions)-1].Publication; publication != nil {
			copied := *publication
			if et := d.records.Type(event.Record.GetSubmission().GetTarget().GetType()); et != nil {
				copied.Format = et.Info().RecordFormat
			}
			result.Decisions[len(result.Decisions)-1].Publication = &copied
		}
		for _, ref := range event.Changed {
			if !d.records.Written(ref) {
				return nil, fmt.Errorf("batch event names an unwritten record")
			}
			seen[ref] = true
		}
	}
	if len(seen) != d.records.WriteCount() {
		result.DirectRows, err = d.savedCallbackRows(seen)
		if err != nil {
			return nil, fmt.Errorf("batch direct rows: %w", err)
		}
		if len(seen)+len(result.DirectRows) != d.records.WriteCount() {
			return nil, fmt.Errorf("batch event omits a written record")
		}
		result.Version = 4
	}
	if len(d.iterations) > 0 {
		result.Iterations = slices.Clone(d.iterations)
		result.Version = 5
	}
	if len(d.businessFacts) > 0 {
		result.Business = slices.Clone(d.businessFacts)
		result.Version = 7
	}
	result.Version = 8
	if len(result.Iterations) > 0 {
		result.Version = 9
	}
	if d.observationInput != nil && d.observationInput.Intervals != nil {
		result.Version = 10
	}
	result.Unique = d.savedUniqueChanges()
	return runtimekernel.SealBatch(d.records, result, seen, d.writers)
}

func (t *Tenant) applyAcceptedBatch(l *platform.Ledger, raw []byte) (bool, error) {
	prepared, err := t.prepareAcceptedBatch(l, raw)
	if err != nil || prepared == nil {
		return false, err
	}
	return t.promoteAcceptedBatch(prepared)
}

func (t *Tenant) prepareAcceptedBatch(l *platform.Ledger, raw []byte) (*runtimekernel.PreparedBatch, error) {
	return t.PrepareBatch(l, raw, t.batchApply())
}

func (t *Tenant) promoteAcceptedBatch(prepared *runtimekernel.PreparedBatch) (bool, error) {
	return t.PromoteBatch(prepared, t.batchApply())
}

func (t *Tenant) batchApply() runtimekernel.BatchApply {
	return runtimekernel.BatchApply{App: t.App, OwnerLedger: t.ownerLedger, CheckBindings: checkRunApplicationBindings, DefinitionType: build.AppType,
		Claims: func(result acceptedformat.Batch, receipt *pb.ChangeRecord) error {
			if len(result.Claims) == 0 {
				return nil
			}
			sub, _ := acceptedformat.BatchSubmission(result, receipt)
			_, err := t.sourceFacts(sub.GetTarget().GetType(), sub.GetTarget().GetId(), receipt.GetChangeId(), &preparedSourceClaim{origin: result.Origin, claims: result.Claims})
			return err
		},
		Check: func(result acceptedformat.Batch) error {
			if err := t.validateAcceptedControl(result.Control); err != nil {
				return err
			}
			if err := t.notices.Check(result.Notices); err != nil {
				return err
			}
			if err := t.validateAcceptedIntents(result.Intents); err != nil {
				return err
			}
			if err := t.validateAcceptedObservations(result.Observations); err != nil {
				return err
			}
			if err := runtimekernel.CheckStates(t.states, result.States); err != nil {
				return err
			}
			return t.validateOperationCancellations(result.Operations)
		},
		Rows: func(draft *records.Store, result acceptedformat.Batch) error {
			return t.prepareCallbackRows(draft, result.DirectRows, result.At)
		},
		Install: func(draft *records.Store, result acceptedformat.Batch) (*records.Store, func() error, error) {
			var install func() error
			if len(result.Definitions) > 0 {
				installation, err := t.prepareBatchDefinitions(draft, result.Definitions)
				if err != nil {
					return nil, nil, err
				}
				draft = installation.Projection
				install = func() error { return t.installReleaseDefinitions(installation, result.Definitions) }
			}
			for _, decision := range result.Decisions {
				if p := decision.Publication; p != nil {
					if err := t.installPublicationLinkConstraint(draft, p.Schema, p.Image); err != nil {
						return nil, nil, err
					}
				}
			}
			return draft, install, nil
		},
		Sequences: func(result acceptedformat.Batch) error {
			return t.sequences.Check(result.SequenceBases, result.Sequences)
		},
		Installed: func(result acceptedformat.Batch) error {
			if len(result.Claims) == 0 {
				return nil
			}
			answer := new(pb.ChangeRecord)
			if err := protojson.Unmarshal(result.Receipt, answer); err != nil {
				return err
			}
			sub := answer.GetSubmission()
			return t.applySourceClaims(sub.GetTarget().GetType(), sub.GetTarget().GetId(), answer.GetChangeId(), result.At, &preparedSourceClaim{origin: result.Origin, claims: result.Claims})
		},
		Apply: func(result acceptedformat.Batch) error {
			if err := t.applyAcceptedControl(result.Control); err != nil {
				return err
			}
			if err := runtimekernel.ApplyStates(t.states, result.States); err != nil {
				return err
			}
			return t.applyOperationCancellations(result.Operations)
		},
		Settle: func(result acceptedformat.Batch) error {
			t.applyAcceptedNotices(result.Notices)
			t.installAcceptedIntents(result.Intents)
			if err := t.applyAcceptedObservations(result.Observations); err != nil {
				return err
			}
			t.sequences.Set(result.Sequences)
			return nil
		}}
}

func (t *Tenant) installReleaseDefinitions(installed *Tenant, definitions []platform.ReleaseInstallation) error {
	t.setDefinitions(installed.definitions)
	t.owner = t.rehome(installed.owner)
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return err
	}
	for _, installation := range definitions {
		plan, err := releaseowner.InstallationPlan(registry, installation)
		if err != nil {
			return err
		}
		if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{}); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) publishAcceptedBatch(result acceptedformat.Batch) {
	for _, decision := range result.Decisions {
		r := new(pb.ChangeRecord)
		_ = protojson.Unmarshal(decision.Receipt, r)
		t.publishAccepted(platform.Event{App: decision.Event.App, Record: r, Changed: decision.Event.Changed},
			decision.Event, 2)
	}
}

type callbackWrite struct {
	Source  platform.Effect
	History int
}

func (d *stagedDecision) savedCallbackRows(published map[string]bool) ([]acceptedformat.CallbackRow, error) {
	var rows []acceptedformat.CallbackRow
	for _, ref := range slices.Sorted(d.records.Writes()) {
		if published[ref] {
			continue
		}
		callback, ok := d.callbackWrites[ref]
		if !ok {
			return nil, fmt.Errorf("batch event omits a written record outside an effect callback")
		}
		direct, err := captureDirectRows(d, callback.Source.App, []string{ref}, nil, "effect answer")
		if err != nil || len(direct) != 1 {
			return nil, fmt.Errorf("callback row cannot be captured: %v", err)
		}
		historyCount := len(direct[0].History)
		if direct[0].HistoryRef != nil {
			historyCount += direct[0].HistoryRef.Base
		}
		if historyCount != callback.History {
			return nil, fmt.Errorf("callback row has a write outside its source callback")
		}
		if _, err := d.tenant.prepareAcceptedDirectRows(direct, "app:"+callback.Source.App, d.at); err != nil {
			return nil, err
		}
		rows = append(rows, acceptedformat.CallbackRow{Row: direct[0], Source: callback.Source})
	}
	return rows, nil
}

func (t *Tenant) prepareCallbackRows(target *records.Store, rows []acceptedformat.CallbackRow, at time.Time) error {
	seen := map[string]bool{}
	for _, callback := range rows {
		row, source := callback.Row, callback.Source
		ref := row.Type + "/" + row.ID
		if seen[ref] || row.App == "" || source.ID == "" || source.App != row.App {
			return fmt.Errorf("callback row has an invalid source or owner")
		}
		seen[ref] = true
		if _, ok := t.App(row.App).(platform.Answerer); !ok {
			return fmt.Errorf("callback row has no effect answer owner")
		}
		queued, _ := t.outbound.Get(source.ID)
		current := queued.Effect
		actual, err := platform.CanonicalDigest(current)
		expected, expectedErr := platform.CanonicalDigest(source)
		if current.ID == "" || err != nil || expectedErr != nil || actual != expected {
			return fmt.Errorf("callback row source effect predecessor differs")
		}
		private, err := t.prepareAcceptedDirectRows([]acceptedformat.InputRow{row}, "app:"+row.App, at)
		if err != nil {
			return err
		}
		typ, id, _ := strings.Cut(ref, "/")
		private.Lock()
		image := private.Type(typ).Rows().Get(id)
		private.Unlock()
		target.Lock()
		target.Type(typ).SetRow(id, image)
		target.TrackWrite(ref)
		if target.Type(typ).Knowledge() {
			target.MarkDirty(ref)
		}
		target.Unlock()
	}
	return nil
}

func (t *Tenant) ownerLedger(app string) *platform.Ledger {
	if owner := t.App(app); owner != nil {
		return owner.AcceptedLedger()
	}
	return nil
}
