package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai/gateway"
	"platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"
	"slices"
	"strings"
	"time"
)

func (t *Tenant) acceptAgentStep(b platform.AgentResultStep, at time.Time) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("agent answer cannot stage: %v", p)
		}
	}()
	draft := t.newStagedDecision()
	draft.At = at
	agent := draft.stateApp(t.agents).(platform.AgentRuntime)
	run, _ := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), b.Run)
	if b.Evaluation != nil {
		run.Agent = b.Evaluation.Agent
	}
	member := agent.AgentPrincipal(run.Agent)
	if b.Evaluation != nil && b.Evaluation.Candidate != "" {
		member = platform.Member{ID: "app:" + platform.AgentApp, Tenant: t.ID}
	}
	c := platform.NewCaller(draft, member, platform.AgentApp, false, true)
	r := acceptedformat.RuntimeAgent{Version: 1, Kind: "agent-result", Tenant: t.ID, App: platform.AgentApp, Member: member.ID, At: at.UTC(), Step: b}
	if b.Evaluation == nil && b.Usage.Model != "" {
		if r.UsageBefore, err = t.usagePredecessor(); err != nil {
			return err
		}
	}
	refusal := agent.ApplyAgentStepCaller(c, b, at)
	if refusal == nil {
		refusal = draft.Failure
	}
	if refusal == nil {
		refusal = draft.answerRequests(at)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	if refusal != nil || len(draft.Events) == 0 {
		return fmt.Errorf("agent answer has no complete decision: %v", refusal)
	}
	last := draft.Events[len(draft.Events)-1]
	if r.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, at); err != nil {
		return err
	}
	r.Key = "agent:" + last.Record.GetSubmission().GetIdempotencyKey()
	r.RequestHash, err = acceptedformat.AgentRequestHash(r)
	if err == nil {
		r.Digest, err = acceptedformat.DigestAgent(r)
	}
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(r)
	}
	if err == nil {
		_, err = acceptedformat.DecodeAgent(raw)
	}
	if err != nil {
		return err
	}
	principal, _ := json.Marshal(member)
	candidate := preparedAccepted{entry: Entry{App: platform.AgentApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: draft.FlowVersions()}, key: r.Key, hash: r.RequestHash, uploads: draft.tenant.preparedUploads()}
	if t.preparedResult != nil {
		*t.preparedResult = candidate
		return nil
	}
	t.finishAgentStep(candidate)
	return nil
}

func (t *Tenant) finishAgentStep(candidate preparedAccepted) {
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		return
	}
	saved, err := acceptedformat.DecodeAgent(committed)
	if err != nil || saved.Key != candidate.key || saved.RequestHash != candidate.hash {
		t.quarantine(fmt.Errorf("committed agent answer differs: %v", err))
		return
	}
	if _, err = t.applyAcceptedAgent(committed); err != nil {
		t.quarantine(err)
		return
	}
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	t.changedOwner(platform.AgentApp)
}

func (t *Tenant) applyAcceptedAgent(raw []byte) (acceptedformat.RuntimeAgent, error) {
	h := runtimekernel.AgentApply{Usage: t.usage(), Batch: t.batchApply(), Published: t.publishAcceptedBatch}
	if t.agents != nil {
		h.Ledger = t.agents.AcceptedLedger()
	}
	return t.ApplyAgentResult(raw, h)
}

func (t *Tenant) applyAcceptedEffect(raw []byte) (acceptedformat.Effect, bool, error) {
	h := runtimekernel.EffectApply{Changes: t.changeApply(), Finish: t.prepareOperationFinish, Published: t.publishAcceptedChanges,
		Current: func(id string) (acceptedformat.EffectState, bool) {
			current, found := t.outbound.Get(id)
			if !found {
				return acceptedformat.EffectState{}, false
			}
			return acceptedformat.EffectState{Effect: current.Effect, Since: current.Since}, true
		},
		Settle: func(id string, after acceptedformat.EffectState, finished *pb.Work) {
			t.outbound.Update(id, func(x *actions.Queued) { x.Effect, x.Since, x.Sending = after.Effect, after.Since, false })
			if finished != nil {
				t.installAcceptedWork(finished)
			}
		}}
	if t.ai != nil {
		h.Usage, h.Meter = t.ai.Snapshot, t.ai.Meter
	}
	return t.ApplyEffectResult(raw, h)
}

func (t *Tenant) settleAccepted(id string, out platform.Outcome, usage *platform.AIUsage, at time.Time) {
	pending, found := t.outbound.Get(id)
	var before acceptedformat.EffectState
	if found {
		before = acceptedformat.EffectState{Effect: pending.Effect, Since: pending.Since}
	}
	if !found || out.Effect != id {
		return
	}
	if platform.EffectSettled(before.State) && before.State != "discarded" {
		return
	}
	if usage != nil {
		copy := *usage
		copy.Applications = slices.Clone(before.Effect.Applications)
		copy.Run = before.Effect.ID
		if before.Effect.Endpoint == acceptedformat.ModelEndpoint {
			var ask aigateway.ModelAsk
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
	if before.Endpoint == acceptedformat.ModelEndpoint && (out.Result == "rejected" || out.Result == "unknown") && (t.tasks == nil || len((hostView{t: t}).Holding(controlowner.PlatformApp, controlowner.Admin)) == 0) {
		out.PublicMessage = "The model call stopped; no eligible reviewer is configured"
	}
	if out.Detail != "" && out.PublicMessage == "" {
		out.PublicMessage = "The last delivery attempt failed"
	}
	after := before.Effect
	acceptedformat.MarkEffect(&after, before.Since, out, at)
	result := acceptedformat.Effect{Version: 5, Kind: "effect-result", Tenant: t.ID, App: controlowner.PlatformApp,
		Key: acceptedformat.EffectResultKey(before), At: at.UTC(), Before: before,
		After: acceptedformat.EffectState{Effect: after, Since: before.Since}, Outcome: out, Usage: usage}
	failed := func(err error) {
		t.observeOwnerRun(at, "effect", id, before.App, before.Endpoint, "failed", "Effect answer", err.Error())
	}
	var err error
	_, result.WorkBefore, result.Work, err = t.prepareOperationFinish(before.Effect, out)
	if err != nil {
		return
	}
	if usage != nil && t.ai == nil {
		failed(fmt.Errorf("model usage is not metered: the AI module is disabled"))
		usage, result.Usage = nil, nil
	}
	if usage != nil {
		state, snapshotErr := t.ai.Snapshot()
		if snapshotErr == nil {
			result.UsageBefore, snapshotErr = platform.CanonicalDigest(state)
		}
		if snapshotErr != nil {
			failed(snapshotErr)
			return
		}
	}
	if result.RequestHash, err = acceptedformat.EffectResultHash(before, out, result.At); err != nil {
		failed(err)
		return
	}
	draft, err := t.stageEffectResult(&result, before, after, out, usage, at)
	if err != nil {
		if t.quarantined() {
			return
		}
		result.CallbackError, result.After.CallbackError = err.Error(), err.Error()
		result.Rows, result.Member, result.Changes, result.Unique = nil, "", nil, nil
		result.Notices, result.Intents, result.States, result.Control = nil, nil, nil, nil
		draft = t.newStagedDecision()
		draft.At = at
	}
	result.Digest, err = acceptedformat.DigestEffect(result)
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(result)
	}
	if err == nil {
		_, err = acceptedformat.DecodeEffect(raw)
	}
	if err != nil {
		failed(err)
		return
	}
	principal, _ := json.Marshal(t.automation(controlowner.PlatformApp, false).Member)
	candidate := preparedAccepted{entry: Entry{App: controlowner.PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: draft.FlowVersions()}, key: result.Key, hash: result.RequestHash, uploads: draft.tenant.preparedUploads()}
	if t.preparedResult != nil {
		*t.preparedResult = candidate
		return
	}
	t.finishEffect(id, candidate)
}

func (t *Tenant) stageEffectResult(result *acceptedformat.Effect, before acceptedformat.EffectState, after platform.Effect, out platform.Outcome, usage *platform.AIUsage, at time.Time) (*stagedDecision, error) {
	draft := t.newStagedDecision()
	draft.At = at
	if after.State == "failed" && t.App(controlowner.PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + controlowner.PlatformApp, Tenant: t.ID}, controlowner.PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Effect to " + after.Endpoint + " failed: " + after.Event,
			Body: after.Error + ". Retry it in Settings → Automation once the cause is fixed.",
			Key:  "failed:" + after.ID}, at, platform.Recipient{AppRole: controlowner.Admin})
	}
	if (platform.EffectSettled(after.State) || after.State == "unknown") && before.State != "discarded" {
		if err := t.stageEffectAnswer(draft, after, out, usage, at); err != nil {
			if t.quarantined() {
				return draft, err
			}
			result.CallbackError = err.Error()
			result.Authorizations = slices.Clone(draft.Authorizations)
			draft = t.newStagedDecision()
			draft.At = at
		}
	}
	if before.Endpoint == acceptedformat.ModelEndpoint && (after.State == "rejected" || after.State == "failed" || after.State == "unknown") {
		recipients := (hostView{t: t, app: t.App(before.App)}).Holding(controlowner.PlatformApp, controlowner.Admin)
		if len(recipients) > 0 && t.tasks != nil {
			change, _, _ := strings.Cut(before.Key, "#")
			var receipt *pb.ChangeRecord
			for _, saved := range t.App(before.App).AcceptedLedger().RecordsFor(t.ID) {
				if saved.GetChangeId() == change {
					receipt = saved
					break
				}
			}
			if receipt != nil {
				caller := platform.NewCaller(draft, platform.Member{ID: "app:" + before.App, Tenant: t.ID}, before.App, false, true)
				to := make([]platform.Recipient, 0, len(recipients))
				for _, member := range recipients {
					to = append(to, platform.Recipient{Member: member})
				}
				if failure := caller.Assign(receipt, platform.Assignment{Title: "Review the stopped model call", Body: out.Detail, Ref: "platform.effect/" + before.ID, Key: "ai-handoff:" + before.ID, To: to}); failure != nil {
					draft.Failure = failure
				}
			}
		}
	}
	result.Authorizations = append(result.Authorizations, draft.Authorizations...)
	result.After.CallbackError = result.CallbackError
	if draft.Failure != nil {
		return draft, draft.Failure
	}
	if err := draft.EffectDecision(draft.sources(), result, before.App, at); err != nil {
		return draft, err
	}
	_, err := t.prepareAcceptedChanges(result.ChangePlan())
	return draft, err
}

func (t *Tenant) finishEffect(id string, candidate preparedAccepted) {
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		if errors.Is(err, errAcceptedConflict) {
			t.quarantine(fmt.Errorf("effect %s conflicts with a previously committed outcome", id))
			return
		}
		t.outbound.Update(id, func(x *actions.Queued) { x.Sending = false })
		return
	}
	saved, applied, err := t.applyAcceptedEffect(committed)
	if err != nil || saved.Key != candidate.key || saved.RequestHash != candidate.hash || !applied {
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
			err = fmt.Errorf("unsupported effect callback: %v", p)
		}
	}()
	if x.App != "" {
		c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
		if refusal := draft.authorizeNative(c, "effect:"+x.Endpoint, "action", nil, t.App(x.App) != nil); refusal != nil {
			return refusal
		}
	}
	if x.Endpoint == acceptedformat.ModelEndpoint {
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
	if x.Endpoint == acceptedformat.OperationEndpoint {
		draft.OperationAnswers = map[string]platform.OperationResult{x.ID: compute.ResultOf(x)}
		processesOwner := t.procs
		if app, ok := t.procs.(platform.App); ok {
			processesOwner = draft.stateApp(app).(platform.Processes)
		}
		if processes, ok := processesOwner.(platform.OperationCompletion); ok {
			c := platform.NewCaller(draft, platform.Member{ID: "app:" + "flow", Tenant: t.ID}, "flow", false, true)
			if err := processes.OperationEnded(c, x.ID, at); err != nil {
				return err
			}
		}
	}
	c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
	if app, ok := draft.stateApp(t.App(x.App)).(platform.Answerer); ok {
		source, _ := t.outbound.Get(x.ID)
		if source.ID == "" || source.App != x.App {
			return fmt.Errorf("effect callback has no accepted source")
		}
		previous := draft.CallbackOrigin
		refusal := func() *kernel.Error {
			draft.CallbackOrigin = &source.Effect
			defer func() { draft.CallbackOrigin = previous }()
			return app.Answer(c, x, out, at)
		}()
		if refusal != nil {
			return refusal
		}
	}
	if t.agents != nil {
		draft.stateApp(t.agents).(platform.AgentRuntime).AgentEffectEnded(t.automated(c, platform.AgentApp), nil, x, out, at)
	}
	if draft.Failure != nil {
		return draft.Failure
	}
	if err := draft.answerRequests(at); err != nil {
		return err
	}
	if err := draft.stageObservers(); err != nil {
		return err
	}
	return nil
}

func (d *stagedDecision) answerRequests(now time.Time) *kernel.Error {
	for count := 0; len(d.requests) > 0; count++ {
		if count >= acceptedformat.MaxHops {
			unsupportedStagedEffect()
		}
		q := d.requests[0]
		d.requests = d.requests[1:]
		if err := d.answerRequest(q, now); err != nil {
			return err
		}
	}
	return d.Failure
}

func (d *stagedDecision) answerRequest(q request, now time.Time) (refusal *kernel.Error) {
	end := d.tenant.begin("request "+q.Protocol+"#"+q.Action, trace.SpanContext{}, attribute.String("platform.app", q.caller.App), attribute.String("platform.target", q.Target))
	outcome := "ok"
	defer func() {
		if refusal != nil {
			outcome = refusal.Code.String()
		}
		end(outcome)
	}()
	s := q.record.GetSubmission()
	key := fmt.Sprintf("%s:%s#%d", q.caller.App, s.GetIdempotencyKey(), q.n)
	providerKey := key
	if q.Key != "" {
		providerKey = q.Key
	}
	answer := platform.Answer{Tag: q.Tag, Call: q.Target, Action: q.Action, Outcome: "accepted"}
	var ref *pb.EntityRef
	_, err := d.Attempt(func() (*pb.ChangeRecord, *kernel.Error) {
		var record *pb.ChangeRecord
		var refusal *kernel.Error
		ref, record, refusal = d.routingTenant().router().Invoke(q.caller, q.Protocol, q.Action, q.Target,
			platform.Raw(q.Payload), providerKey, s.GetIdempotencyKey(), now, q.Provider)
		return record, refusal
	})
	if err != nil {
		answer.Outcome, answer.Code, answer.Reason = "refused", err.Code.String(), err.Message
		outcome = answer.Code
	} else {
		answer.Ref = ref.GetType() + "/" + ref.GetId()
	}
	payload, errJSON := json.Marshal(answer)
	if errJSON != nil {
		unsupportedStagedEffect()
	}
	c := d.tenant.automated(q.caller, q.caller.App)
	reply := &pb.Submission{TenantId: d.tenant.ID, PrincipalId: c.ID, Authority: s.GetAuthority(), Target: s.GetTarget(),
		Schema: &pb.SchemaRef{Name: q.Reply, Version: 1}, IdempotencyKey: "answer:" + key,
		CorrelationId: s.GetIdempotencyKey(), Payload: payload}
	if _, err := platform.Decide(c, d.tenant.App(c.App), reply, now); err != nil {
		return err
	}
	return d.Failure
}

type request struct {
	platform.Request
	caller platform.Caller
	record *pb.ChangeRecord
	n      int
}
