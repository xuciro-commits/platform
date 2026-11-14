package kernel

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type EffectApply struct {
	Changes ChangeApply
	Current func(id string) (acceptedformat.EffectState, bool)
	Finish  func(platform.Effect, platform.Outcome) (*kernel.Works, json.RawMessage, json.RawMessage, error)
	Usage   func() (json.RawMessage, error)
	Settle  func(id string, after acceptedformat.EffectState, finished *pb.Work)
	Meter   func(platform.AIUsage)
	Publish func(platform.Event, platform.AcceptedEventPlan)
}

func (s *Sequence) ApplyEffectResult(raw []byte, h EffectApply) (acceptedformat.Effect, bool, error) {
	result, err := acceptedformat.DecodeEffect(raw)
	if err != nil {
		return result, false, err
	}
	if result.Tenant != s.ID {
		return result, false, fmt.Errorf("effect result belongs to another tenant")
	}
	before, found := h.Current(result.Before.ID)
	beforeHash, err := platform.CanonicalDigest(before)
	savedHash, savedErr := platform.CanonicalDigest(result.Before)
	if !found || err != nil || savedErr != nil || beforeHash != savedHash {
		return result, false, fmt.Errorf("effect result predecessor differs")
	}
	works, priorWork, savedWork, err := h.Finish(result.Before.Effect, result.Outcome)
	if err != nil {
		return result, false, err
	}
	if works != nil {
		priorHash, _ := platform.CanonicalDigest(priorWork)
		expectedPrior, _ := platform.CanonicalDigest(result.WorkBefore)
		workHash, _ := platform.CanonicalDigest(savedWork)
		expectedWork, _ := platform.CanonicalDigest(result.Work)
		if priorHash != expectedPrior || workHash != expectedWork {
			return result, false, fmt.Errorf("operation result ownership differs")
		}
	}
	if result.Usage != nil {
		if h.Usage == nil {
			return result, false, fmt.Errorf("effect result needs the AI meter")
		}
		state, err := h.Usage()
		currentHash, hashErr := platform.CanonicalDigest(state)
		if err != nil || hashErr != nil || currentHash != result.UsageBefore {
			return result, false, fmt.Errorf("effect result model usage predecessor differs")
		}
	}
	prepared, err := s.PrepareChanges(result.ChangePlan(), h.Changes)
	if err != nil {
		return result, false, err
	}
	if err := s.PromoteChanges(prepared, h.Changes); err != nil {
		return result, false, err
	}
	var finished *pb.Work
	if works != nil {
		finished, _ = works.Get(result.Before.ID)
	}
	h.Settle(result.Before.ID, result.After, finished)
	if result.Usage != nil {
		h.Meter(*result.Usage)
	}
	prepared.PublishChild(h.Publish)
	return result, true, nil
}

func (d *Decision) EffectDecision(src ResultSources, result *acceptedformat.Effect, app string, at time.Time) error {
	var err error
	if len(d.Events) == 0 && (len(d.Observations) > 0 || len(d.Allocated) > 0 ||
		len(d.Logs) > 0 || src.Pending) {
		err = fmt.Errorf("effect answer has unrepresented changes")
	} else {
		eventRows := map[string]bool{}
		for _, event := range d.Events {
			for _, ref := range event.Changed {
				eventRows[ref] = true
			}
		}
		result.Rows, err = d.CaptureDirectRows(app, nil, eventRows, "effect answer")
		if len(result.Rows) > 0 {
			result.Member = "app:" + app
		}
		if err == nil && len(d.Events) > 0 {
			writes := d.Records.WriteSnapshot()
			d.Records.UseWrites(eventRows)
			last := d.Events[len(d.Events)-1]
			result.Changes, err = d.BatchResult(src, last.App, last.Record.GetSubmission(), last.Record, at)
			d.Records.UseWrites(writes)
		} else if err == nil {
			var plan acceptedformat.ChangePlan
			plan, err = d.ChangePlan(src.Control)
			result.Notices, result.Intents = plan.Notices, plan.Intents
			result.States, result.Control = plan.States, plan.Control
		}
	}
	if err != nil {
		return err
	}
	result.Unique = d.SavedUniqueChanges()
	return nil
}
