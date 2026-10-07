package platformserver

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/journal"
	"platformserver/platform"
)

// Replay feeds recorded inputs through the apps that first accepted them, as the
// members they came from; a refusal means the record and the code disagree.
func (t *Tenant) Replay(entries []Entry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.procs != nil {
		defer t.procs.Pin(nil)
	}
	for i, e := range entries {
		if t.procs != nil {
			t.procs.Pin(e.Versions)
		}
		var m platform.Member
		a := t.app(e.App)
		if a == nil || json.Unmarshal(e.Principal, &m) != nil {
			return fmt.Errorf("entry %d: app %q not enabled or member unreadable", i+1, e.App)
		}
		var err *kernel.Error
		if e.Kind == "effect" { // an outbound attempt's outcome: applied, never sent again
			var o platform.Outcome
			if json.Unmarshal(e.Body, &o) != nil || !t.apply(o, e.At, true) {
				return fmt.Errorf("entry %d: effect outcome for an effect the replay did not create", i+1)
			}
			continue
		}
		if e.Kind == "usage" && t.ai != nil { // a model call's usage: applied, the call never made again
			var u ai.Usage
			if json.Unmarshal(e.Body, &u) != nil {
				return fmt.Errorf("entry %d: bad usage", i+1)
			}
			t.ai.Meter(u)
			continue
		}
		if e.Kind == "agent" && t.agents != nil { // a step an agent's model chose: applied, the model never called again
			var b stepBody
			if json.Unmarshal(e.Body, &b) != nil {
				return fmt.Errorf("entry %d: bad agent step", i+1)
			}
			if err := t.agents.apply(b, e.At, true); err != nil {
				return fmt.Errorf("entry %d: agent step: %v", i+1, err)
			}
			t.enqueue(e.At)
			continue
		}
		if e.Kind == "delivery" || e.Kind == "job" {
			if err := t.replayWork(e.Kind, e.Body, e.At); err != nil {
				return fmt.Errorf("entry %d: %v", i+1, err)
			}
			continue
		}
		if e.Kind == "accepted-result" {
			if err := t.replayAcceptedResult(i, e, m, a); err != nil {
				return err
			}
			continue
		}
		if e.Kind == "submission" {
			s := &pb.Submission{}
			if protojson.Unmarshal(e.Body, s) != nil {
				return fmt.Errorf("entry %d: bad submission", i+1)
			}
			_, err = a.Submit(t.caller(m, a, true), s, e.At)
			t.audit.remember(submitted(m.ID, a, s, e.At))
			t.enqueue(e.At)
		} else {
			_, err = a.Input(t.caller(m, a, true), e.Kind, e.Body, e.At)
			t.audit.remember(AuditEntry{At: e.At, Member: m.ID, App: e.App, Action: "input:" + e.Kind})
			t.enqueue(e.At)
		}
		if err != nil {
			return fmt.Errorf("entry %d (%s %s): %v", i+1, e.App, e.Kind, err)
		}
	}
	return nil
}

// replayAcceptedResult applies journal entry i, an accepted result of one of
// the pipeline's kinds, exactly as it was first answered.
func (t *Tenant) replayAcceptedResult(i int, e Entry, m platform.Member, a platform.App) error {
	var envelope struct{ Kind string }
	if err := json.Unmarshal(e.Body, &envelope); err != nil {
		return fmt.Errorf("entry %d: unreadable result envelope: %w", i+1, err)
	}
	if envelope.Kind == "release-result" {
		saved, err := t.applyAcceptedRelease(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID ||
			!journal.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: immutable release result: %v", i+1, err)
		}
		return nil
	}
	if envelope.Kind == "composite-result" {
		saved, err := t.applyAcceptedComposite(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID || !journal.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: composite result: %v", i+1, err)
		}
		t.changed()
		return nil
	}
	if envelope.Kind == "input-result" {
		saved, applied, err := t.applyAcceptedInput(e.Body)
		if err != nil || !applied || saved.App != e.App || saved.Member != m.ID || !journal.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: connector result: %v (applied=%t)", i+1, err, applied)
		}
		if saved.Refusal == nil {
			t.audit.remember(AuditEntry{At: e.At, Member: m.ID, App: e.App, Action: "input:" + saved.Name})
			t.enqueue(e.At)
		}
		return nil
	}
	if envelope.Kind == "effect-result" {
		saved, applied, err := t.applyAcceptedEffect(e.Body)
		if err != nil || !applied || saved.App != e.App || m.ID != "app:"+PlatformApp ||
			!journal.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: effect result: %v (applied=%t)", i+1, err, applied)
		}
		t.enqueue(saved.At)
		return nil
	}
	if envelope.Kind == "operation-claim" {
		saved, err := t.applyOperationClaim(e.Body)
		if err != nil || saved.App != e.App || m.ID != "app:"+PlatformApp || !journal.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: operation claim: %v", i+1, err)
		}
		t.opsMu.Lock()
		for _, x := range t.outbound {
			if x.ID == saved.Before.ID {
				x.sending = false
			}
		}
		t.opsMu.Unlock()
		return nil
	}
	ra, ok := a.(platform.ResultApp)
	if !ok {
		return fmt.Errorf("entry %d: app %s cannot apply accepted results", i+1, e.App)
	}
	if envelope.Kind == "work-result" {
		saved, decodeErr := decodeAcceptedWork(e.Body)
		if decodeErr != nil || saved.App != e.App || !journal.SameTime(saved.At, e.At) || m.ID != "app:"+e.App {
			return fmt.Errorf("entry %d: invalid work result: %v", i+1, decodeErr)
		}
		if _, err := t.applyAcceptedWork(e.Body); err != nil {
			return fmt.Errorf("entry %d: work result: %w", i+1, err)
		}
		t.enqueue(e.At)
		return nil
	}
	if envelope.Kind == "refusal" {
		saved, sub, decodeErr := decodeRefusedResult(e.Body)
		if decodeErr != nil || saved.App != e.App || saved.Tenant != t.ID ||
			!journal.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
			return fmt.Errorf("entry %d: invalid refused result: %v", i+1, decodeErr)
		}
		key := e.App + "/" + sub.GetIdempotencyKey()
		if _, duplicate := t.committed.refusals[key]; duplicate || ra.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()) != nil {
			return fmt.Errorf("entry %d: duplicate refused result key", i+1)
		}
		t.committed.refusals[key] = saved
		return nil
	}
	if envelope.Kind == "record-batch" {
		saved, receipt, decodeErr := decodeAcceptedBatch(e.Body)
		if decodeErr != nil {
			return fmt.Errorf("entry %d: invalid record batch: %w", i+1, decodeErr)
		}
		sub, subErr := batchSubmission(saved, receipt)
		if subErr != nil {
			return fmt.Errorf("entry %d: invalid record batch request: %w", i+1, subErr)
		}
		if saved.App != e.App || saved.Tenant != t.ID || !journal.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
			return fmt.Errorf("entry %d: record batch journal identity differs (app=%q/%q tenant=%q/%q principal=%q/%q time=%s/%s)",
				i+1, saved.App, e.App, saved.Tenant, t.ID, sub.GetPrincipalId(), m.ID, saved.At, e.At)
		}
		if _, refused := t.committed.refusals[e.App+"/"+sub.GetIdempotencyKey()]; refused {
			return fmt.Errorf("entry %d: record batch reused a refused key", i+1)
		}
		applied, applyErr := t.applyAcceptedBatch(ra.AcceptedLedger(), e.Body)
		if applyErr != nil || !applied {
			return fmt.Errorf("entry %d: record batch: %v (applied=%t)", i+1, applyErr, applied)
		}
		t.audit.remember(submitted(m.ID, a, sub, saved.At))
		t.publishAcceptedBatch(saved)
		t.enqueue(e.At)
		return nil
	}
	saved, receipt, decodeErr := decodeAcceptedResult(e.Body)
	if decodeErr != nil {
		return fmt.Errorf("entry %d: invalid accepted result: %v", i+1, decodeErr)
	}
	if saved.App != e.App || receipt.GetSubmission().GetPrincipalId() != m.ID ||
		saved.Version >= 2 && !journal.SameTime(saved.At, e.At) {
		return fmt.Errorf("entry %d: accepted result app or input clock differs from journal entry", i+1)
	}
	if _, refused := t.committed.refusals[e.App+"/"+receipt.GetSubmission().GetIdempotencyKey()]; refused {
		return fmt.Errorf("entry %d: accepted result reused a refused key", i+1)
	}
	applied, applyErr := t.applyAcceptedResult(ra.AcceptedLedger(), e.Body)
	if applyErr != nil || !applied {
		return fmt.Errorf("entry %d: accepted result: %v (applied=%t)", i+1, applyErr, applied)
	}
	t.audit.remember(submitted(m.ID, a, receipt.GetSubmission(), e.At))
	t.publishAccepted(platform.Event{App: e.App, Record: receipt, Changed: saved.Event.Changed}, saved.Event, saved.Version)
	t.enqueue(e.At)
	return nil
}
