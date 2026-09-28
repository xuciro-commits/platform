package platformserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// acceptedResult is the bounded 19a result for one generated create/edit
// decision. It is not yet a journal entry: its version and size are checked
// here before the host is allowed to persist or recover this family.
const acceptedResultVersion = 1
const maxAcceptedResultBytes = 1 << 20

type acceptedResult struct {
	Version     int             `json:"version"`
	Tenant      string          `json:"tenant"`
	App         string          `json:"app"`
	RequestHash string          `json:"requestHash"`
	Digest      string          `json:"digest,omitempty"`
	Receipt     json.RawMessage `json:"receipt"`
	Row         acceptedRow     `json:"row"`
	Event       acceptedEvent   `json:"event"`
}

type acceptedRow struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Value   json.RawMessage `json:"value"`
	History []RecordChange  `json:"history"`
}

type acceptedEvent struct {
	App     string   `json:"app"`
	Changed []string `json:"changed"`
}

func (d *stagedDecision) result(app string, receipt *pb.ChangeRecord) ([]byte, error) {
	if receipt == nil || len(d.events) != 1 || len(d.records.writes) != 1 || len(d.logs) != 1 {
		return nil, fmt.Errorf("one accepted record and event are required")
	}
	accepted := false
	for _, log := range d.logs {
		records := log.Records(receipt.GetSubmission().GetTenantId())
		accepted = len(records) > 0 && proto.Equal(records[len(records)-1], receipt)
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
	if et == nil || et.info.App != app || et.rows[id] == nil {
		d.records.mu.Unlock()
		return nil, fmt.Errorf("decision has no declared record image")
	}
	value, err := json.Marshal(et.rows[id].value.Interface())
	history := copyHistory(et.rows[id].history)
	d.records.mu.Unlock()
	if err != nil {
		return nil, err
	}
	raw, err := protojson.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	request, err := proto.MarshalOptions{Deterministic: true}.Marshal(receipt.GetSubmission())
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(request)
	result := acceptedResult{Version: acceptedResultVersion, Tenant: receipt.GetSubmission().GetTenantId(),
		App: app, RequestHash: hex.EncodeToString(hash[:]), Receipt: raw,
		Row:   acceptedRow{Type: typ, ID: id, Value: value, History: history},
		Event: acceptedEvent{App: app, Changed: []string{ref}}}
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
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return result, nil, fmt.Errorf("accepted result is empty or exceeds %d bytes", maxAcceptedResultBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, nil, fmt.Errorf("accepted result has trailing data")
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
	if result.Version != acceptedResultVersion || result.Tenant == "" || result.App == "" ||
		sub == nil || sub.GetTenantId() != result.Tenant || sub.GetAuthority() != result.App ||
		sub.GetIdempotencyKey() == "" || sub.GetPrincipalId() == "" || receipt.GetChangeId() == "" ||
		receipt.GetRecordedTime() == nil || receipt.GetValidTime() == nil ||
		result.Row.Type == "" || result.Row.ID == "" || result.Row.Type != sub.GetTarget().GetType() ||
		result.Row.ID != sub.GetTarget().GetId() || result.Event.App != result.App ||
		len(result.Event.Changed) != 1 || result.Event.Changed[0] != result.Row.Type+"/"+result.Row.ID ||
		len(result.Row.History) == 0 || len(result.Row.Value) == 0 {
		return result, nil, fmt.Errorf("accepted result has inconsistent identity or effects")
	}
	verb, ok := strings.CutPrefix(sub.GetSchema().GetName(), result.Row.Type+".")
	if !ok || verb != "create" && verb != "edit" {
		return result, nil, fmt.Errorf("accepted result is outside generated create/edit family")
	}
	request, err := proto.MarshalOptions{Deterministic: true}.Marshal(sub)
	if err != nil {
		return result, nil, err
	}
	hash := sha256.Sum256(request)
	if result.RequestHash != hex.EncodeToString(hash[:]) {
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
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

// applyAcceptedResult reconstructs one record and its kernel receipt without
// calling current application decision code. The tenant lock must be held;
// journaling, work intents and a production entry point are not wired yet.
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
	decoder := json.NewDecoder(bytes.NewReader(result.Row.Value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Addr().Interface()); err != nil {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record image: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record has trailing data")
	}
	rec := recordOf(value)
	if rec.ID != result.Row.ID || rec.Revision != receipt.GetRevision() ||
		rec.Changed.By != receipt.GetSubmission().GetPrincipalId() ||
		rec.Changed.Change != receipt.GetChangeId() ||
		!rec.Changed.At.Equal(receipt.GetRecordedTime().AsTime()) {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record image differs from receipt")
	}
	prior := et.rows[rec.ID]
	if prior != nil && reflect.DeepEqual(prior.value.Interface(), value.Interface()) &&
		reflect.DeepEqual(prior.history, result.Row.History) {
		draft.mu.Unlock()
		return l.ApplyAcceptedChange(receipt)
	}
	verb, _ := strings.CutPrefix(receipt.GetSubmission().GetSchema().GetName(), result.Row.Type+".")
	before := len(result.Row.History) - 1
	if verb == "create" && prior != nil || verb == "edit" && prior == nil ||
		prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) ||
		prior == nil && before != 0 || prior != nil && (len(prior.history) != before ||
		!reflect.DeepEqual(prior.history, result.Row.History[:before]) ||
		!reflect.DeepEqual(recordOf(prior.value).Created, rec.Created)) {
		draft.mu.Unlock()
		return false, fmt.Errorf("accepted record is missing its predecessor")
	}
	et.rows[rec.ID] = &row{value: value, history: copyHistory(result.Row.History)}
	draft.writes[result.Row.Type+"/"+rec.ID] = true
	draft.remember(result.App+"/"+receipt.GetChangeId(), result.Row.Type+"/"+rec.ID)
	if et.knowledge {
		draft.dirty[result.Row.Type+"/"+rec.ID] = true
	}
	draft.mu.Unlock()
	// An invalid receipt cannot expose the staged row. With the tenant lock
	// held, promote cannot race another supported write after this check.
	changed, err := l.ApplyAcceptedChange(receipt)
	if err != nil {
		return false, err
	}
	if err := s.promoteRecords(draft); err != nil {
		return false, err
	}
	return changed, nil
}
