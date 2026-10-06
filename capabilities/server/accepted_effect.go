package platformserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"io"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/apps/ai"
	"platformserver/platform"
)

// An external attempt's answer is its own top-level input. The effect state,
// application callback and any resulting decisions belong to the same
// accepted journal row; applying one never resends the external request.
type acceptedEffect struct {
	Version       int                `json:"version"`
	Kind          string             `json:"kind"`
	Tenant        string             `json:"tenant"`
	App           string             `json:"app"`
	Key           string             `json:"key"`
	At            time.Time          `json:"at"`
	RequestHash   string             `json:"requestHash"`
	Digest        string             `json:"digest"`
	Outcome       platform.Outcome   `json:"outcome"`
	Usage         *ai.Usage          `json:"usage,omitempty"`
	UsageBefore   string             `json:"usageBefore,omitempty"`
	CallbackError string             `json:"callbackError,omitempty"`
	Before        effectState        `json:"before"`
	After         effectState        `json:"after"`
	Member        string             `json:"member,omitempty"`
	Rows          []acceptedInputRow `json:"rows,omitempty"`
	Changes       json.RawMessage    `json:"changes,omitempty"`
	Notices       *acceptedNotices   `json:"notices,omitempty"`
	Intents       []platform.Effect  `json:"intents,omitempty"`
	States        []acceptedState    `json:"states,omitempty"`
	WorkBefore    json.RawMessage    `json:"workBefore,omitempty"`
	Work          json.RawMessage    `json:"work,omitempty"`
}

func effectResultKey(before effectState) string {
	return "effect:" + before.ID + ":" + strconv.Itoa(before.Attempts+1)
}

func effectResultHash(before effectState, out platform.Outcome, at time.Time) (string, error) {
	return canonicalDigest([]any{before, out, at})
}

func digestAcceptedEffect(result acceptedEffect) (string, error) {
	result.Digest = ""
	return canonicalDigest(result)
}

func decodeAcceptedEffect(raw []byte) (acceptedEffect, error) {
	var result acceptedEffect
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return result, fmt.Errorf("effect result is empty or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("effect result has trailing data")
	}
	digest, err := digestAcceptedEffect(result)
	hash, hashErr := effectResultHash(result.Before, result.Outcome, result.At)
	expected := &effect{Effect: result.Before.Effect, since: result.Before.Since}
	markEffect(expected, result.Outcome, result.At)
	if err != nil || hashErr != nil || result.Digest != digest || result.RequestHash != hash ||
		result.Version != 1 || result.Kind != "effect-result" || result.Tenant == "" ||
		result.App != PlatformApp || result.At.IsZero() || result.Before.ID == "" ||
		!slices.Contains([]string{"delivered", "rejected", "retry"}, result.Outcome.Result) ||
		len(result.Outcome.Answer) > 64<<10 ||
		len(result.Outcome.Answer) > 0 && !json.Valid(result.Outcome.Answer) ||
		result.Outcome.Effect != result.Before.ID || result.Key != effectResultKey(result.Before) ||
		!reflect.DeepEqual(result.After, effectState{Effect: expected.Effect, Since: expected.since}) {
		return result, fmt.Errorf("effect result has inconsistent identity, transition or digest")
	}
	if len(result.Changes) > 0 && (result.Notices != nil || len(result.Intents) > 0 || len(result.States) > 0) {
		return result, fmt.Errorf("effect result duplicates its child decision effects")
	}
	if len(result.Rows) > 0 && result.Member == "" {
		return result, fmt.Errorf("effect result omits the direct record writer")
	}
	for _, row := range result.Rows {
		if row.App != result.Before.App || result.Member != "app:"+row.App ||
			row.Type == "" || row.ID == "" || len(row.History) == 0 || len(row.Value) == 0 {
			return result, fmt.Errorf("effect result has an invalid direct record")
		}
	}
	if result.Usage != nil {
		if result.Before.Endpoint != modelEndpoint || result.UsageBefore == "" ||
			result.Usage.Member != "app:"+result.Before.App || result.Usage.Model == "" ||
			!result.Usage.At.Equal(result.At) || result.Usage.Input < 0 || result.Usage.Output < 0 ||
			result.Usage.Cost < 0 || result.Usage.Millis < 0 {
			return result, fmt.Errorf("effect result has an invalid model usage")
		}
	} else if result.UsageBefore != "" {
		return result, fmt.Errorf("effect result has a meter predecessor without usage")
	}
	if result.Before.Endpoint == operationEndpoint {
		var prior, work pb.Work
		if result.Outcome.Generation == 0 || result.Outcome.Generation != result.Before.Generation ||
			protojson.Unmarshal(result.WorkBefore, &prior) != nil || protojson.Unmarshal(result.Work, &work) != nil ||
			prior.GetWorkId() != result.Before.ID || prior.GetGeneration() != result.Before.Generation || prior.GetState() != pb.WorkState_WORK_STATE_RUNNING ||
			work.GetWorkId() != prior.GetWorkId() || work.GetGeneration() != prior.GetGeneration() {
			return result, fmt.Errorf("operation result has invalid ownership")
		}
	} else if len(result.WorkBefore) > 0 || len(result.Work) > 0 {
		return result, fmt.Errorf("external effect has compute ownership")
	}
	return result, nil
}

// applyAcceptedEffect checks every predecessor before promoting an effect
// outcome. Neither the application callback nor the external send is run here.
func (t *Tenant) applyAcceptedEffect(raw []byte) (acceptedEffect, bool, error) {
	result, err := decodeAcceptedEffect(raw)
	if err != nil {
		return result, false, err
	}
	if result.Tenant != t.ID {
		return result, false, fmt.Errorf("effect result belongs to another tenant")
	}
	t.opsMu.Lock()
	var current *effect
	for _, x := range t.outbound {
		if x.ID == result.Before.ID {
			current = x
			break
		}
	}
	var before effectState
	if current != nil {
		before = effectState{Effect: current.Effect, Since: current.since}
	}
	t.opsMu.Unlock()
	beforeHash, err := canonicalDigest(before)
	savedHash, savedErr := canonicalDigest(result.Before)
	if current == nil || err != nil || savedErr != nil || beforeHash != savedHash {
		return result, false, fmt.Errorf("effect result predecessor differs")
	}
	works, priorWork, savedWork, err := t.prepareOperationFinish(result.Before.Effect, result.Outcome)
	if err != nil {
		return result, false, err
	}
	if works != nil {
		priorHash, _ := canonicalDigest(priorWork)
		expectedPrior, _ := canonicalDigest(result.WorkBefore)
		workHash, _ := canonicalDigest(savedWork)
		expectedWork, _ := canonicalDigest(result.Work)
		if priorHash != expectedPrior || workHash != expectedWork {
			return result, false, fmt.Errorf("operation result ownership differs")
		}
	}
	if result.Usage != nil {
		if t.ai == nil {
			return result, false, fmt.Errorf("effect result needs the AI meter")
		}
		state, err := t.ai.Snapshot()
		currentHash, hashErr := canonicalDigest(state)
		if err != nil || hashErr != nil || currentHash != result.UsageBefore {
			return result, false, fmt.Errorf("effect result model usage predecessor differs")
		}
	}
	if err := t.validateAcceptedNotices(result.Notices); err != nil {
		return result, false, err
	}
	if err := t.validateAcceptedIntents(result.Intents); err != nil {
		return result, false, err
	}
	if err := t.validateAcceptedStates(result.States); err != nil {
		return result, false, err
	}
	if len(result.Rows) > 0 {
		if _, err := t.prepareAcceptedDirectRows(result.Rows, result.Member, result.At); err != nil {
			return result, false, err
		}
	}
	var child *acceptedBatch
	if len(result.Changes) > 0 {
		batch, _, err := decodeAcceptedBatch(result.Changes)
		if err != nil || batch.Tenant != t.ID || !batch.At.Equal(result.At) {
			return result, false, fmt.Errorf("effect result has an invalid child decision: %v", err)
		}
		owner, ok := t.app(batch.App).(platform.ResultApp)
		if !ok {
			return result, false, fmt.Errorf("effect result needs missing child authority")
		}
		directRefs := make(map[string]bool, len(result.Rows))
		for _, row := range result.Rows {
			directRefs[row.Type+"/"+row.ID] = true
		}
		for _, row := range batch.Rows {
			if directRefs[row.Type+"/"+row.ID] {
				return result, false, fmt.Errorf("effect result writes the same direct and child record")
			}
		}
		if applied, err := t.applyAcceptedBatch(owner.AcceptedLedger(), result.Changes); err != nil || !applied {
			return result, false, fmt.Errorf("effect child result: %v (applied=%t)", err, applied)
		}
		child = &batch
	}
	if len(result.Rows) > 0 {
		// The child advances the record-store generation. Fork after it is
		// applied, having checked the disjoint direct preimages above.
		direct, err := t.prepareAcceptedDirectRows(result.Rows, result.Member, result.At)
		if err != nil {
			return result, false, err
		}
		if err := t.records.promoteRecords(direct); err != nil {
			return result, false, err
		}
	}
	if child != nil {
		t.publishAcceptedBatch(*child)
	}
	if err := t.applyAcceptedStates(result.States); err != nil {
		return result, false, err
	}
	t.applyAcceptedNotices(result.Notices)
	t.installAcceptedIntents(result.Intents)
	t.opsMu.Lock()
	current.Effect, current.since, current.sending = result.After.Effect, result.After.Since, false
	t.opsMu.Unlock()
	if works != nil {
		// The child may have cancelled other computation generations. Promote
		// only this work's frozen completion, never overwrite that child state.
		finished, _ := works.Get(result.Before.ID)
		all := t.works.All()
		for i, work := range all {
			if work.GetWorkId() == finished.GetWorkId() {
				all[i] = finished
			}
		}
		t.works.Restore(all)
	}
	if result.Usage != nil {
		t.ai.Meter(*result.Usage)
	}
	return result, true, nil
}

// settleAccepted stages the observed outcome and its callbacks before one
// append. A failed append releases the transient in-flight flag so the same
// outbound ID can be retried; neither the outcome nor callback becomes visible.
func (t *Tenant) settleAccepted(id string, out platform.Outcome, usage *ai.Usage, at time.Time) {
	t.opsMu.Lock()
	var pending *effect
	for _, x := range t.outbound {
		if x.ID == id {
			pending = x
			break
		}
	}
	var before effectState
	if pending != nil {
		before = effectState{Effect: pending.Effect, Since: pending.since}
	}
	t.opsMu.Unlock()
	if pending == nil || out.Effect != id {
		t.quarantine(fmt.Errorf("external answer for an unknown or different effect %s", id))
		return
	}
	after := &effect{Effect: before.Effect, since: before.Since}
	markEffect(after, out, at)
	result := acceptedEffect{Version: 1, Kind: "effect-result", Tenant: t.ID, App: PlatformApp,
		Key: effectResultKey(before), At: at.UTC(), Before: before,
		After: effectState{Effect: after.Effect, Since: after.since}, Outcome: out, Usage: usage}
	var err error
	_, result.WorkBefore, result.Work, err = t.prepareOperationFinish(before.Effect, out)
	if err != nil {
		return
	}
	if usage != nil {
		if t.ai == nil {
			t.quarantine(fmt.Errorf("effect %s has usage without an AI meter", id))
			return
		}
		state, snapshotErr := t.ai.Snapshot()
		if snapshotErr == nil {
			result.UsageBefore, snapshotErr = canonicalDigest(state)
		}
		if snapshotErr != nil {
			t.quarantine(fmt.Errorf("effect %s usage checkpoint: %w", id, snapshotErr))
			return
		}
	}
	result.RequestHash, err = effectResultHash(before, out, result.At)
	if err != nil {
		t.quarantine(err)
		return
	}
	draft := t.newStagedDecision()
	draft.at = at
	if after.State == "failed" && t.app(PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + PlatformApp, Tenant: t.ID}, PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Effect to " + after.Endpoint + " failed: " + after.Event,
			Body: after.Error + ". Retry it in Settings → Automation once the cause is fixed.",
			Key:  "failed:" + after.ID}, at, platform.Recipient{AppRole: Admin})
	}
	if settled(after.State) && before.State != "discarded" {
		if err := t.stageEffectAnswer(draft, after.Effect, out, usage, at); err != nil {
			if t.quarantined() {
				return
			}
			// A business refusal is itself a durable callback outcome. Drop its
			// private writes; an unsupported staging operation is not a
			// business refusal and instead isolates this tenant.
			result.CallbackError = err.Error()
			draft = t.newStagedDecision()
			draft.at = at
		}
	}
	if draft.failure != nil {
		t.quarantine(fmt.Errorf("stage effect %s: %v", id, draft.failure))
		return
	}
	if len(draft.events) == 0 && (len(draft.deliveries) > 0 || len(draft.observations) > 0 || len(draft.allocated) > 0 ||
		len(draft.logs) > 0 || len(draft.requests) > 0) {
		err = fmt.Errorf("effect answer has unrepresented changes")
	} else {
		eventRows := map[string]bool{}
		for _, event := range draft.events {
			for _, ref := range event.Changed {
				eventRows[ref] = true
			}
		}
		result.Rows, err = captureDirectEffectRows(draft, before.App, eventRows)
		if len(result.Rows) > 0 {
			result.Member = "app:" + before.App
		}
		if err == nil && len(draft.events) > 0 {
			// A direct observation and a child decision may write disjoint
			// records in one callback. The child batch owns only its events;
			// direct rows retain their own preimages in the outer result.
			writes := draft.records.writes
			draft.records.writes = eventRows
			last := draft.events[len(draft.events)-1]
			result.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, at)
			draft.records.writes = writes
		} else if err == nil {
			result.Notices, result.Intents = draft.savedNotices(), slices.Clone(draft.intents)
			result.States, err = draft.savedStates()
		}
	}
	if err != nil {
		t.quarantine(fmt.Errorf("stage effect %s: %w", id, err))
		return
	}
	if len(result.Rows) > 0 {
		if _, err := t.prepareAcceptedDirectRows(result.Rows, result.Member, result.At); err != nil {
			t.quarantine(fmt.Errorf("stage effect %s direct rows: %w", id, err))
			return
		}
	}
	result.Digest, err = digestAcceptedEffect(result)
	if err != nil {
		t.quarantine(err)
		return
	}
	raw, err := json.Marshal(result)
	if err == nil {
		_, err = decodeAcceptedEffect(raw)
	}
	if err != nil {
		t.quarantine(fmt.Errorf("encode effect %s: %w", id, err))
		return
	}
	principal, _ := json.Marshal(t.automation(PlatformApp, false).Member)
	committed := raw
	if t.AcceptResult != nil {
		committed, err = t.AcceptResult(Entry{App: PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at}, result.Key, result.RequestHash)
	} else if app := t.app(PlatformApp); app != nil {
		t.record(app, "accepted-result", t.automation(PlatformApp, false).Member, raw, at)
	}
	if err != nil {
		if errors.Is(err, errAcceptedConflict) {
			t.quarantine(fmt.Errorf("effect %s conflicts with a previously committed outcome", id))
			return
		}
		t.opsMu.Lock()
		pending.sending = false
		t.opsMu.Unlock()
		return
	}
	saved, applied, err := t.applyAcceptedEffect(committed)
	if err != nil || saved.Key != result.Key || saved.RequestHash != result.RequestHash || !applied {
		t.quarantine(fmt.Errorf("apply committed effect %s: %v (applied=%t)", id, err, applied))
		return
	}
	t.enqueue(saved.At)
	if len(saved.Changes) == 0 && len(saved.Rows) == 0 && len(saved.States) == 0 && saved.Notices == nil {
		t.operationsChanged()
	} else {
		t.changedOwner(saved.App)
	}
}

func captureDirectEffectRows(d *stagedDecision, owner string, eventRows map[string]bool) ([]acceptedInputRow, error) {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	d.records.parent.mu.Lock()
	defer d.records.parent.mu.Unlock()
	var rows []acceptedInputRow
	for _, ref := range slices.Sorted(maps.Keys(d.records.writes)) {
		if eventRows[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.records.types[typ]
		if !ok || et == nil || et.info.App != owner || et.rows[id] == nil {
			return nil, fmt.Errorf("effect answer writes a foreign or undeclared record")
		}
		image, err := acceptedRowOf(typ, id, et.rows[id])
		if err != nil {
			return nil, err
		}
		row := acceptedInputRow{acceptedRow: image, App: owner}
		if prior := d.records.parent.types[typ]; prior != nil && prior.rows[id] != nil {
			old, err := acceptedRowOf(typ, id, prior.rows[id])
			if err != nil {
				return nil, err
			}
			row.Before, err = canonicalDigest(old)
			if err != nil {
				return nil, err
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (t *Tenant) stageEffectAnswer(draft *stagedDecision, x platform.Effect, out platform.Outcome, usage *ai.Usage, at time.Time) (err error) {
	defer func() {
		if p := recover(); p != nil {
			// An unsupported staged effect must never masquerade as a durable
			// business refusal and silently discard a received answer.
			t.quarantine(fmt.Errorf("effect callback cannot stage: %v", p))
			err = fmt.Errorf("unsupported effect callback")
		}
	}()
	if x.Endpoint == modelEndpoint {
		app, reply := t.modelAnswerSubmission(x, out, usage)
		if app == nil || reply == nil {
			return nil
		}
		c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
		_, refusal := draft.Decide(c, app, reply, at)
		if refusal != nil {
			return refusal
		}
		return nil
	}
	if x.App == "" {
		return nil
	}
	if x.Endpoint == operationEndpoint {
		draft.operationAnswers = map[string]platform.OperationResult{x.ID: operationResultOf(x)}
		if processes, ok := t.procs.(interface {
			OperationEnded(platform.Caller, string, time.Time) *kernel.Error
		}); ok {
			c := platform.NewCaller(draft, platform.Member{ID: "app:" + "flow", Tenant: t.ID}, "flow", false, true)
			if err := processes.OperationEnded(c, x.ID, at); err != nil {
				return err
			}
		}
	}
	c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
	if app, ok := t.app(x.App).(platform.Answerer); ok {
		if err := app.Answer(c, x, out, at); err != nil {
			return err
		}
	}
	if t.agents != nil {
		t.agents.effectEnded(t.automated(c, AgentApp), nil, x, out, at)
	}
	if draft.failure != nil {
		return draft.failure
	}
	if err := draft.answerRequests(at); err != nil {
		return err
	}
	if err := draft.stageObservers(); err != nil {
		return err
	}
	return nil
}
