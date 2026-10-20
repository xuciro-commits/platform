package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/internal/acceptance"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/journal"
	"platformserver/platform"
)

// acceptedResult is the bounded journal result for one generated create/edit/archive
// or builder publication decision. Its version and size are checked before
// persistence and recovery.
const acceptedResultVersion = 5

type acceptedResult struct {
	Unique         []journal.StateUnique   `json:"unique,omitempty"`
	Definition     *DecisionDefinition     `json:"definition,omitempty"`
	SchemaVersion  *platform.SchemaVersion `json:"schemaVersion,omitempty"`
	Authorizations []authorizationEvidence `json:"authorizations,omitempty"`
	Version        int                     `json:"version"`
	Kind           string                  `json:"kind,omitempty"` // version 4: callback-free lifecycle transition
	Tenant         string                  `json:"tenant"`
	App            string                  `json:"app"`
	At             time.Time               `json:"at,omitempty"` // the original input clock, not a retry's
	RequestHash    string                  `json:"requestHash"`
	Digest         string                  `json:"digest,omitempty"`
	Receipt        json.RawMessage         `json:"receipt"`
	Row            acceptedRow             `json:"row"`
	Event          acceptedEvent           `json:"event"`
}

type acceptedRow struct {
	Type        string              `json:"type"`
	Format      string              `json:"format,omitempty"`
	HistoryRef  *acceptedHistoryRef `json:"historyRef,omitempty"`
	Predecessor string              `json:"predecessor,omitempty"`
	ID          string              `json:"id"`
	Value       json.RawMessage     `json:"value"`
	History     []RecordChange      `json:"history"`
	Projection  *recordProjection   `json:"projection,omitempty"`
	Extraction  *extractionOrigin   `json:"extraction,omitempty"`
}

type acceptedEvent struct {
	App         string            `json:"app"`
	Changed     []string          `json:"changed"`
	Names       []string          `json:"names,omitempty"`
	Subscribers []string          `json:"subscribers,omitempty"`
	Effects     []platform.Effect `json:"effects,omitempty"`
	Observed    bool              `json:"observed,omitempty"`
	Hops        int               `json:"hops,omitempty"`
	Stopped     []string          `json:"stopped,omitempty"`
}

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
	d.records.mu.Lock()
	et := d.records.types[typ]
	if et == nil || et.info.App != app || et.rows.Get(id) == nil {
		d.records.mu.Unlock()
		return nil, fmt.Errorf("decision has no declared record image")
	}
	if et.rows.Get(id).projection != nil {
		d.records.mu.Unlock()
		return nil, fmt.Errorf("versioned migration row requires the complete record batch")
	}
	var prior *row
	if old := d.records.parent.types[typ]; old != nil {
		prior = old.rows.Get(id)
	}
	image, err := acceptedRowChange(typ, id, et.rows.Get(id), prior)
	if err == nil {
		image.Predecessor, err = recordPredecessor(typ, id, prior)
	}
	d.records.mu.Unlock()
	if err != nil {
		return nil, err
	}
	raw, err := protojson.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	hash, err := submissionHash(receipt.GetSubmission(), d.schemaVersion)
	if err != nil {
		return nil, err
	}
	version := acceptedResultVersion
	kind := "record"
	if publisher, ok := d.tenant.app(app).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "publication"
	} else if acceptsPureTransition(d.tenant.app(app), receipt.GetSubmission().GetSchema().GetName()) {
		kind = "pure-transition"
	}
	result := acceptedResult{Version: version, Tenant: receipt.GetSubmission().GetTenantId(),
		Definition: d.definitions[app+"/"+receipt.GetChangeId()],
		Kind:       kind, App: app, At: submittedAt.UTC(), RequestHash: hash, Receipt: raw, SchemaVersion: d.schemaVersion,
		Row:   image,
		Event: d.tenant.planAcceptedEvent(d.events[0]), Authorizations: slices.Clone(d.authorizations)}
	result.Unique = d.savedUniqueChanges()
	result.Digest, err = digestAcceptedResult(result)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := decodeAcceptedResult(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

func decodeAcceptedResult(raw []byte) (acceptedResult, *pb.ChangeRecord, error) {
	var result acceptedResult
	if len(raw) == 0 || len(raw) > acceptance.MaxResultBytes {
		return result, nil, fmt.Errorf("accepted result is empty or exceeds %d bytes", acceptance.MaxResultBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, nil, err
	}
	if result.Row.Projection != nil {
		return result, nil, fmt.Errorf("versioned migration row requires the complete record batch")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, nil, fmt.Errorf("accepted result has trailing data")
	}
	if err := validateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, nil, err
	}

	digest, err := digestAcceptedResult(result)
	if err != nil || result.Digest != digest {
		return result, nil, fmt.Errorf("accepted result content digest differs")
	}
	receipt := new(pb.ChangeRecord)
	if err := protojson.Unmarshal(result.Receipt, receipt); err != nil {
		return result, nil, fmt.Errorf("accepted receipt: %w", err)
	}
	sub := receipt.GetSubmission()
	if result.Version < 1 || result.Version > acceptedResultVersion || result.Tenant == "" || result.App == "" ||
		sub == nil || sub.GetTenantId() != result.Tenant || sub.GetAuthority() != result.App ||
		sub.GetIdempotencyKey() == "" || sub.GetPrincipalId() == "" || receipt.GetChangeId() == "" ||
		receipt.GetRecordedTime() == nil || receipt.GetValidTime() == nil ||
		result.Row.Type == "" || result.Row.ID == "" || result.Row.Type != sub.GetTarget().GetType() ||
		result.Row.ID != sub.GetTarget().GetId() || result.Event.App != result.App ||
		len(result.Event.Changed) != 1 || result.Event.Changed[0] != result.Row.Type+"/"+result.Row.ID ||
		len(result.Row.History) == 0 || len(result.Row.Value) == 0 {
		return result, nil, fmt.Errorf("accepted result has inconsistent identity or effects")
	}
	if result.Version >= 2 {
		if result.At.IsZero() || len(result.Event.Names) == 0 || result.Event.Names[0] != sub.GetSchema().GetName() {
			return result, nil, fmt.Errorf("accepted result has no event intent")
		}
		seen := map[string]bool{}
		for _, id := range result.Event.Subscribers {
			if id == "" || seen[id] {
				return result, nil, fmt.Errorf("accepted result has duplicate or empty subscriber")
			}
			seen[id] = true
		}
		for _, effect := range result.Event.Effects {
			if effect.ID == "" || effect.Endpoint == "" || effect.Event == "" {
				return result, nil, fmt.Errorf("accepted result has incomplete effect intent")
			}
		}
	}
	verb, ok := strings.CutPrefix(sub.GetSchema().GetName(), result.Row.Type+".")
	standard := verb == "create" || verb == "edit" || verb == "archive"
	publication := (result.Version == 3 && result.Kind == "" || result.Version == 5 && result.Kind == "publication") && verb == "publish"
	pureTransition := (result.Version == 4 || result.Version == 5) && !standard && verb != "publish" && result.Kind == "pure-transition"
	legacy := result.Version == 1 && (verb == "create" || verb == "edit")
	if !ok || !(legacy && result.Kind == "" ||
		standard && (result.Version == 2 && result.Kind == "" || result.Version == 5 && result.Kind == "record") || publication || pureTransition) {
		return result, nil, fmt.Errorf("accepted result is outside the supported generated action family")
	}
	if result.Version < 5 && len(result.Unique) > 0 {
		return result, nil, fmt.Errorf("legacy result has unique-key metadata")
	}
	if result.Version == 5 {
		if err := validateHistoryRef(result.Row); err != nil {
			return result, nil, err
		}
	} else if result.Row.HistoryRef != nil || result.Row.Predecessor != "" || result.Row.Format != "" {
		return result, nil, fmt.Errorf("legacy record carries new metadata")
	}
	if err := validateSubmissionSchemaVersion(sub, result.SchemaVersion); err != nil {
		return result, nil, err
	}
	if result.Definition != nil {
		if err := result.Definition.check(result.App, sub.GetSchema().GetName()); err != nil {
			return result, nil, err
		}
	}
	hash, err := submissionHash(sub, result.SchemaVersion)
	if err != nil || result.RequestHash != hash {
		return result, nil, fmt.Errorf("accepted result request digest differs from receipt")
	}
	last := result.Row.History[len(result.Row.History)-1]
	if last.Change != receipt.GetChangeId() || last.Schema != sub.GetSchema().GetName() ||
		last.By != sub.GetPrincipalId() || !last.At.Equal(receipt.GetRecordedTime().AsTime()) {
		return result, nil, fmt.Errorf("record history does not end at accepted receipt")
	}
	return result, receipt, nil
}

// JSONB can reorder object keys; normalize even raw record and receipt JSON
// before computing the corruption-detection digest. This is not an
// authentication tag against an actor who can rewrite the database.
func digestAcceptedResult(result acceptedResult) (string, error) {
	result.Digest = ""
	return acceptance.Digest(result)
}

// applyAcceptedResult reconstructs one record and its kernel receipt without
// calling application decision code. A version-three publication additionally
// installs the committed definition. The tenant lock must be held.
func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	result, receipt, err := decodeAcceptedResult(raw)
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
	decoded, err := rowDecodeFormattedImage(et.info, result.Row.Value, result.Row.Projection, draft.readRelease, result.Row.Format)
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
	rec := recordOf(value)
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
	if prior != nil && reflect.DeepEqual(prior.value.Interface(), value.Interface()) &&
		acceptedRowAlreadyApplied(prior, result.Row) {
		draft.mu.Unlock()
		changed, err := l.ApplyAcceptedChange(receipt)
		if err == nil {
			t.committed.saveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
			t.committed.saveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
		}
		return changed, err
	}
	if result.Version == 5 {
		if err := checkHistoryPredecessor(result.Row, prior); err != nil {
			draft.mu.Unlock()
			return false, err
		}
		before, err := recordPredecessor(result.Row.Type, result.Row.ID, prior)
		if err != nil || before != result.Row.Predecessor || prior != nil && !reflect.DeepEqual(recordOf(prior.value).Created, rec.Created) || prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted record predecessor differs: %v", err)
		}
	} else {
		verb, _ := strings.CutPrefix(receipt.GetSubmission().GetSchema().GetName(), result.Row.Type+".")
		before := len(result.Row.History) - 1
		if verb == "create" && prior != nil || verb != "create" && prior == nil ||
			prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) ||
			prior == nil && before != 0 || prior != nil && (len(prior.recordHistory()) != before ||
			before > 0 && !reflect.DeepEqual(prior.recordHistory(), result.Row.History[:before]) ||
			!reflect.DeepEqual(recordOf(prior.value).Created, rec.Created)) {
			draft.mu.Unlock()
			return false, fmt.Errorf("accepted record is missing its predecessor")
		}
	}
	recovered := &row{value: value, format: result.Row.Format, history: copyHistory(result.Row.History)}
	if result.Version == 5 {
		if err := appendRowHistory(recovered, prior, result.Row.History); err != nil {
			draft.mu.Unlock()
			return false, err
		}
	}
	recovered.retainOriginal(result.Row.Value)
	et.setRow(rec.ID, recovered)
	draft.writes[result.Row.Type+"/"+rec.ID] = true
	draft.remember(result.App+"/"+receipt.GetChangeId(), result.Row.Type+"/"+rec.ID)
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
	t.committed.saveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
	t.committed.saveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
	if changed && publisher != nil {
		if err := publisher.ApplyAcceptedPublication(receipt.GetSubmission().GetSchema().GetName(), decoded); err != nil {
			return false, err
		}
	}
	return changed, nil
}
