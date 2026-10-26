package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/internal/construction"

	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/platform"
)

// sealResult preserves the complete decision. The compact record format is
// only an encoding optimisation; its writer must account for every effect.
// The batch format also handles history that no longer fits a compact result.
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
	if receipt == nil || len(d.events) != 1 || len(d.records.writes) != 1 || len(d.logs) != 1 {
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
	if !d.records.writes[ref] || d.events[0].App != app || !proto.Equal(d.events[0].Record, receipt) ||
		len(d.events[0].Changed) != 1 || d.events[0].Changed[0] != ref {
		return nil, fmt.Errorf("decision has other record or event effects")
	}
	image, err := func() (records.Image, error) {
		d.records.mu.Lock()
		defer d.records.mu.Unlock()
		et := d.records.types[typ]
		if et == nil || et.info.App != app || et.rows.Get(id) == nil {
			return records.Image{}, fmt.Errorf("decision has no declared record image")
		}
		if et.rows.Get(id).Projection != nil {
			return records.Image{}, fmt.Errorf("versioned migration row requires the complete record batch")
		}
		var prior *records.Row
		if old := d.records.parent.types[typ]; old != nil {
			prior = old.rows.Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.rows.Get(id), prior)
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
	hash, err := resultformat.SubmissionHash(receipt.GetSubmission(), d.schemaVersion)
	if err != nil {
		return nil, err
	}
	version := resultformat.RecordVersion
	kind := "record"
	if publisher, ok := d.tenant.app(app).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "publication"
	} else if construction.CallbackFreeTransition(d.tenant.app(app).Manifest(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "pure-transition"
	}
	plan, refusal := d.tenant.planAcceptedEvent(d.events[0])
	if refusal != nil {
		return nil, refusal
	}
	result := resultformat.Record{Version: version, Tenant: receipt.GetSubmission().GetTenantId(),
		Definition: d.definitions[app+"/"+receipt.GetChangeId()],
		Kind:       kind, App: app, At: submittedAt.UTC(), RequestHash: hash, Receipt: raw, SchemaVersion: d.schemaVersion,
		Row:   image,
		Event: plan, Authorizations: slices.Clone(d.authorizations)}
	result.Unique = d.savedUniqueChanges()
	result.Digest, err = resultformat.DigestRecord(result)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := resultformat.DecodeRecord(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

// JSONB can reorder object keys; normalize even raw record and receipt JSON
// before computing the corruption-detection digest. This is not an
// authentication tag against an actor who can rewrite the database.

// applyAcceptedResult reconstructs one record and its kernel receipt without
// calling application decision code. A version-three publication additionally
// installs the committed definition. The tenant lock must be held.
func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	result, receipt, err := resultformat.DecodeRecord(raw)
	if err != nil {
		return false, err
	}
	if result.Tenant != t.ID || l == nil || !slices.ContainsFunc(l.Declarations(), func(d *pb.AuthorityDeclaration) bool {
		return d.GetTenantId() == result.Tenant && d.GetAuthorityId() == result.App && d.GetDataClass() == result.Row.Type
	}) {
		return false, fmt.Errorf("accepted result belongs to another tenant or authority")
	}
	s := t.records
	draft := s.forkRecords()
	draft.mu.Lock()
	et := draft.types[result.Row.Type]
	if et == nil || et.info.App != result.App {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted result needs installed entity %s", result.Row.Type)
	}
	value := reflect.New(et.info.Go).Elem()
	decoded, err := records.DecodeFormattedImage(et.info, result.Row.Value, result.Row.Projection, draft.readRelease, result.Row.Format)
	if err != nil {
		draft.mu.Unlock()
		return false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Addr().Interface()); err != nil {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record image: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record has trailing data")
	}
	if err := checkRunApplicationBindings(value.Interface()); err != nil {
		draft.mu.Unlock()
		return false, err
	}
	rec := records.Header(value)
	if rec.ID != result.Row.ID || rec.Revision != receipt.GetRevision() ||
		rec.Changed.By != receipt.GetSubmission().GetPrincipalId() ||
		rec.Changed.Change != receipt.GetChangeId() ||
		!rec.Changed.At.Equal(receipt.GetRecordedTime().AsTime()) {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record image differs from receipt")
	}
	var publisher platform.AcceptedPublisher
	if result.Version == 3 || result.Version == 5 && result.Kind == "publication" {
		schema := receipt.GetSubmission().GetSchema().GetName()
		var ok bool
		publisher, ok = t.app(result.App).(platform.AcceptedPublisher)
		if !ok || !slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted publication has no installer")
		}
		if err := publisher.ValidateAcceptedPublication(schema, decoded); err != nil {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted publication is invalid: %w", err)
		}
	}
	prior := et.rows.Get(rec.ID)
	if prior != nil && reflect.DeepEqual(prior.Value.Interface(), value.Interface()) &&
		records.ImageAlreadyApplied(prior, result.Row) {
		draft.mu.Unlock()
		changed, err := l.ApplyAcceptedChange(receipt)
		if err == nil {
			t.committed.saveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
			t.committed.saveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
		}
		return changed, err
	}
	if result.Version == 5 {
		if err := records.CheckHistoryPredecessor(result.Row, prior); err != nil {
			draft.mu.Unlock()
			return false, err
		}
		before, err := records.Predecessor(result.Row.Type, result.Row.ID, prior)
		if err != nil || before != result.Row.Predecessor || prior != nil && !reflect.DeepEqual(records.Header(prior.Value).Created, rec.Created) || prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) {
			draft.mu.Unlock()
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
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted record is missing its predecessor")
		}
	}
	recovered := &records.Row{Value: value, Format: result.Row.Format, History: records.CopyHistory(result.Row.History)}
	if result.Version == 5 {
		if err := records.AppendRowHistory(recovered, prior, result.Row.History); err != nil {
			draft.mu.Unlock()
			return false, err
		}
	}
	recovered.RetainOriginal(result.Row.Value)
	et.setRow(rec.ID, recovered)
	draft.writes[result.Row.Type+"/"+rec.ID] = true
	draft.recentChanges.Remember(result.App+"/"+receipt.GetChangeId(), result.Row.Type+"/"+rec.ID)
	if et.knowledge {
		draft.dirty.Set(result.Row.Type+"/"+rec.ID, true)
	}
	draft.mu.Unlock()
	if result.Version == 5 {
		if err := validateUniqueReplacements(draft, result.Unique); err != nil {
			return false, err
		}
	}
	// An invalid receipt cannot expose the staged row. With the tenant lock
	// held, promote cannot race another supported write after this check.
	changed, err := l.ApplyAcceptedChange(receipt)
	if err != nil {
		return false, err
	}
	if err := s.promoteRecords(draft); err != nil {
		return false, err
	}
	if result.Row.Type == construction.AppType {
		t.defGen.Add(1)
	}
	t.committed.saveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
	t.committed.saveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
	if changed && publisher != nil {
		if err := publisher.ApplyAcceptedPublication(receipt.GetSubmission().GetSchema().GetName(), decoded); err != nil {
			return false, err
		}
	}
	return changed, nil
}
