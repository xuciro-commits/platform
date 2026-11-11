package host

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
	"strings"
	"time"
)

func (d *stagedDecision) sealResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.checkFinalConstraints(); err != nil {
		return nil, err
	}
	owner := d.tenant.App(app)
	if owner == nil || request.GetAuthority() != app {
		return nil, fmt.Errorf("decision has no matching submission owner")
	}
	action, declared := owner.Manifest().Actions.Action(request.GetSchema().GetName())
	if !declared || action.Target != request.GetTarget().GetType() {
		return nil, fmt.Errorf("submission target differs from its declared action")
	}
	if len(d.businessFacts) == 0 && !strings.HasPrefix(request.GetCorrelationId(), "correction:") && proto.Equal(request, receipt.GetSubmission()) {
		if raw, err := d.result(app, receipt, at); err == nil {
			return raw, nil
		}
	}
	return d.batchResult(app, request, receipt, at)
}

func (d *stagedDecision) result(app string, receipt *pb.ChangeRecord, submittedAt time.Time) ([]byte, error) {
	if d.control != nil || d.Failure != nil || len(d.allocated) > 0 || d.notices.Changed() ||
		len(d.intents) > 0 || len(d.observations) > 0 || len(d.states) > 0 ||
		len(d.operationCancels) > 0 ||
		d.observed && len(d.tenant.observers) > 0 {
		return nil, fmt.Errorf("decision needs a result carrying all state and intents")
	}
	if receipt == nil || len(d.events) != 1 || d.records.WriteCount() != 1 || len(d.Logs) != 1 {
		return nil, fmt.Errorf("one accepted record and event are required")
	}
	accepted := false
	for _, log := range d.Logs {
		accepted = proto.Equal(log.Last(receipt.GetSubmission().GetTenantId()), receipt)
	}
	if !accepted {
		return nil, fmt.Errorf("decision has no staged kernel receipt")
	}
	typ, id := receipt.GetSubmission().GetTarget().GetType(), receipt.GetSubmission().GetTarget().GetId()
	ref := typ + "/" + id
	if !d.records.Written(ref) || d.events[0].App != app || !proto.Equal(d.events[0].Record, receipt) ||
		len(d.events[0].Changed) != 1 || d.events[0].Changed[0] != ref {
		return nil, fmt.Errorf("decision has other record or event effects")
	}
	kind := "record"
	if publisher, ok := d.tenant.App(app).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "publication"
	} else if build.CallbackFreeTransition(d.tenant.App(app).Manifest(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "pure-transition"
	}
	plan, refusal := d.tenant.planAcceptedEvent(d.events[0])
	if refusal != nil {
		return nil, refusal
	}
	return runtimekernel.EncodeAcceptedRecord(runtimekernel.RecordResult{Store: d.records, App: app, Kind: kind, Receipt: receipt, At: submittedAt,
		SchemaVersion: d.schemaVersion, Definition: d.definitions[app+"/"+receipt.GetChangeId()], Event: plan,
		Authorizations: d.authorizations, Unique: d.savedUniqueChanges()})
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
