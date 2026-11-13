package kernel

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	acceptedformat "platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"time"
)

type ReplayedResult struct {
	App, Member string
	At          time.Time
	Applied     bool
	After       func(entry time.Time)
}

type ReplayRule struct {
	Label   string
	Applied bool
	Apply   func(body []byte) (ReplayedResult, error)
}

type ReplayHooks struct {
	Rules         map[string]ReplayRule
	HostControl   func() HostControlOwner
	Work          func(body []byte) error
	Batch         func(l *platform.Ledger, body []byte) (bool, error)
	Record        func(l *platform.Ledger, body []byte) (bool, error)
	Submitted     func(member string, a platform.App, s *pb.Submission, at time.Time) platform.AuditEntry
	Invalidate    func()
	Pin           func(versions map[string]int)
	App           func(id string) platform.App
	Audit         func(platform.AuditEntry)
	Enqueue       func(time.Time)
	PublishBatch  func(acceptedformat.Batch)
	PublishRecord func(platform.Event, platform.AcceptedEventPlan)
}

func (s *Sequence) ReplayEntries(entries []persistence.Entry, h ReplayHooks) error {
	s.Lane.Lock()
	defer s.Lane.Unlock()
	h.Invalidate()
	defer h.Pin(nil)
	for i, e := range entries {
		if e.Epoch != persistence.FormatEpoch {
			return fmt.Errorf("entry %d: journal is format epoch %d and this host reads epoch %d; rebuild the tenant", i+1, e.Epoch, persistence.FormatEpoch)
		}
	}
	for i, e := range entries {
		h.Pin(e.Versions)
		var m platform.Member
		a := h.App(e.App)
		if a == nil || json.Unmarshal(e.Principal, &m) != nil {
			return fmt.Errorf("entry %d: app %q not enabled or member unreadable", i+1, e.App)
		}
		if err := s.replayResult(i, e, m, a, h); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sequence) replayResult(i int, e persistence.Entry, m platform.Member, a platform.App, h ReplayHooks) error {
	var envelope struct{ Kind string }
	if err := json.Unmarshal(e.Body, &envelope); err != nil {
		return fmt.Errorf("entry %d: unreadable result envelope: %w", i+1, err)
	}
	if rule, ok := h.Rules[envelope.Kind]; ok {
		saved, err := rule.Apply(e.Body)
		if err != nil || rule.Applied && !saved.Applied || saved.App != e.App || saved.Member != m.ID || !persistence.SameTime(saved.At, e.At) {
			if rule.Applied {
				return fmt.Errorf("entry %d: %s: %v (applied=%t)", i+1, rule.Label, err, saved.Applied)
			}
			return fmt.Errorf("entry %d: %s: %v", i+1, rule.Label, err)
		}
		if saved.After != nil {
			saved.After(e.At)
		}
		return nil
	}
	switch envelope.Kind {
	case "host-result":
		saved, err := acceptedformat.DecodeHostControl(e.Body)
		if err != nil || saved.App != e.App || saved.Member != m.ID || saved.Tenant != s.ID || !persistence.SameTime(saved.At, e.At) {
			return fmt.Errorf("entry %d: invalid host result: %v", i+1, err)
		}
		if _, err := s.ApplyHostControl(e.Body, h.HostControl()); err != nil {
			return fmt.Errorf("entry %d: host result: %w", i+1, err)
		}
		return nil
	case "work-result":
		saved, decodeErr := acceptedformat.DecodeWork(e.Body)
		if decodeErr != nil || saved.App != e.App || !persistence.SameTime(saved.At, e.At) || m.ID != "app:"+e.App {
			return fmt.Errorf("entry %d: invalid work result: %v", i+1, decodeErr)
		}
		if err := h.Work(e.Body); err != nil {
			return fmt.Errorf("entry %d: work result: %w", i+1, err)
		}
		h.Enqueue(e.At)
		return nil
	case "refusal":
		saved, sub, decodeErr := acceptedformat.DecodeRefusal(e.Body)
		if decodeErr != nil || saved.App != e.App || saved.Tenant != s.ID ||
			!persistence.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
			return fmt.Errorf("entry %d: invalid refused result: %v", i+1, decodeErr)
		}
		key := e.App + "/" + sub.GetIdempotencyKey()
		if _, duplicate := s.Committed.Refusal(key); duplicate || a.AcceptedLedger().AcceptedFor(s.ID, sub.GetIdempotencyKey()) != nil {
			return fmt.Errorf("entry %d: duplicate refused result key", i+1)
		}
		s.Committed.SaveRefusal(key, saved)
		return nil
	case "record-batch":
		saved, receipt, decodeErr := acceptedformat.DecodeBatch(e.Body)
		if decodeErr != nil {
			return fmt.Errorf("entry %d: invalid record batch: %w", i+1, decodeErr)
		}
		sub, subErr := acceptedformat.BatchSubmission(saved, receipt)
		if subErr != nil {
			return fmt.Errorf("entry %d: invalid record batch request: %w", i+1, subErr)
		}
		if saved.App != e.App || saved.Tenant != s.ID || !persistence.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
			return fmt.Errorf("entry %d: record batch journal identity differs (app=%q/%q tenant=%q/%q principal=%q/%q time=%s/%s)",
				i+1, saved.App, e.App, saved.Tenant, s.ID, sub.GetPrincipalId(), m.ID, saved.At, e.At)
		}
		if _, refused := s.Committed.Refusal(e.App + "/" + sub.GetIdempotencyKey()); refused {
			return fmt.Errorf("entry %d: record batch reused a refused key", i+1)
		}
		applied, applyErr := h.Batch(a.AcceptedLedger(), e.Body)
		if applyErr != nil || !applied {
			return fmt.Errorf("entry %d: record batch: %v (applied=%t)", i+1, applyErr, applied)
		}
		h.Audit(h.Submitted(m.ID, a, sub, saved.At))
		h.PublishBatch(saved)
		h.Enqueue(e.At)
		return nil
	}
	saved, receipt, decodeErr := acceptedformat.DecodeRecord(e.Body)
	if decodeErr != nil {
		return fmt.Errorf("entry %d: invalid accepted result: %v", i+1, decodeErr)
	}
	if saved.App != e.App || receipt.GetSubmission().GetPrincipalId() != m.ID || !persistence.SameTime(saved.At, e.At) {
		return fmt.Errorf("entry %d: accepted result app or input clock differs from journal entry", i+1)
	}
	if _, refused := s.Committed.Refusal(e.App + "/" + receipt.GetSubmission().GetIdempotencyKey()); refused {
		return fmt.Errorf("entry %d: accepted result reused a refused key", i+1)
	}
	applied, applyErr := h.Record(a.AcceptedLedger(), e.Body)
	if applyErr != nil || !applied {
		return fmt.Errorf("entry %d: accepted result: %v (applied=%t)", i+1, applyErr, applied)
	}
	h.Audit(h.Submitted(m.ID, a, receipt.GetSubmission(), e.At))
	h.PublishRecord(platform.Event{App: e.App, Record: receipt, Changed: saved.Event.Changed}, saved.Event)
	h.Enqueue(e.At)
	return nil
}
