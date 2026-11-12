package host

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"strings"
	"time"
)

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a, ok := d.tenant.App(app).(platform.App)
	if !ok || c.Automation || c.Tenant != d.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if d.ProbeOnly {
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
	return d.BatchResult(d.sources(), app, request, receipt, at)
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
			if err := runtimekernel.CheckObservations(t.App, result.Observations); err != nil {
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
			if err := runtimekernel.ApplyObservations(t.App, result.Observations); err != nil {
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
		t.publishAccepted(platform.Event{App: decision.Event.App, Record: r, Changed: decision.Event.Changed}, decision.Event)
	}
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
