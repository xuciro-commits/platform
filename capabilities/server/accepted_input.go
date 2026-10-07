package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// A recorded connector input is an independent top-level result, not a
// submission manufactured to make its application callback fit a ledger.
// Its answer, record images and K8 marks are saved together. An application
// with additional mutable state must supply an AcceptedStateApp fork.
type acceptedInput struct {
	Version     int                         `json:"version"`
	Kind        string                      `json:"kind"`
	Tenant      string                      `json:"tenant"`
	App         string                      `json:"app"`
	Name        string                      `json:"name"`
	Member      string                      `json:"member"`
	Key         string                      `json:"key"`
	At          time.Time                   `json:"at"`
	Body        []byte                      `json:"body"`
	RequestHash string                      `json:"requestHash"`
	Digest      string                      `json:"digest"`
	Answer      json.RawMessage             `json:"answer,omitempty"`
	Refusal     *kernel.Error               `json:"refusal,omitempty"`
	Changes     json.RawMessage             `json:"changes,omitempty"`
	Rows        []acceptedInputRow          `json:"rows,omitempty"`
	Deliveries  []acceptedConnectorDelivery `json:"deliveries,omitempty"`
	States      []acceptedState             `json:"states,omitempty"`
	Notices     *acceptedNotices            `json:"notices,omitempty"`
}

type acceptedInputRow struct {
	acceptedRow
	App    string `json:"app"`
	Before string `json:"before,omitempty"`
}

func acceptedInputHash(tenant, app, name, member string, body []byte) (string, error) {
	return canonicalDigest(struct {
		Tenant, App, Name, Member string
		Body                      []byte
	}{tenant, app, name, member, body})
}

func (d *stagedDecision) inputResult(app, name string, m platform.Member, body []byte, answer any, at time.Time) ([]byte, error) {
	if d.failure != nil || len(d.requests) > 0 {
		return nil, fmt.Errorf("connector input contains effects not represented by its result")
	}
	hash, err := acceptedInputHash(d.tenant.ID, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := acceptedInput{Version: 1, Kind: "input-result", Tenant: d.tenant.ID, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Deliveries: slices.Clone(d.deliveries), Notices: d.savedNotices()}
	if message, ok := answer.(proto.Message); ok {
		result.Answer, err = protojson.Marshal(message)
	} else {
		result.Answer, err = json.Marshal(answer)
	}
	if err != nil {
		return nil, err
	}
	if len(d.events) > 0 {
		receipt, ok := answer.(*pb.ChangeRecord)
		if !ok || receipt.GetSubmission().GetAuthority() != app {
			return nil, fmt.Errorf("connector input has decisions without a matching answer")
		}
		result.Changes, err = d.batchResult(app, receipt.GetSubmission(), receipt, at)
		if err != nil {
			return nil, err
		}
		// The child batch owns the entire staged state; do not apply its marks,
		// notices or record images twice from the enclosing input.
		result.Deliveries, result.Notices = nil, nil
		result.Digest, err = digestAcceptedInput(result)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if _, err := decodeAcceptedInput(raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if len(d.intents) > 0 || len(d.observations) > 0 || len(d.allocated) > 0 || len(d.logs) > 0 {
		return nil, fmt.Errorf("connector input contains effects without a decision")
	}
	result.States, err = d.savedStates()
	if err != nil {
		return nil, err
	}
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	d.records.parent.mu.Lock()
	defer d.records.parent.mu.Unlock()
	for _, ref := range slices.Sorted(maps.Keys(d.records.writes)) {
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if !ok || et == nil || et.info.App != app || et.rows[id] == nil {
			return nil, fmt.Errorf("connector input writes a foreign or undeclared record")
		}
		row, err := acceptedRowOf(typ, id, et.rows[id])
		if err != nil {
			return nil, err
		}
		saved := acceptedInputRow{acceptedRow: row, App: app}
		if prior := d.records.parent.types[typ]; prior != nil && prior.rows[id] != nil {
			before, err := acceptedRowOf(typ, id, prior.rows[id])
			if err != nil {
				return nil, err
			}
			saved.Before, err = canonicalDigest(before)
			if err != nil {
				return nil, err
			}
		}
		result.Rows = append(result.Rows, saved)
	}
	result.Digest, err = digestAcceptedInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := decodeAcceptedInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func digestAcceptedInput(result acceptedInput) (string, error) {
	result.Digest = ""
	return canonicalDigest(result)
}

func encodeAcceptedInputRefusal(tenant, app, name string, m platform.Member, body []byte, at time.Time, refusal *kernel.Error) ([]byte, error) {
	hash, err := acceptedInputHash(tenant, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := acceptedInput{Version: 1, Kind: "input-result", Tenant: tenant, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Refusal: refusal}
	result.Digest, err = digestAcceptedInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := decodeAcceptedInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (t *Tenant) inputAccepted(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time) (any, *kernel.Error) {
	if m.ID == "" || m.Tenant != t.ID || name == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connector input has an invalid identity")
	}
	hash, hashErr := acceptedInputHash(t.ID, a.Manifest().ID, name, m.ID, body)
	if hashErr != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	key := "input:" + hash
	if saved := t.committed.inputs[a.Manifest().ID+"/"+key]; len(saved) != 0 {
		return t.answerAcceptedInput(a, saved)
	}
	draft := t.newStagedDecision()
	answer, refusal, unsupported := decideAcceptedInput(a, draft, m, name, body, now)
	if unsupported {
		return nil, refusal
	}
	var raw []byte
	var err error
	if refusal != nil {
		raw, err = encodeAcceptedInputRefusal(t.ID, a.Manifest().ID, name, m, body, now, refusal)
	} else {
		raw, err = draft.inputResult(a.Manifest().ID, name, m, body, answer, now)
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input cannot produce a bounded accepted result")
	}
	principal, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = t.procs.Versions()
	}
	committed, err := t.AcceptResult(Entry{App: a.Manifest().ID, Kind: "accepted-result",
		Principal: principal, Body: raw, At: now, Versions: versions}, key, hash)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return t.finishCommittedInput(a, m, name, key, hash, committed)
}

func (t *Tenant) finishCommittedInput(a platform.AcceptedInputApp, m platform.Member, name, key, hash string, committed []byte) (answer any, refusal *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("apply committed connector result: %v", failure))
			answer = nil
			refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}()
	result, applied, err := t.applyAcceptedInput(committed)
	if err != nil || result.Key != key || result.RequestHash != hash || result.Member != m.ID ||
		result.Name != name || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("committed connector input differs from request: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if applied && result.Refusal == nil {
		t.audit.remember(AuditEntry{At: result.At, Member: m.ID, App: result.App, Action: "input:" + name})
		t.enqueue(result.At)
		t.changedOwner(result.App)
	}
	return t.answerAcceptedInput(a, committed)
}

func decideAcceptedInput(a platform.AcceptedInputApp, draft *stagedDecision, m platform.Member, name string, body []byte, at time.Time) (answer any, refusal *kernel.Error, unsupported bool) {
	defer func() {
		if p := recover(); p != nil {
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				panic(p)
			}
			answer = nil
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input uses an effect outside the accepted-result boundary")
			unsupported = true
		}
	}()
	answer, refusal = draft.stateApp(a).Input(platform.NewCaller(draft, m, a.Manifest().ID, false, false),
		name, slices.Clone(body), at)
	return answer, refusal, false
}

func (t *Tenant) answerAcceptedInput(a platform.AcceptedInputApp, raw []byte) (any, *kernel.Error) {
	result, err := decodeAcceptedInput(raw)
	if err != nil || result.Tenant != t.ID || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("saved connector answer is incompatible: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if result.Refusal != nil {
		return nil, result.Refusal
	}
	answer, err := a.DecodeAcceptedInput(result.Answer)
	if err != nil {
		t.quarantine(fmt.Errorf("saved connector answer cannot be decoded: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return answer, nil
}

func decodeAcceptedInput(raw []byte) (acceptedInput, error) {
	var result acceptedInput
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return result, fmt.Errorf("connector result is empty or oversized")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("connector result has trailing data")
	}
	digest, err := digestAcceptedInput(result)
	if err != nil || digest != result.Digest {
		return result, fmt.Errorf("connector result digest differs")
	}
	hash, err := acceptedInputHash(result.Tenant, result.App, result.Name, result.Member, result.Body)
	if err != nil || result.Version != 1 || result.Kind != "input-result" ||
		result.Tenant == "" || result.App == "" || result.Name == "" || result.Member == "" ||
		result.At.IsZero() || result.RequestHash != hash || result.Key != "input:"+hash ||
		result.Refusal != nil && (result.Refusal.Code == pb.ErrorCode_ERROR_CODE_UNSPECIFIED ||
			len(result.Changes) > 0 || len(result.Rows) > 0 || len(result.Deliveries) > 0 ||
			len(result.States) > 0 || result.Notices != nil) ||
		result.Refusal == nil && len(result.Answer) == 0 {
		return result, fmt.Errorf("connector result has inconsistent identity or effects")
	}
	if len(result.Changes) > 0 {
		batch, receipt, err := decodeAcceptedBatch(result.Changes)
		answer := new(pb.ChangeRecord)
		if err != nil || protojson.Unmarshal(result.Answer, answer) != nil ||
			batch.Tenant != result.Tenant || batch.App != result.App || !batch.At.Equal(result.At) ||
			receipt.GetSubmission().GetPrincipalId() != result.Member || !proto.Equal(receipt, answer) ||
			len(result.Rows) > 0 || len(result.Deliveries) > 0 || len(result.States) > 0 || result.Notices != nil {
			return result, fmt.Errorf("connector result has a foreign child decision: %v", err)
		}
	}
	seen := map[string]bool{}
	for _, row := range result.Rows {
		ref := row.Type + "/" + row.ID
		if row.Type == "" || row.ID == "" || row.App != result.App || seen[ref] ||
			len(row.Value) == 0 || len(row.History) == 0 {
			return result, fmt.Errorf("connector result has a duplicate or invalid row")
		}
		seen[ref] = true
	}
	return result, nil
}

func (t *Tenant) applyAcceptedInput(raw []byte) (acceptedInput, bool, error) {
	result, err := decodeAcceptedInput(raw)
	if err != nil {
		return result, false, err
	}
	app, ok := t.app(result.App).(platform.AcceptedInputApp)
	if result.Tenant != t.ID || !ok || !slices.Contains(app.AcceptedInputs(), result.Name) {
		return result, false, fmt.Errorf("connector result belongs to another input")
	}
	if result.Refusal == nil {
		if _, err := app.DecodeAcceptedInput(result.Answer); err != nil {
			return result, false, fmt.Errorf("connector result has an invalid answer: %w", err)
		}
	}
	if prior := t.committed.inputs[result.App+"/"+result.Key]; len(prior) > 0 {
		saved, err := decodeAcceptedInput(prior)
		if err != nil || saved.Digest != result.Digest {
			return result, false, fmt.Errorf("connector input key belongs to another result")
		}
		return result, false, nil
	}
	if len(result.Changes) > 0 {
		owner, ok := t.app(result.App).(platform.ResultApp)
		if !ok {
			return result, false, fmt.Errorf("connector result needs missing child authority")
		}
		applied, err := t.applyAcceptedBatch(owner.AcceptedLedger(), result.Changes)
		if err != nil || !applied {
			return result, false, fmt.Errorf("connector child result: %v (applied=%t)", err, applied)
		}
		batch, _, _ := decodeAcceptedBatch(result.Changes)
		t.publishAcceptedBatch(batch)
		t.committed.saveInput(result.App+"/"+result.Key, raw)
		return result, true, nil
	}
	if err := t.validateAcceptedDeliveries(result.Deliveries); err != nil {
		return result, false, err
	}
	if err := t.validateAcceptedNotices(result.Notices); err != nil {
		return result, false, err
	}
	if err := t.validateAcceptedStates(result.States); err != nil {
		return result, false, err
	}
	draft, err := t.prepareAcceptedDirectRows(result.Rows, result.Member, result.At)
	if err != nil {
		return result, false, err
	}
	if len(result.Rows) > 0 {
		if err := t.records.promoteRecords(draft); err != nil {
			return result, false, err
		}
	}
	if err := t.applyAcceptedStates(result.States); err != nil {
		return result, false, err
	}
	if err := t.applyAcceptedDeliveries(result.Deliveries); err != nil {
		return result, false, err
	}
	t.applyAcceptedNotices(result.Notices)
	if result.Refusal != nil {
		t.refused(result.Member, result.Name, result.Refusal, result.At)
	}
	t.committed.saveInput(result.App+"/"+result.Key, raw)
	return result, true, nil
}
