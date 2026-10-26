package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"

	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"

	"slices"

	"time"

	"platformserver/platform"
)

// applyAcceptedEffect checks every predecessor before promoting an effect
// outcome. Neither the application callback nor the external send is run here.
func (t *Tenant) applyAcceptedEffect(raw []byte) (resultformat.Effect, bool, error) {
	result, err := resultformat.DecodeEffect(raw)
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
	var before resultformat.EffectState
	if current != nil {
		before = resultformat.EffectState{Effect: current.Effect, Since: current.since}
	}
	t.opsMu.Unlock()
	beforeHash, err := acceptance.Digest(before)
	savedHash, savedErr := acceptance.Digest(result.Before)
	if current == nil || err != nil || savedErr != nil || beforeHash != savedHash {
		return result, false, fmt.Errorf("effect result predecessor differs")
	}
	works, priorWork, savedWork, err := t.prepareOperationFinish(result.Before.Effect, result.Outcome)
	if err != nil {
		return result, false, err
	}
	if works != nil {
		priorHash, _ := acceptance.Digest(priorWork)
		expectedPrior, _ := acceptance.Digest(result.WorkBefore)
		workHash, _ := acceptance.Digest(savedWork)
		expectedWork, _ := acceptance.Digest(result.Work)
		if priorHash != expectedPrior || workHash != expectedWork {
			return result, false, fmt.Errorf("operation result ownership differs")
		}
	}
	if result.Usage != nil {
		if t.ai == nil {
			return result, false, fmt.Errorf("effect result needs the AI meter")
		}
		state, err := t.ai.Snapshot()
		currentHash, hashErr := acceptance.Digest(state)
		if err != nil || hashErr != nil || currentHash != result.UsageBefore {
			return result, false, fmt.Errorf("effect result model usage predecessor differs")
		}
	}
	prepared, err := t.prepareAcceptedChanges(result.ChangePlan())
	if err != nil {
		return result, false, err
	}
	if err := prepared.apply(t); err != nil {
		return result, false, err
	}
	t.opsMu.Lock()
	current.Effect, current.since, current.sending = result.After.Effect, result.After.Since, false
	t.opsMu.Unlock()
	if works != nil {
		// The child may have cancelled other computation generations. Promote
		// only this work's frozen completion, never overwrite that child state.
		finished, _ := works.Get(result.Before.ID)
		t.installAcceptedWork(finished)
	}
	if result.Usage != nil {
		t.ai.Meter(*result.Usage)
	}
	prepared.publish(t)
	return result, true, nil
}

// settleAccepted stages the observed outcome and its callbacks before one
// append. A failed append releases the transient in-flight flag so the same
// outbound ID can be retried; neither the outcome nor callback becomes visible.
func (t *Tenant) settleAccepted(id string, out platform.Outcome, usage *platform.AIUsage, at time.Time) {
	t.opsMu.Lock()
	var pending *effect
	for _, x := range t.outbound {
		if x.ID == id {
			pending = x
			break
		}
	}
	var before resultformat.EffectState
	if pending != nil {
		before = resultformat.EffectState{Effect: pending.Effect, Since: pending.since}
	}
	t.opsMu.Unlock()
	if pending == nil || out.Effect != id {
		t.quarantine(fmt.Errorf("external answer for an unknown or different effect %s", id))
		return
	}
	if settled(before.State) && before.State != "discarded" {
		return // A late transport answer cannot replace an accepted human/source resolution.
	}
	if usage != nil {
		copy := *usage
		copy.Applications = slices.Clone(before.Effect.Applications)
		copy.Run = before.Effect.ID
		if before.Effect.Endpoint == resultformat.ModelEndpoint {
			var ask modelAsk
			if json.Unmarshal([]byte(before.Effect.Body), &ask) == nil && ask.Function != nil {
				owner := ask.Function.Owner
				if owner == "" {
					owner = before.Effect.App
				}
				ref := platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: ask.Function.Definition.Name}
				copy.Resource = &ref
			}
		}
		usage = &copy
	}
	// New attempts always retain a safe public message when they retain an
	// error. An empty message on an old effect remains an explicit legacy case.
	if out.Detail != "" && out.PublicMessage == "" {
		out.PublicMessage = "The last delivery attempt failed"
	}
	after := &effect{Effect: before.Effect, since: before.Since}
	markEffect(after, out, at)
	result := resultformat.Effect{Version: 5, Kind: "effect-result", Tenant: t.ID, App: PlatformApp,
		Key: resultformat.EffectResultKey(before), At: at.UTC(), Before: before,
		After: resultformat.EffectState{Effect: after.Effect, Since: after.since}, Outcome: out, Usage: usage}
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
			result.UsageBefore, snapshotErr = acceptance.Digest(state)
		}
		if snapshotErr != nil {
			t.quarantine(fmt.Errorf("effect %s usage checkpoint: %w", id, snapshotErr))
			return
		}
	}
	result.RequestHash, err = resultformat.EffectResultHash(before, out, result.At)
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
	if (settled(after.State) || after.State == "unknown") && before.State != "discarded" {
		if err := t.stageEffectAnswer(draft, after.Effect, out, usage, at); err != nil {
			if t.quarantined() {
				return
			}
			// A business refusal is itself a durable callback outcome. Drop its
			// private writes; an unsupported staging operation is not a
			// business refusal and instead isolates this tenant.
			result.CallbackError = err.Error()
			result.Authorizations = slices.Clone(draft.authorizations)
			draft = t.newStagedDecision()
			draft.at = at
		}
	}
	result.Authorizations = append(result.Authorizations, draft.authorizations...)
	result.After.CallbackError = result.CallbackError
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
		result.Rows, err = captureDirectRows(draft, before.App, nil, eventRows, "effect answer")
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
			var plan resultformat.ChangePlan
			plan, err = draft.savedChangePlan()
			result.Notices, result.Intents = plan.Notices, plan.Intents
			result.States, result.Control = plan.States, plan.Control
		}
	}
	if err != nil {
		t.quarantine(fmt.Errorf("stage effect %s: %w", id, err))
		return
	}
	result.Unique = draft.savedUniqueChanges()
	if _, err := t.prepareAcceptedChanges(result.ChangePlan()); err != nil {
		t.quarantine(fmt.Errorf("stage effect %s changes: %w", id, err))
		return
	}
	result.Digest, err = resultformat.DigestEffect(result)
	if err != nil {
		t.quarantine(err)
		return
	}
	raw, err := json.Marshal(result)
	if err == nil {
		_, err = resultformat.DecodeEffect(raw)
	}
	if err != nil {
		t.quarantine(fmt.Errorf("encode effect %s: %w", id, err))
		return
	}
	principal, _ := json.Marshal(t.automation(PlatformApp, false).Member)
	committed, err := t.acceptResult(Entry{App: PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at}, result.Key, result.RequestHash)
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
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	if len(saved.Changes) == 0 && len(saved.Rows) == 0 && len(saved.States) == 0 && saved.Notices == nil {
		t.operationsChanged(saved.App)
	} else {
		t.changedOwner(saved.App)
	}
}

func (t *Tenant) stageEffectAnswer(draft *stagedDecision, x platform.Effect, out platform.Outcome, usage *platform.AIUsage, at time.Time) (err error) {
	defer func() {
		if p := recover(); p != nil {
			// An unsupported staged effect must never masquerade as a durable
			// business refusal and silently discard a received answer.
			t.quarantine(fmt.Errorf("effect callback cannot stage: %v", p))
			err = fmt.Errorf("unsupported effect callback")
		}
	}()
	if x.App != "" {
		c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
		if refusal := draft.authorizeNative(c, "effect:"+x.Endpoint, "action", nil, t.app(x.App) != nil); refusal != nil {
			return refusal
		}
	}
	if x.Endpoint == resultformat.ModelEndpoint {
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
	if x.Endpoint == resultformat.OperationEndpoint {
		draft.operationAnswers = map[string]platform.OperationResult{x.ID: operationResultOf(x)}

	}
	c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
	if app, ok := draft.stateApp(t.app(x.App)).(platform.Answerer); ok {
		var source platform.Effect
		t.opsMu.Lock()
		for _, effect := range t.outbound {
			if effect.ID == x.ID {
				source = effect.Effect
				break
			}
		}
		t.opsMu.Unlock()
		if source.ID == "" || source.App != x.App {
			return fmt.Errorf("effect callback has no accepted source")
		}
		previous := draft.callbackOrigin
		refusal := func() *kernel.Error {
			draft.callbackOrigin = &source
			defer func() { draft.callbackOrigin = previous }()
			return app.Answer(c, x, out, at)
		}()
		if refusal != nil {
			return refusal
		}
	}
	if t.agents != nil {
		draft.stateApp(t.agents).(AgentRuntime).AgentEffectEnded(t.automated(c, AgentApp), nil, x, out, at)
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
