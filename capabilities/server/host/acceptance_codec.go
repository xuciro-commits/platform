package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"reflect"
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
	if d.control != nil || d.failure != nil || len(d.allocated) > 0 || d.notices.Changed() ||
		len(d.intents) > 0 || len(d.observations) > 0 || len(d.states) > 0 ||
		len(d.operationCancels) > 0 ||
		d.observed && len(d.tenant.observers) > 0 {
		return nil, fmt.Errorf("decision needs a result carrying all state and intents")
	}
	if receipt == nil || len(d.events) != 1 || d.records.WriteCount() != 1 || len(d.logs) != 1 {
		return nil, fmt.Errorf("one accepted record and event are required")
	}
	accepted := false
	for _, log := range d.logs {
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
	image, err := func() (records.Image, error) {
		d.records.Lock()
		defer d.records.Unlock()
		et := d.records.Type(typ)
		if et == nil || et.Info().App != app || et.Rows().Get(id) == nil {
			return records.Image{}, fmt.Errorf("decision has no declared record image")
		}
		if et.Rows().Get(id).Projection != nil {
			return records.Image{}, fmt.Errorf("versioned migration row requires the complete record batch")
		}
		var prior *records.Row
		if old := d.records.Parent().Type(typ); old != nil {
			prior = old.Rows().Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.Rows().Get(id), prior)
		if err == nil {
			image.Predecessor, err = records.Predecessor(typ, id, prior)
		}
		return image, err
	}()
	if err != nil {
		return nil, err
	}
	raw, err := protojson.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	hash, err := acceptedformat.SubmissionHash(receipt.GetSubmission(), d.schemaVersion)
	if err != nil {
		return nil, err
	}
	version := acceptedformat.RecordVersion
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
	result := acceptedformat.Record{Version: version, Tenant: receipt.GetSubmission().GetTenantId(),
		Definition: d.definitions[app+"/"+receipt.GetChangeId()],
		Kind:       kind, App: app, At: submittedAt.UTC(), RequestHash: hash, Receipt: raw, SchemaVersion: d.schemaVersion,
		Row:   image,
		Event: plan, Authorizations: slices.Clone(d.authorizations)}
	result.Unique = d.savedUniqueChanges()
	result.Digest, err = acceptedformat.DigestRecord(result)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := acceptedformat.DecodeRecord(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	result, receipt, err := acceptedformat.DecodeRecord(raw)
	if err != nil {
		return false, err
	}
	if result.Tenant != t.ID || l == nil || !slices.ContainsFunc(l.Declarations(), func(d *pb.AuthorityDeclaration) bool {
		return d.GetTenantId() == result.Tenant && d.GetAuthorityId() == result.App && d.GetDataClass() == result.Row.Type
	}) {
		return false, fmt.Errorf("accepted result belongs to another tenant or authority")
	}
	s := t.Projection
	draft := s.ForkRecords()
	draft.Lock()
	et := draft.Type(result.Row.Type)
	if et == nil || et.Info().App != result.App {
		draft.Unlock()
		return false, fmt.Errorf("accepted result needs installed entity %s", result.Row.Type)
	}
	value, decoded, err := runtimekernel.DecodeAcceptedRecord(et, draft, result, receipt, checkRunApplicationBindings)
	if err != nil {
		draft.Unlock()
		return false, err
	}
	rec := records.Header(value)
	var source *preparedSourceClaim
	if len(result.Claims) > 0 {
		source = &preparedSourceClaim{origin: result.Origin, claims: result.Claims}
		if _, err := t.sourceFacts(result.Row.Type, result.Row.ID, receipt.GetChangeId(), source); err != nil {
			draft.Unlock()
			return false, err
		}
	}
	var publisher platform.AcceptedPublisher
	if result.Version == 3 || result.Version == 5 && result.Kind == "publication" {
		schema := receipt.GetSubmission().GetSchema().GetName()
		var ok bool
		publisher, ok = t.App(result.App).(platform.AcceptedPublisher)
		if !ok || !slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
			draft.Unlock()
			return false, fmt.Errorf("accepted publication has no installer")
		}
		if err := publisher.ValidateAcceptedPublication(schema, decoded); err != nil {
			draft.Unlock()
			return false, fmt.Errorf("accepted publication is invalid: %w", err)
		}
	}
	prior := et.Rows().Get(rec.ID)
	if prior != nil && reflect.DeepEqual(prior.Value.Interface(), value.Interface()) &&
		records.ImageAlreadyApplied(prior, result.Row) {
		draft.Unlock()
		changed, err := l.ApplyAcceptedChange(receipt)
		if err == nil {
			t.Committed.SaveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
			t.Committed.SaveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
			err = t.applySourceClaims(result.Row.Type, result.Row.ID, receipt.GetChangeId(), result.At, source)
		}
		return changed, err
	}
	if err := runtimekernel.InstallAcceptedRecord(draft, et, result, receipt, value, prior); err != nil {
		draft.Unlock()
		return false, err
	}
	draft.Unlock()
	if result.Version >= 5 {
		if err := runtimekernel.ValidateUniqueReplacements(draft, result.Unique); err != nil {
			return false, err
		}
	}
	changed, err := l.ApplyAcceptedChange(receipt)
	if err != nil {
		return false, err
	}
	if err := s.PromoteRecords(draft); err != nil {
		return false, err
	}
	if err := t.applySourceClaims(result.Row.Type, result.Row.ID, receipt.GetChangeId(), result.At, source); err != nil {
		return false, err
	}
	if result.Row.Type == build.AppType {
		t.DefinitionEpoch.Add(1)
	}
	t.Committed.SaveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
	t.Committed.SaveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
	if changed && publisher != nil {
		if err := publisher.ApplyAcceptedPublication(receipt.GetSubmission().GetSchema().GetName(), decoded); err != nil {
			return false, err
		}
	}
	return changed, nil
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
