package host

import (
	"encoding/json"
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/services/security/control"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/actions"
)

func (t *Tenant) Replay(entries []Entry) error {
	t.Lane.Lock()
	defer t.Lane.Unlock()
	t.invalidateReadImage()
	if t.procs != nil {
		defer t.procs.Pin(nil)
	}
	for i, e := range entries {
		if e.Epoch != persistence.FormatEpoch {
			return fmt.Errorf("entry %d: journal is format epoch %d and this host reads epoch %d; rebuild the tenant", i+1, e.Epoch, persistence.FormatEpoch)
		}
	}
	for i, e := range entries {
		if t.procs != nil {
			t.procs.Pin(e.Versions)
		}
		var m platform.Member
		a := t.App(e.App)
		if a == nil || json.Unmarshal(e.Principal, &m) != nil {
			return fmt.Errorf("entry %d: app %q not enabled or member unreadable", i+1, e.App)
		}
		if err := t.replayAcceptedResult(i, e, m, a); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) replayAcceptedResult(i int, e Entry, m platform.Member, a platform.App) error {
	var envelope struct{ Kind string }
	if err := json.Unmarshal(e.Body, &envelope); err != nil {
		return fmt.Errorf("entry %d: unreadable result envelope: %w", i+1, err)
	}
	if envelope.Kind == "release-result" {
		saved, err := t.applyAcceptedRelease(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID ||
			!persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: immutable release result: %v", i+1, err)
		}
		return nil
	}
	if envelope.Kind == "composite-result" {
		saved, err := t.applyAcceptedComposite(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID || !persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: composite result: %v", i+1, err)
		}
		t.changed()
		return nil
	}
	if envelope.Kind == "input-result" {
		saved, applied, err := t.applyAcceptedInput(e.Body)
		if err != nil || !applied || saved.App != e.App || saved.Member != m.ID || !persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: connector result: %v (applied=%t)", i+1, err, applied)
		}
		if saved.Refusal == nil {
			t.audit.Remember(platform.AuditEntry{At: e.At, Member: m.ID, App: e.App, Action: "input:" + saved.Name})
			t.enqueue(e.At)
		}
		return nil
	}
	if envelope.Kind == "effect-result" {
		saved, applied, err := t.applyAcceptedEffect(e.Body)
		if err != nil || !applied || saved.App != e.App || m.ID != "app:"+controlowner.PlatformApp ||
			!persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: effect result: %v (applied=%t)", i+1, err, applied)
		}
		t.enqueue(saved.At)
		return nil
	}
	if envelope.Kind == "host-result" {
		saved, err := acceptedformat.DecodeHostControl(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID || saved.Tenant != t.ID || !persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: invalid host result: %v", i+1, err)
		}
		if _, err := t.applyAcceptedHostControl(e.Body); err != nil {
			return fmt.Errorf("entry %d: host result: %w", i+1, err)
		}
		return nil
	}
	if envelope.Kind == "usage-result" {
		saved, err := t.applyAcceptedUsage(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID || !persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: model usage result: %v", i+1, err)
		}
		return nil
	}
	if envelope.Kind == "agent-result" {
		saved, err := t.applyAcceptedAgent(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID || !persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: agent result: %v", i+1, err)
		}
		t.enqueue(saved.At)
		return nil
	}
	if envelope.Kind == "operation-claim" {
		saved, err := t.applyOperationClaim(e.Body)
		if err != nil || saved.App != e.App || m.ID != "app:"+controlowner.PlatformApp || !persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: operation claim: %v", i+1, err)
		}
		t.outbound.Update(saved.Before.ID, func(x *actions.Queued) { x.Sending = false })
		return nil
	}
	ra := a
	if envelope.Kind == "work-result" {
		saved, decodeErr := acceptedformat.DecodeWork(e.Body)
		if decodeErr != nil || saved.App != e.App || !persistence.SameTime(saved.At, e.At) || m.ID != "app:"+e.App {
			return fmt.Errorf("entry %d: invalid work result: %v", i+1, decodeErr)
		}
		if _, err := t.applyAcceptedWork(e.Body); err != nil {
			return fmt.Errorf("entry %d: work result: %w", i+1, err)
		}
		t.enqueue(e.At)
		return nil
	}
	if envelope.Kind == "refusal" {
		saved, sub, decodeErr := acceptedformat.DecodeRefusal(e.Body)
		if decodeErr != nil || saved.App != e.App || saved.Tenant != t.ID ||
			!persistence.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
			return fmt.Errorf("entry %d: invalid refused result: %v", i+1, decodeErr)
		}
		key := e.App + "/" + sub.GetIdempotencyKey()
		if _, duplicate := t.Committed.Refusal(key); duplicate || ra.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()) != nil {
			return fmt.Errorf("entry %d: duplicate refused result key", i+1)
		}
		t.Committed.SaveRefusal(key, saved)
		return nil
	}
	if envelope.Kind == "record-batch" {
		saved, receipt, decodeErr := acceptedformat.DecodeBatch(e.Body)
		if decodeErr != nil {
			return fmt.Errorf("entry %d: invalid record batch: %w", i+1, decodeErr)
		}
		sub, subErr := acceptedformat.BatchSubmission(saved, receipt)
		if subErr != nil {
			return fmt.Errorf("entry %d: invalid record batch request: %w", i+1, subErr)
		}
		if saved.App != e.App || saved.Tenant != t.ID || !persistence.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
			return fmt.Errorf("entry %d: record batch journal identity differs (app=%q/%q tenant=%q/%q principal=%q/%q time=%s/%s)",
				i+1, saved.App, e.App, saved.Tenant, t.ID, sub.GetPrincipalId(), m.ID, saved.At, e.At)
		}
		if _, refused := t.Committed.Refusal(e.App + "/" + sub.GetIdempotencyKey()); refused {
			return fmt.Errorf("entry %d: record batch reused a refused key", i+1)
		}
		applied, applyErr := t.applyAcceptedBatch(ra.AcceptedLedger(), e.Body)
		if applyErr != nil || !applied {
			return fmt.Errorf("entry %d: record batch: %v (applied=%t)", i+1, applyErr, applied)
		}
		t.audit.Remember(submitted(m.ID, a, sub, saved.At))
		t.publishAcceptedBatch(saved)
		t.enqueue(e.At)
		return nil
	}
	saved, receipt, decodeErr := acceptedformat.DecodeRecord(e.Body)
	if decodeErr != nil {
		return fmt.Errorf("entry %d: invalid accepted result: %v", i+1, decodeErr)
	}
	if saved.App != e.App || receipt.GetSubmission().GetPrincipalId() != m.ID || !persistence.SameTime(saved.At, e.At) {
		return fmt.Errorf("entry %d: accepted result app or input clock differs from journal entry", i+1)
	}
	if _, refused := t.Committed.Refusal(e.App + "/" + receipt.GetSubmission().GetIdempotencyKey()); refused {
		return fmt.Errorf("entry %d: accepted result reused a refused key", i+1)
	}
	applied, applyErr := t.applyAcceptedResult(ra.AcceptedLedger(), e.Body)
	if applyErr != nil || !applied {
		return fmt.Errorf("entry %d: accepted result: %v (applied=%t)", i+1, applyErr, applied)
	}
	t.audit.Remember(submitted(m.ID, a, receipt.GetSubmission(), e.At))
	t.publishAccepted(platform.Event{App: e.App, Record: receipt, Changed: saved.Event.Changed}, saved.Event)
	t.enqueue(e.At)
	return nil
}
