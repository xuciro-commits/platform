package host

import (
	"context"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (d *stagedDecision) sources() runtimekernel.ResultSources {
	return runtimekernel.ResultSources{Tenant: d.tenant.ID, App: d.tenant.App, Control: d.savedControl, Controlled: d.control != nil, Pending: len(d.requests) > 0,
		Observers: len(d.tenant.observers) > 0, Operations: d.savedOperationCancellations, Plan: d.routingTenant().planAcceptedEvent,
		Pure: build.CallbackFreeTransition}
}

func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	return t.ApplyRecordResult(l, raw, runtimekernel.RecordApply{App: t.App, CheckBindings: build.CheckRunApplicationBindings, DefinitionType: build.AppType,
		Claims: func(result acceptedformat.Record, receipt *pb.ChangeRecord) (func() error, error) {
			var source *runtimekernel.ClaimSet
			if len(result.Claims) > 0 {
				source = &runtimekernel.ClaimSet{Origin: result.Origin, Claims: result.Claims}
				if _, err := t.claims.Prepare(t.ID, result.Row.Type, result.Row.ID, receipt.GetChangeId(), source); err != nil {
					return nil, err
				}
			}
			return func() error {
				return t.claims.Apply(t.ID, result.Row.Type, result.Row.ID, receipt.GetChangeId(), result.At, source)
			}, nil
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
