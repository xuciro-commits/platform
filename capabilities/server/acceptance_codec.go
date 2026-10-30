package platformserver

import runtimekernel "platformserver/kernel"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/journal"
	"platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/studio/build"
	"reflect"
	"slices"
	"strings"
	"time"
)

func resultRequestHash(raw []byte) (string, error) {
	identity, err := acceptedIdentity(raw)
	return identity.Hash, err
}

func acceptedIdentity(raw []byte) (resultIdentity, error) {
	var identity resultIdentity
	var envelope struct{ Kind string }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return identity, err
	}
	identity.Scope = "submission"
	if envelope.Kind == "host-result" {
		r, e := acceptedformat.DecodeHostControl(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "host"
		return identity, e
	}
	if envelope.Kind == "composite-result" {
		r, e := acceptedformat.DecodeComposite(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "composite"
		return identity, e
	}
	if envelope.Kind == "usage-result" {
		r, e := acceptedformat.DecodeUsage(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "usage"
		return identity, e
	}
	if envelope.Kind == "agent-result" {
		r, e := acceptedformat.DecodeAgent(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "agent"
		return identity, e
	}
	if envelope.Kind == "operation-claim" {
		result, err := acceptedformat.DecodeOperationClaim(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	if envelope.Kind == "refusal" {
		result, sub, err := acceptedformat.DecodeRefusal(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "record-batch" {
		result, receipt, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			return identity, err
		}
		sub, err := acceptedformat.BatchSubmission(result, receipt)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "work-result" {
		result, err := acceptedformat.DecodeWork(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "work"
		return identity, err
	}
	if envelope.Kind == "input-result" {
		result, err := acceptedformat.DecodeInput(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "input"
		return identity, err
	}
	if envelope.Kind == "effect-result" {
		result, err := acceptedformat.DecodeEffect(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, controlowner.PlatformApp, result.Key, result.RequestHash
		identity.Scope = "effect"
		return identity, err
	}
	if envelope.Kind == "release-result" {
		result, err := acceptedformat.DecodeRelease(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	result, receipt, err := acceptedformat.DecodeRecord(raw)
	identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, receipt.GetSubmission().GetIdempotencyKey(), result.RequestHash
	return identity, err
}

func (d *stagedDecision) sealResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if err := d.checkFinalConstraints(); err != nil {
		return nil, err
	}
	owner := d.tenant.app(app)
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
	if d.control != nil || d.failure != nil || len(d.allocated) > 0 || d.noticeChanged || len(d.noticeEffects) > 0 ||
		len(d.intents) > 0 || len(d.observations) > 0 || len(d.states) > 0 ||
		len(d.deliveries) > 0 || len(d.operationCancels) > 0 ||
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
	if publisher, ok := d.tenant.app(app).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "publication"
	} else if build.CallbackFreeTransition(d.tenant.app(app).Manifest(), receipt.GetSubmission().GetSchema().GetName()) {
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
	s := t.records
	draft := s.ForkRecords()
	draft.Lock()
	et := draft.Type(result.Row.Type)
	if et == nil || et.Info().App != result.App {
		draft.Unlock()
		return false, fmt.Errorf("accepted result needs installed entity %s", result.Row.Type)
	}
	value := reflect.New(et.Info().Go).Elem()
	decoded, err := records.DecodeFormattedImage(et.Info(), result.Row.Value, result.Row.Projection, draft.Current().Release(), result.Row.Format)
	if err != nil {
		draft.Unlock()
		return false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Addr().Interface()); err != nil {
		draft.Unlock()
		return false, fmt.Errorf("accepted record image: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		draft.Unlock()
		return false, fmt.Errorf("accepted record has trailing data")
	}
	if err := checkRunApplicationBindings(value.Interface()); err != nil {
		draft.Unlock()
		return false, err
	}
	rec := records.Header(value)
	if rec.ID != result.Row.ID || rec.Revision != receipt.GetRevision() ||
		rec.Changed.By != receipt.GetSubmission().GetPrincipalId() ||
		rec.Changed.Change != receipt.GetChangeId() ||
		!rec.Changed.At.Equal(receipt.GetRecordedTime().AsTime()) {
		draft.Unlock()
		return false, fmt.Errorf("accepted record image differs from receipt")
	}
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
		publisher, ok = t.app(result.App).(platform.AcceptedPublisher)
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
			t.committed.SaveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
			t.committed.SaveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
			err = t.applySourceClaims(result.Row.Type, result.Row.ID, receipt.GetChangeId(), result.At, source)
		}
		return changed, err
	}
	if result.Version >= 5 {
		if err := records.CheckHistoryPredecessor(result.Row, prior); err != nil {
			draft.Unlock()
			return false, err
		}
		before, err := records.Predecessor(result.Row.Type, result.Row.ID, prior)
		if err != nil || before != result.Row.Predecessor || prior != nil && !reflect.DeepEqual(records.Header(prior.Value).Created, rec.Created) || prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) {
			draft.Unlock()
			return false, fmt.Errorf("accepted record predecessor differs: %v", err)
		}
	} else {
		verb, _ := strings.CutPrefix(receipt.GetSubmission().GetSchema().GetName(), result.Row.Type+".")
		before := len(result.Row.History) - 1
		if verb == "create" && prior != nil || verb != "create" && prior == nil ||
			prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) ||
			prior == nil && before != 0 || prior != nil && (len(prior.RecordHistory()) != before ||
			before > 0 && !reflect.DeepEqual(prior.RecordHistory(), result.Row.History[:before]) ||
			!reflect.DeepEqual(records.Header(prior.Value).Created, rec.Created)) {
			draft.Unlock()
			return false, fmt.Errorf("accepted record is missing its predecessor")
		}
	}
	recovered := &records.Row{Value: value, Format: result.Row.Format, History: records.CopyHistory(result.Row.History)}
	if result.Version >= 5 {
		if err := records.AppendRowHistory(recovered, prior, result.Row.History); err != nil {
			draft.Unlock()
			return false, err
		}
	}
	recovered.RetainOriginal(result.Row.Value)
	et.SetRow(rec.ID, recovered)
	draft.TrackWrite(result.Row.Type + "/" + rec.ID)
	draft.RememberChange(result.App+"/"+receipt.GetChangeId(), result.Row.Type+"/"+rec.ID)
	if et.Knowledge() {
		draft.MarkDirty(result.Row.Type + "/" + rec.ID)
	}
	draft.Unlock()
	if result.Version >= 5 {
		if err := validateUniqueReplacements(draft, result.Unique); err != nil {
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
		t.defGen.Add(1)
	}
	t.committed.SaveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
	t.committed.SaveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
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
	if t.recordReadError != nil {
		return nil, t.recordReadError
	}
	if t.AcceptResult != nil {
		before := int64(0)
		if t.runtimePosition != nil {
			before = t.runtimePosition()
			t.committed.ReleasePending()
		}
		raw, err := t.AcceptResult(entry, key, hash)
		if err != nil && (errors.Is(err, journal.ErrCommitUnknown) ||
			t.runtimePosition != nil && t.runtimePosition() > before) {
			t.quarantine(fmt.Errorf("accepted result commit could not be resolved: %w", err))
		}
		if err == nil && t.runtimePosition != nil && t.runtimePosition() > before {
			delta, decodeErr := runtimekernel.StorageDelta(raw)
			if decodeErr != nil {
				t.quarantine(fmt.Errorf("committed state delta: %w", decodeErr))
				return nil, decodeErr
			}
			t.committed.HoldPending(delta)
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
	t.records.Lock()
	if t.records.Current().Position() != nil {
		t.records.Current().MarkApplied(t.records.Current().Position()())
	}
	if t.currentReadScopes {
		t.records.UseCurrentRows()
	}
	t.records.Unlock()
	if t.currentReadScopes {
		t.closeRecordRead()
		t.openRecordRead(context.Background())
	}
}
