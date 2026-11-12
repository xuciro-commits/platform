package host

import (
	"context"
	"errors"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (d *stagedDecision) sources() runtimekernel.ResultSources {
	return runtimekernel.ResultSources{Tenant: d.tenant.ID, App: d.tenant.App, Control: d.savedControl, Controlled: d.control != nil, Pending: len(d.requests) > 0,
		Observers: len(d.tenant.observers) > 0, Operations: d.savedOperationCancellations, Plan: d.routingTenant().planAcceptedEvent,
		Pure: build.CallbackFreeTransition}
}

func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	return t.ApplyRecordResult(l, raw, runtimekernel.RecordApply{App: t.App, CheckBindings: checkRunApplicationBindings, DefinitionType: build.AppType,
		Claims: func(result acceptedformat.Record, receipt *pb.ChangeRecord) (func() error, error) {
			var source *preparedSourceClaim
			if len(result.Claims) > 0 {
				source = &preparedSourceClaim{origin: result.Origin, claims: result.Claims}
				if _, err := t.sourceFacts(result.Row.Type, result.Row.ID, receipt.GetChangeId(), source); err != nil {
					return nil, err
				}
			}
			return func() error {
				return t.applySourceClaims(result.Row.Type, result.Row.ID, receipt.GetChangeId(), result.At, source)
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
	entry.Epoch = persistence.FormatEpoch
	if t.AcceptResult != nil {
		before := int64(0)
		if t.RuntimePosition != nil {
			before = t.RuntimePosition()
			t.Committed.ReleasePending()
		}
		raw, err := t.AcceptResult(entry, key, hash)
		if err != nil && (errors.Is(err, persistence.ErrCommitUnknown) ||
			t.RuntimePosition != nil && t.RuntimePosition() > before) {
			t.quarantine(fmt.Errorf("accepted result commit could not be resolved: %w", err))
		}
		if err == nil && t.RuntimePosition != nil && t.RuntimePosition() > before {
			delta, decodeErr := storageDelta(raw)
			if decodeErr != nil {
				t.quarantine(fmt.Errorf("committed state delta: %w", decodeErr))
				return nil, decodeErr
			}
			t.Committed.HoldPending(delta)
		}
		return raw, err
	}
	if t.Record != nil {
		t.Record(entry)
	}
	return entry.Body, nil
}

func (t *Tenant) recordsAcceptedResults() bool { return t.AcceptResult != nil || t.Record != nil }

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
