package host

import (
	"context"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"time"
)

func (d *stagedDecision) sources() runtimekernel.ResultSources {
	return runtimekernel.ResultSources{Tenant: d.tenant.ID, App: d.tenant.App, Control: d.savedControl, Controlled: d.control != nil, Pending: len(d.requests) > 0,
		Observers: len(d.tenant.observers) > 0, Operations: d.savedOperationCancellations, Plan: d.routingTenant().planAcceptedEvent,
		Pure: build.CallbackFreeTransition}
}

func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	return t.ApplyRecordResult(l, raw, runtimekernel.RecordApply{App: t.App, CheckBindings: build.CheckRunApplicationBindings, DefinitionType: build.AppType,
		Claims: func(result acceptedformat.Record, receipt *pb.ChangeRecord) (func() error, error) {
			return t.Claims.RecordClaims(t.ID, result, receipt)
		}})
}

func (t *Tenant) acceptResult(entry Entry, key, hash string) ([]byte, error) {
	if t.quarantined() {
		return nil, fmt.Errorf("tenant %s is stopped", t.ID)
	}
	if t.readError() != nil {
		return nil, t.readError()
	}
	return t.Accept(entry, key, hash, storageDelta, t.quarantine)
}

func (t *Tenant) acceptedResultApplied() {
	t.StateEpoch.Add(1)
	t.Projection.Lock()
	if t.Projection.Current().Position() != nil {
		t.Projection.Current().MarkApplied(t.Projection.Current().Position()())
	}
	if t.currentReadScopes {
		t.Projection.UseCurrentRows()
	}
	t.Projection.Unlock()
	if t.currentReadScopes {
		t.closeRecordRead()
		t.openRecordRead(context.Background())
	}
}

func (t *Tenant) settlement() runtimekernel.Settlement {
	return runtimekernel.Settlement{
		Quarantine:        t.quarantine,
		Applied:           t.acceptedResultApplied,
		Enqueue:           t.enqueue,
		ChangedOwner:      t.changedOwner,
		OperationsChanged: t.operationsChanged,
	}
}

func (t *Tenant) changeApply() runtimekernel.ChangeApply {
	return runtimekernel.ChangeApply{Batch: t.batchApply(),
		Check: func(plan acceptedformat.ChangePlan) error {
			if err := t.validateAcceptedControl(plan.Control); err != nil {
				return err
			}
			if err := t.Notices.Check(plan.Notices); err != nil {
				return err
			}
			if err := t.validateAcceptedIntents(plan.Intents); err != nil {
				return err
			}
			if err := runtimekernel.CheckStates(t.states, plan.States); err != nil {
				return err
			}
			return t.sequences.Check(plan.SequenceBases, plan.Sequences)
		},
		Apply: func(plan acceptedformat.ChangePlan) error {
			if err := t.applyAcceptedControl(plan.Control); err != nil {
				return err
			}
			if err := runtimekernel.ApplyStates(t.states, plan.States); err != nil {
				return err
			}
			t.applyAcceptedNotices(plan.Notices)
			t.installAcceptedIntents(plan.Intents)
			t.sequences.Set(plan.Sequences)
			return nil
		}}
}

func (t *Tenant) prepareAcceptedChanges(plan acceptedformat.ChangePlan) (*runtimekernel.PreparedChanges, error) {
	return t.PrepareChanges(plan, t.changeApply())
}

func (t *Tenant) publishAcceptedChanges(p *runtimekernel.PreparedChanges) {
	if p.Child != nil {
		t.publishAcceptedBatch(p.Child.Result)
	}
}

func (t *Tenant) prepareAcceptedDirectRows(rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error) {
	return runtimekernel.ApplyDirectRows(t.Projection, rows, member, at, build.CheckRunApplicationBindings)
}
