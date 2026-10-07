package platformserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// One top-level input, not one row or one nested decision. Before validates
// the predecessor image/history before any authoritative ledger is advanced.
type acceptedBatch struct {
	Version       int                             `json:"version"`
	Kind          string                          `json:"kind"`
	Tenant        string                          `json:"tenant"`
	App           string                          `json:"app"`
	At            time.Time                       `json:"at"`
	RequestHash   string                          `json:"requestHash"`
	Digest        string                          `json:"digest"`
	Receipt       json.RawMessage                 `json:"receipt"`              // top-level answer/key
	Submission    json.RawMessage                 `json:"submission,omitempty"` // original input, when answer is an approval receipt
	Rows          []acceptedBatchRow              `json:"rows"`
	Decisions     []acceptedDecision              `json:"decisions"`
	Sequences     map[string]int                  `json:"sequences,omitempty"`
	SequenceBases map[string]int                  `json:"sequenceBases,omitempty"`
	Notices       *acceptedNotices                `json:"notices,omitempty"`
	Intents       []platform.Effect               `json:"intents,omitempty"`
	Observations  []acceptedObservation           `json:"observations,omitempty"`
	States        []acceptedState                 `json:"states,omitempty"`
	Deliveries    []acceptedConnectorDelivery     `json:"deliveries,omitempty"`
	Operations    []acceptedOperationCancellation `json:"operations,omitempty"`
}

type acceptedBatchRow struct {
	acceptedRow
	App         string   `json:"app"`
	Before      string   `json:"before,omitempty"`
	Authorities []string `json:"authorities,omitempty"` // receipt authority for each new history entry
}

type acceptedDecision struct {
	Receipt     json.RawMessage      `json:"receipt"`
	Event       acceptedEvent        `json:"event"`
	Publication *acceptedPublication `json:"publication,omitempty"`
}

type acceptedPublication struct {
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

func canonicalDigest(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func acceptedRowOf(typ, id string, r *row) (acceptedRow, error) {
	value, err := r.image()
	return acceptedRow{Type: typ, ID: id, Value: value, History: copyHistory(r.history)}, err
}

func (d *stagedDecision) batchResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	if receipt == nil || len(d.events) == 0 || d.failure != nil {
		return nil, fmt.Errorf("batch needs accepted decisions and all written records")
	}
	if err := d.records.validateLinkConstraints(); err != nil {
		return nil, err
	}
	result := acceptedBatch{Version: 1, Kind: "record-batch", Tenant: d.tenant.ID,
		App: app, At: at.UTC(), Sequences: maps.Clone(d.allocated), SequenceBases: maps.Clone(d.sequenceBases),
		Notices: d.savedNotices(), Intents: slices.Clone(d.intents), Observations: slices.Clone(d.observations),
		Deliveries: slices.Clone(d.deliveries)}
	var err error
	result.Operations, err = d.savedOperationCancellations()
	if err != nil {
		return nil, err
	}
	result.States, err = d.savedStates()
	if err != nil {
		return nil, err
	}
	result.Receipt, err = protojson.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	if !proto.Equal(request, receipt.GetSubmission()) {
		result.Submission, err = protojson.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	result.RequestHash, err = submissionHash(request)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, event := range d.events {
		owner, ok := d.tenant.app(event.App).(platform.ResultApp)
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
			return nil, err
		}
		plan := d.tenant.planAcceptedEvent(event)
		plan.Observed = d.observed
		plan.Hops = d.hops
		if plan.Hops > maxHops {
			plan.Stopped, plan.Subscribers = plan.Subscribers, nil
		}
		result.Decisions = append(result.Decisions, acceptedDecision{Receipt: raw, Event: plan,
			Publication: d.publications[event.App+"/"+event.Record.GetChangeId()]})
		for _, ref := range event.Changed {
			if !d.records.writes[ref] {
				return nil, fmt.Errorf("batch event names an unwritten record")
			}
			seen[ref] = true
		}
	}
	if len(seen) != len(d.records.writes) {
		return nil, fmt.Errorf("batch event omits a written record")
	}
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	d.records.parent.mu.Lock()
	defer d.records.parent.mu.Unlock()
	for _, ref := range slices.Sorted(maps.Keys(d.records.writes)) {
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if !ok || et == nil || et.rows[id] == nil {
			return nil, fmt.Errorf("batch includes an undeclared record")
		}
		image, err := acceptedRowOf(typ, id, et.rows[id])
		if err != nil {
			return nil, err
		}
		row := acceptedBatchRow{acceptedRow: image, App: et.info.App, Authorities: slices.Clone(d.writers[ref])}
		if priorType := d.records.parent.types[typ]; priorType != nil && priorType.rows[id] != nil {
			before, err := acceptedRowOf(typ, id, priorType.rows[id])
			if err != nil {
				return nil, err
			}
			row.Before, err = canonicalDigest(before)
			if err != nil {
				return nil, err
			}
		}
		result.Rows = append(result.Rows, row)
	}
	result.Digest, err = digestAcceptedBatch(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := decodeAcceptedBatch(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeAcceptedBatch(raw []byte) (acceptedBatch, *pb.ChangeRecord, error) {
	var result acceptedBatch
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return result, nil, fmt.Errorf("record batch is empty or exceeds %d bytes", maxAcceptedResultBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, nil, fmt.Errorf("record batch has trailing data")
	}
	digest, err := digestAcceptedBatch(result)
	if err != nil || result.Digest != digest {
		return result, nil, fmt.Errorf("record batch content digest differs")
	}
	receipt := new(pb.ChangeRecord)
	if err := protojson.Unmarshal(result.Receipt, receipt); err != nil {
		return result, nil, fmt.Errorf("record batch receipt: %w", err)
	}
	sub, err := batchSubmission(result, receipt)
	if err != nil {
		return result, nil, err
	}
	if result.Version != 1 || result.Kind != "record-batch" || result.Tenant == "" || result.App == "" ||
		result.At.IsZero() || sub == nil || sub.GetTenantId() != result.Tenant ||
		sub.GetAuthority() != result.App || sub.GetPrincipalId() == "" || sub.GetIdempotencyKey() == "" ||
		sub.GetSchema() == nil || sub.GetTarget() == nil || receipt.GetChangeId() == "" ||
		receipt.GetRecordedTime() == nil || receipt.GetValidTime() == nil ||
		len(result.Decisions) == 0 {
		return result, nil, fmt.Errorf("record batch has inconsistent identity or effects")
	}
	hash, err := submissionHash(sub)
	if err != nil || result.RequestHash != hash {
		return result, nil, fmt.Errorf("record batch request digest differs")
	}
	changes := map[string]*pb.ChangeRecord{}
	written := map[string]bool{}
	top := false
	for _, decision := range result.Decisions {
		r := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(decision.Receipt, r); err != nil {
			return result, nil, fmt.Errorf("record batch child receipt: %w", err)
		}
		s, e := r.GetSubmission(), decision.Event
		key := e.App + "/" + r.GetChangeId()
		if s == nil || s.GetTenantId() != result.Tenant || s.GetAuthority() != e.App ||
			s.GetIdempotencyKey() == "" || s.GetPrincipalId() == "" || s.GetTarget() == nil || s.GetSchema() == nil ||
			r.GetChangeId() == "" || r.GetRecordedTime() == nil || r.GetValidTime() == nil ||
			e.App == "" || len(e.Names) == 0 || e.Names[0] != s.GetSchema().GetName() || changes[key] != nil {
			return result, nil, fmt.Errorf("record batch has an inconsistent child decision")
		}
		if e.Hops < 0 || e.Hops > maxHops+1 || e.Hops > maxHops && len(e.Subscribers) > 0 ||
			e.Hops <= maxHops && len(e.Stopped) > 0 {
			return result, nil, fmt.Errorf("record batch has an invalid delivery hop boundary")
		}
		changes[key] = r
		top = top || proto.Equal(r, receipt)
		changed := map[string]bool{}
		for _, ref := range e.Changed {
			if ref == "" || changed[ref] {
				return result, nil, fmt.Errorf("record batch has duplicate or foreign changed records")
			}
			changed[ref], written[ref] = true, true
		}
		subscribers := map[string]bool{}
		for _, id := range e.Subscribers {
			if id == "" || subscribers[id] {
				return result, nil, fmt.Errorf("record batch has duplicate or empty subscriber")
			}
			subscribers[id] = true
		}
		for _, effect := range e.Effects {
			if effect.ID == "" || effect.Endpoint == "" || effect.Event == "" {
				return result, nil, fmt.Errorf("record batch has incomplete effect intent")
			}
		}
	}
	if !top || len(written) != len(result.Rows) {
		return result, nil, fmt.Errorf("record batch omits the top-level receipt or a written row")
	}
	seen := map[string]bool{}
	for _, row := range result.Rows {
		ref := row.Type + "/" + row.ID
		if row.Type == "" || row.ID == "" || row.App == "" || len(row.Value) == 0 ||
			len(row.History) == 0 || seen[ref] || !written[ref] {
			return result, nil, fmt.Errorf("record batch omits or duplicates a row")
		}
		seen[ref] = true
		last := row.History[len(row.History)-1]
		lastApp := row.App
		if len(row.Authorities) > 0 {
			lastApp = row.Authorities[len(row.Authorities)-1]
		}
		r := changes[lastApp+"/"+last.Change]
		if r == nil || last.Schema != r.GetSubmission().GetSchema().GetName() ||
			last.By != r.GetSubmission().GetPrincipalId() || !last.At.Equal(r.GetRecordedTime().AsTime()) {
			return result, nil, fmt.Errorf("record batch row history differs from its decision")
		}
	}
	for key, n := range result.Sequences {
		app, _, ok := strings.Cut(key, "/")
		base, exists := result.SequenceBases[key]
		if !ok || !exists || base < 0 || n <= base ||
			!slices.ContainsFunc(result.Decisions, func(d acceptedDecision) bool { return d.Event.App == app }) {
			return result, nil, fmt.Errorf("record batch has invalid sequence allocation")
		}
	}
	if len(result.SequenceBases) != len(result.Sequences) {
		return result, nil, fmt.Errorf("record batch has unmatched sequence predecessors")
	}
	return result, receipt, nil
}

func batchSubmission(result acceptedBatch, receipt *pb.ChangeRecord) (*pb.Submission, error) {
	if len(result.Submission) == 0 {
		return receipt.GetSubmission(), nil
	}
	s := new(pb.Submission)
	if err := protojson.Unmarshal(result.Submission, s); err != nil {
		return nil, fmt.Errorf("record batch original submission: %w", err)
	}
	if receipt.GetSubmission().GetAuthority() != "work" ||
		receipt.GetSubmission().GetSchema().GetName() != "work.approval.request" {
		return nil, fmt.Errorf("record batch answer differs without an approval request")
	}
	answer := receipt.GetSubmission()
	var payload struct {
		Requester  string          `json:"requester"`
		Submission json.RawMessage `json:"submission"`
	}
	held := new(pb.Submission)
	if json.Unmarshal(answer.GetPayload(), &payload) != nil || protojson.Unmarshal(payload.Submission, held) != nil ||
		!proto.Equal(held, s) || payload.Requester != s.GetPrincipalId() || answer.GetPrincipalId() != "app:work" ||
		answer.GetTarget().GetType() != "work.approval" ||
		answer.GetTarget().GetId() != s.GetAuthority()+"."+s.GetIdempotencyKey() ||
		answer.GetIdempotencyKey() != "approval:"+answer.GetTarget().GetId() {
		return nil, fmt.Errorf("record batch approval does not hold its original submission")
	}
	return s, nil
}

func digestAcceptedBatch(result acceptedBatch) (string, error) {
	result.Digest = ""
	return canonicalDigest(result)
}

// Validate every row, counter and ledger on private forks before promotion.
// A late invalid child cannot advance an earlier authoritative ledger.
func (t *Tenant) applyAcceptedBatch(l *platform.Ledger, raw []byte) (bool, error) {
	result, receipt, err := decodeAcceptedBatch(raw)
	if err != nil {
		return false, err
	}
	owner, ok := t.app(result.App).(platform.ResultApp)
	if result.Tenant != t.ID || !ok || owner.AcceptedLedger() != l {
		return false, fmt.Errorf("record batch belongs to another tenant or ledger")
	}
	sub, _ := batchSubmission(result, receipt)
	key := result.App + "/" + sub.GetIdempotencyKey()
	if saved := t.committed.answers[key]; len(saved) > 0 {
		prior, answer, err := decodeAcceptedBatch(saved)
		if err != nil || prior.RequestHash != result.RequestHash || !proto.Equal(answer, receipt) {
			return false, fmt.Errorf("record batch key belongs to another answer")
		}
		return false, nil
	}
	answerOwner, ok := t.app(receipt.GetSubmission().GetAuthority()).(platform.ResultApp)
	if !ok {
		return false, fmt.Errorf("record batch answer has no authority")
	}
	if prior := answerOwner.AcceptedLedger().AcceptedFor(t.ID, receipt.GetSubmission().GetIdempotencyKey()); prior != nil {
		if !proto.Equal(prior, receipt) {
			return false, fmt.Errorf("record batch key belongs to another result")
		}
		return false, nil // later commits may already have changed rows/counters
	}
	ledgers := map[string]*platform.Ledger{}
	changes := map[string]*pb.ChangeRecord{}
	for _, decision := range result.Decisions {
		ra, ok := t.app(decision.Event.App).(platform.ResultApp)
		if !ok {
			return false, fmt.Errorf("record batch needs missing authority %s", decision.Event.App)
		}
		ledgers[decision.Event.App] = ra.AcceptedLedger()
		for _, subscriber := range decision.Event.Subscribers {
			if t.app(subscriber) == nil {
				return false, fmt.Errorf("record batch needs missing subscriber %s", subscriber)
			}
		}
		r := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(decision.Receipt, r); err != nil {
			return false, err
		}
		if _, refused := t.committed.refusals[decision.Event.App+"/"+r.GetSubmission().GetIdempotencyKey()]; refused {
			return false, fmt.Errorf("record batch reused a refused child key")
		}
		changes[decision.Event.App+"/"+r.GetChangeId()] = r
	}
	draft := t.records.forkRecords()
	if err := t.validateAcceptedNotices(result.Notices); err != nil {
		return false, err
	}
	if err := t.validateAcceptedIntents(result.Intents); err != nil {
		return false, err
	}
	if err := t.validateAcceptedObservations(result.Observations); err != nil {
		return false, err
	}
	if err := t.validateAcceptedStates(result.States); err != nil {
		return false, err
	}
	if err := t.validateOperationCancellations(result.Operations); err != nil {
		return false, err
	}
	if err := t.validateAcceptedDeliveries(result.Deliveries); err != nil {
		return false, err
	}
	for _, decision := range result.Decisions {
		if decision.Publication == nil {
			continue
		}
		publisher, ok := t.app(decision.Event.App).(platform.AcceptedPublisher)
		if !ok || !slices.Contains(publisher.AcceptedPublicationSchemas(), decision.Publication.Schema) ||
			decision.Publication.Schema != decision.Event.Names[0] {
			return false, fmt.Errorf("record batch has an invalid publication")
		}
		if err := publisher.ValidateAcceptedPublication(decision.Publication.Schema, decision.Publication.Image); err != nil {
			return false, err
		}
	}
	if err := t.applyBatchRows(draft, result, ledgers, changes); err != nil {
		return false, err
	}
	for _, decision := range result.Decisions {
		if p := decision.Publication; p != nil {
			if err := t.installPublicationLinkConstraint(draft, p.Schema, p.Image); err != nil {
				return false, err
			}
		}
	}
	if err := draft.validateLinkConstraints(); err != nil {
		return false, err
	}
	if err := t.sequences.check(result.SequenceBases, result.Sequences); err != nil {
		return false, err
	}
	checks := map[string]*kernel.ChangeLog{}
	for app, ledger := range ledgers {
		checks[app] = ledger.ForkAcceptedChanges()
	}
	for _, decision := range result.Decisions {
		r := changes[decision.Event.App+"/"+receiptID(decision.Receipt)]
		if changed, err := checks[decision.Event.App].ApplyAccepted(r); err != nil || !changed {
			return false, fmt.Errorf("record batch ledger validation: %v (new=%t)", err, changed)
		}
	}
	for _, decision := range result.Decisions {
		r := changes[decision.Event.App+"/"+receiptID(decision.Receipt)]
		if changed, err := ledgers[decision.Event.App].ApplyAcceptedChange(r); err != nil || !changed {
			return false, fmt.Errorf("record batch ledger application: %v (new=%t)", err, changed)
		}
	}
	if err := t.records.promoteRecords(draft); err != nil {
		return false, err
	}
	if err := t.applyAcceptedStates(result.States); err != nil {
		return false, err
	}
	if err := t.applyOperationCancellations(result.Operations); err != nil {
		return false, err
	}
	if err := t.applyAcceptedDeliveries(result.Deliveries); err != nil {
		return false, err
	}
	for _, decision := range result.Decisions {
		if p := decision.Publication; p != nil {
			if err := t.app(decision.Event.App).(platform.AcceptedPublisher).ApplyAcceptedPublication(p.Schema, p.Image); err != nil {
				return false, err
			}
		}
	}
	t.applyAcceptedNotices(result.Notices)
	t.installAcceptedIntents(result.Intents)
	if err := t.applyAcceptedObservations(result.Observations); err != nil {
		return false, err
	}
	t.sequences.set(result.Sequences)
	if len(result.Submission) > 0 {
		t.committed.saveAnswer(key, raw)
	}
	return true, nil
}

// applyBatchRows writes the batch's row images into the forked records, each
// under a ledger that declares its type, keeping the history it extends.
func (t *Tenant) applyBatchRows(draft *recordStore, result acceptedBatch, ledgers map[string]*platform.Ledger, changes map[string]*pb.ChangeRecord) error {
	draft.mu.Lock()
	defer draft.mu.Unlock()
	for _, image := range result.Rows {
		ledger := ledgers[image.App]
		if ledger == nil {
			if owner, ok := t.app(image.App).(platform.ResultApp); ok {
				ledger = owner.AcceptedLedger()
			}
		}
		if ledger == nil || !slices.ContainsFunc(ledger.Declarations(), func(d *pb.AuthorityDeclaration) bool {
			return d.GetTenantId() == t.ID && d.GetAuthorityId() == image.App && d.GetDataClass() == image.Type
		}) {
			draft.mu.Unlock()
			return fmt.Errorf("record batch has no authority for %s", image.Type)
		}
		et := draft.types[image.Type]
		if et == nil || et.info.App != image.App {
			draft.mu.Unlock()
			return fmt.Errorf("record batch needs installed entity %s", image.Type)
		}
		prior := et.rows[image.ID]
		if restored := legacyAgentContextPredecessor(prior, image); restored != nil {
			prior = restored
		}
		before := ""
		baseHistory, revision := 0, uint32(0)
		if prior != nil {
			row, err := acceptedRowOf(image.Type, image.ID, prior)
			if err != nil {
				draft.mu.Unlock()
				return err
			}
			before, err = canonicalDigest(row)
			if err != nil {
				draft.mu.Unlock()
				return err
			}
			baseHistory, revision = len(prior.history), recordOf(prior.value).Revision
		}
		prefixEqual := true
		if prior != nil && baseHistory > 0 && len(image.History) >= baseHistory {
			oldDigest, oldErr := canonicalDigest(prior.history)
			savedDigest, savedErr := canonicalDigest(image.History[:baseHistory])
			prefixEqual = oldErr == nil && savedErr == nil && oldDigest == savedDigest
		}
		// PostgreSQL JSONB normalizes JSON field-change bytes (object key
		// order/whitespace). The canonical predecessor hash is authoritative;
		// bytewise DeepEqual would quarantine a sound recovered result.
		if before != image.Before || len(image.History) <= baseHistory || !prefixEqual {
			draft.mu.Unlock()
			return fmt.Errorf("record batch predecessor differs for %s/%s (value=%t history=%d/%d prefix=%t)",
				image.Type, image.ID, before == image.Before, baseHistory, len(image.History), prefixEqual)
		}
		value := reflect.New(et.info.Go).Elem()
		decoder := json.NewDecoder(bytes.NewReader(image.Value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(value.Addr().Interface()); err != nil {
			draft.mu.Unlock()
			return fmt.Errorf("record batch image: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			draft.mu.Unlock()
			return fmt.Errorf("record batch image has trailing data")
		}
		rec := recordOf(value)
		var last *pb.ChangeRecord
		if len(image.Authorities) > 0 && len(image.Authorities) != len(image.History)-baseHistory {
			draft.mu.Unlock()
			return fmt.Errorf("record batch history authorities differ")
		}
		for index, change := range image.History[baseHistory:] {
			app := image.App
			if len(image.Authorities) > 0 {
				app = image.Authorities[index]
			}
			r := changes[app+"/"+change.Change]
			if r == nil || change.Schema != r.GetSubmission().GetSchema().GetName() ||
				change.By != r.GetSubmission().GetPrincipalId() || !change.At.Equal(r.GetRecordedTime().AsTime()) {
				draft.mu.Unlock()
				return fmt.Errorf("record batch has a foreign history suffix")
			}
			if r.GetSubmission().GetTarget().GetType() == image.Type &&
				r.GetSubmission().GetTarget().GetId() == image.ID {
				revision = r.GetRevision()
			}
			last = r
		}
		if rec.ID != image.ID || rec.Revision != revision || last == nil ||
			rec.Changed.Change != last.GetChangeId() || rec.Changed.By != last.GetSubmission().GetPrincipalId() ||
			!rec.Changed.At.Equal(last.GetRecordedTime().AsTime()) ||
			prior != nil && !reflect.DeepEqual(rec.Created, recordOf(prior.value).Created) ||
			prior == nil && (rec.Created.Change != image.History[0].Change ||
				rec.Created.By != image.History[0].By || !rec.Created.At.Equal(image.History[0].At)) {
			draft.mu.Unlock()
			return fmt.Errorf("record batch image differs from its decisions")
		}
		recovered := &row{value: value, history: copyHistory(image.History)}
		recovered.retainOriginal(image.Value)
		et.rows[rec.ID] = recovered
		draft.writes[image.Type+"/"+rec.ID] = true
		for _, change := range image.History[baseHistory:] {
			draft.remember(image.App+"/"+change.Change, image.Type+"/"+rec.ID)
		}
		if et.knowledge {
			draft.dirty[image.Type+"/"+rec.ID] = true
		}
	}
	return nil
}

func receiptID(raw json.RawMessage) string {
	r := new(pb.ChangeRecord)
	_ = protojson.Unmarshal(raw, r)
	return r.GetChangeId()
}

func (t *Tenant) publishAcceptedBatch(result acceptedBatch) {
	for _, decision := range result.Decisions {
		r := new(pb.ChangeRecord)
		_ = protojson.Unmarshal(decision.Receipt, r) // already checked
		t.publishAccepted(platform.Event{App: decision.Event.App, Record: r, Changed: decision.Event.Changed},
			decision.Event, 2)
	}
}
