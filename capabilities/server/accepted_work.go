package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/journal"
	"platformserver/platform"
)

// A job or delivery is a top-level input in its own right. Its immutable
// result includes the K9 generation, scheduling outcome, business changes and
// notices. No runner/listener executes when this result is recovered.
type acceptedWork struct {
	Unique         []journal.StateUnique                `json:"unique,omitempty"`
	Authorizations []resultformat.AuthorizationEvidence `json:"authorizations,omitempty"`
	Version        int                                  `json:"version"`
	Kind           string                               `json:"kind"`
	Tenant         string                               `json:"tenant"`
	App            string                               `json:"app"`
	Key            string                               `json:"key"`
	At             time.Time                            `json:"at"`
	RequestHash    string                               `json:"requestHash"`
	Digest         string                               `json:"digest"`
	Input          taskState                            `json:"input"`
	PriorWork      json.RawMessage                      `json:"priorWork,omitempty"`
	Work           json.RawMessage                      `json:"work"`
	Outcome        string                               `json:"outcome"`
	After          Task                                 `json:"after"`
	Changes        json.RawMessage                      `json:"changes,omitempty"`
	Notices        *resultformat.Notices                `json:"notices,omitempty"`
	Intents        []platform.Effect                    `json:"intents,omitempty"`
	States         []resultformat.State                 `json:"states,omitempty"`
	Control        *resultformat.Control                `json:"control,omitempty"`
}

func stateOfTask(task *Task) (taskState, error) {
	s := taskState{Task: *task, Since: task.since}
	if task.event != nil {
		s.EventApp, s.Hops, s.Changed, s.Plan = task.event.App, task.event.hops, slices.Clone(task.event.Changed), task.event.plan
		var err error
		s.Event, err = platform.Protos([]*pb.ChangeRecord{task.event.Record})
		if err != nil {
			return s, err
		}
	}
	return s, nil
}

func workInputHash(input taskState, prior json.RawMessage) (string, error) {
	return acceptance.Digest([]any{input, prior})
}

func (t *Tenant) acceptWork(task *Task, now time.Time) (outcome string) {
	if task.event != nil {
		end := t.begin("deliver "+task.event.Record.GetSubmission().GetSchema().GetName()+" to "+task.App, task.event.span,
			attribute.String("platform.app", task.App), attribute.String("platform.target", target(task.event.Record.GetSubmission())))
		defer func() { end(outcome); counted(t, "delivery", task.App, outcome) }()
	}
	// Everything below is under the top-level tenant commit lock.
	defer func() {
		if p := recover(); p != nil {
			t.hops = 0
			t.quarantine(fmt.Errorf("accepted work: %v", p))
			outcome = pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	}()
	input, err := stateOfTask(task)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	result := acceptedWork{Version: 3, Kind: "work-result", Tenant: t.ID, App: task.App,
		At: now.UTC(), Input: input, Outcome: "ok"}
	works := t.forkWorkState()
	if prior, err := works.Get(task.ID); err == nil {
		result.PriorWork, _ = protojson.Marshal(prior)
	}
	generation, _, refusal := works.Start(task.ID, "host")
	if refusal != nil {
		return refusal.Error()
	}
	result.Key = fmt.Sprintf("work:%s:%d", task.ID, generation)
	result.RequestHash, err = workInputHash(input, result.PriorWork)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	draft := t.newStagedDecision()
	draft.at = now
	if task.event != nil {
		draft.hops = task.event.hops + 1
	}
	c := platform.NewCaller(draft, platform.Member{ID: "app:" + task.App, Tenant: t.ID}, task.App, false, true)
	refusal = decideWork(t, draft, c, task, now)
	result.Authorizations = slices.Clone(draft.authorizations)
	if refusal != nil {
		result.Outcome = refusal.Error()
		// A failed attempt may advance its K9 retry state, never a half-applied
		// business decision made by the listener before returning its refusal.
		draft = t.newStagedDecision()
		draft.at = now
	}
	if err := works.Finish(task.ID, generation, result.Outcome != "ok"); err != nil {
		return err.Error()
	}
	finished, _ := works.Get(task.ID)
	result.Work, _ = protojson.Marshal(finished)
	result.After = *task
	result.After.Attempts, result.After.Last, result.After.Error = int(generation), now, ""
	if task.Kind == "job" {
		result.After.Due = now.Add(task.job.Every)
		if result.Outcome != "ok" {
			result.After.Error = result.Outcome
		}
	} else {
		retry := t.retryOf(task.App)
		switch {
		case result.Outcome == "ok":
			result.After.State = "done"
		case int(generation)-task.since >= retry.Attempts:
			result.After.State, result.After.Error = "failed", result.Outcome
		default:
			result.After.State, result.After.Error = "retrying", result.Outcome
			result.After.Due = now.Add(retry.After(int(generation) - task.since))
		}
	}
	if result.After.State == "failed" && t.app(PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + PlatformApp, Tenant: t.ID}, PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Delivery to " + task.App + " failed: " + task.Title,
			Body: result.Outcome + ". Retry it in Settings → Automation once the cause is fixed.", Key: "failed:" + task.ID},
			now, platform.Recipient{AppRole: Admin})
	}
	if len(draft.events) > 0 {
		last := draft.events[len(draft.events)-1]
		result.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, now)
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	} else if len(draft.records.writes) > 0 || len(draft.deliveries) > 0 || draft.failure != nil ||
		len(draft.observations) > 0 || len(draft.allocated) > 0 || len(draft.logs) > 0 || len(draft.operationCancels) > 0 || len(draft.requests) > 0 {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	} else {
		var plan resultformat.ChangePlan
		plan, err = draft.savedChangePlan()
		result.Notices, result.Intents = plan.Notices, plan.Intents
		result.States, result.Control = plan.States, plan.Control
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	}
	result.Unique = draft.savedUniqueChanges()
	if task.Kind == "job" && result.Outcome == "ok" && len(result.Changes) == 0 &&
		result.Notices == nil && len(result.Intents) == 0 && len(result.States) == 0 && result.Control == nil && len(result.Unique) == 0 {
		// Polling without a business result is not an accepted K9 generation.
		// Leave the journaled predecessor intact for the next real result.
		return result.Outcome
	}
	if task.Kind == "job" {
		bindings, err := t.bindApplications(platform.AssetRef{App: task.App, Kind: platform.AssetApp, Name: task.App}, "", "", task.App)
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
		result.After.Applications = bindings
	}
	result.Digest, err = digestAcceptedWork(result)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := decodeAcceptedWork(raw); err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := t.prepareAcceptedChanges(result.ChangePlan()); err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	principal, _ := json.Marshal(c.Member)
	committed, err := t.acceptResult(Entry{App: task.App, Kind: "accepted-result", Principal: principal, Body: raw, At: now, Versions: draft.flowVersions()},
		result.Key, result.RequestHash)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	saved, err := decodeAcceptedWork(committed)
	if err != nil || saved.Tenant != t.ID || saved.App != task.App || saved.Key != result.Key || saved.RequestHash != result.RequestHash {
		t.quarantine(fmt.Errorf("committed work differs from input: %v", err))
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := t.applyAcceptedWork(committed); err != nil {
		t.quarantine(err)
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	if len(saved.Changes) == 0 && len(saved.States) == 0 && saved.Notices == nil {
		t.operationsChanged(saved.App)
	} else {
		t.changedOwner(saved.App)
	}
	return saved.Outcome
}

func decideWork(t *Tenant, d *stagedDecision, c platform.Caller, task *Task, now time.Time) (refusal *kernel.Error) {
	defer func() {
		t.hops = 0
		if p := recover(); p != nil {
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				panic(p)
			}
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action uses an effect outside the accepted-result boundary")
		}
	}()
	owner := t.app(task.App)
	if owner == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", "verb", "The operation is unavailable in its owner declaration")
	}
	permission, declared := "job:"+task.job.Name, false
	if task.Kind == "job" {
		declared = slices.ContainsFunc(owner.Manifest().Jobs, func(job platform.Job) bool { return job.Name == task.job.Name })
	} else if task.event != nil {
		permission = "delivery:" + task.event.Record.GetSubmission().GetSchema().GetName()
		declared = len(owner.Manifest().Subscribes) > 0 || slices.Contains(t.listeners, task.App)
	}
	if refusal := d.authorizeNative(c, permission, "action", owner.Manifest().AllRoles(), declared); refusal != nil {
		return refusal
	}
	if task.Kind == "job" {
		refusal = d.stateApp(t.app(task.App)).(platform.Runner).Run(c, task.job.Name, now)
	} else {
		t.hops = task.event.hops + 1
		if listener, ok := d.stateApp(t.app(task.App)).(host.Listener); ok {
			names := append([]string{task.event.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(task.event.Event)...)
			if task.event.plan != nil {
				names = task.event.plan.Names
			}
			refusal = listener.Listen(c, task.event.Event, names, now)
		} else {
			refusal = d.stateApp(t.app(task.App)).(platform.Subscriber).Handle(c, task.event.Event)
		}
	}
	if refusal == nil {
		refusal = d.failure
	}
	if refusal == nil {
		refusal = d.answerRequests(now)
	}
	if refusal == nil {
		refusal = d.stageObservers()
	}
	return refusal
}

func digestAcceptedWork(result acceptedWork) (string, error) {
	result.Digest = ""
	return acceptance.Digest(result)
}

func decodeAcceptedWork(raw []byte) (acceptedWork, error) {
	var result acceptedWork
	if len(raw) == 0 || len(raw) > acceptance.MaxResultBytes {
		return result, fmt.Errorf("work result is empty or too large")
	}
	if err := acceptance.DecodeJSON(raw, &result, "work result"); err != nil {
		return result, err
	}
	if err := resultformat.ValidateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, err
	}

	if result.Version < 3 && len(result.Unique) > 0 {
		return result, fmt.Errorf("legacy work result carries derived unique keys")
	}
	digest, err := digestAcceptedWork(result)
	hash, hashErr := workInputHash(result.Input, result.PriorWork)
	finished := new(pb.Work)
	workErr := protojson.Unmarshal(result.Work, finished)
	if err != nil || hashErr != nil || result.Digest != digest || result.RequestHash != hash || workErr != nil ||
		(result.Version != 1 && result.Version != 2 && result.Version != 3) || result.Version == 1 && (result.Control != nil || len(result.States) > 0) || result.Kind != "work-result" || result.Tenant == "" || result.App == "" || result.At.IsZero() ||
		result.Input.ID == "" || result.Input.App != result.App || result.Input.Kind != "delivery" && result.Input.Kind != "job" ||
		result.After.ID != result.Input.ID || result.After.App != result.App || result.After.Kind != result.Input.Kind ||
		result.After.Title != result.Input.Title || result.After.Attempts != result.Input.Attempts+1 ||
		!result.After.Last.Equal(result.At) || result.Key != fmt.Sprintf("work:%s:%d", result.Input.ID, result.After.Attempts) ||
		finished.GetWorkId() != result.Input.ID || finished.GetOwnerId() != "host" ||
		int(finished.GetGeneration()) != result.After.Attempts || result.Outcome == "" {
		return result, fmt.Errorf("work result has inconsistent identity, outcome or digest")
	}
	if err := checkApplicationBindings(result.After.Applications); err != nil {
		return result, err
	}
	if result.Input.Since < 0 || result.Input.Since > result.Input.Attempts {
		return result, fmt.Errorf("work result has an invalid retry base")
	}
	if len(result.PriorWork) > 0 {
		prior := new(pb.Work)
		if protojson.Unmarshal(result.PriorWork, prior) != nil || prior.GetWorkId() != result.Input.ID ||
			int(prior.GetGeneration()) != result.Input.Attempts || prior.GetState() == pb.WorkState_WORK_STATE_RUNNING {
			return result, fmt.Errorf("work result has an invalid prior generation")
		}
	} else if result.Input.Attempts != 0 {
		return result, fmt.Errorf("work result omits its prior generation")
	}
	if result.Input.Kind == "job" {
		if result.Input.State != "scheduled" || result.After.State != "scheduled" || !result.After.Due.After(result.At) ||
			result.Outcome == "ok" && result.After.Error != "" ||
			result.Outcome != "ok" && result.After.Error != result.Outcome {
			return result, fmt.Errorf("work result has an invalid job outcome")
		}
	} else {
		if result.Input.State != "queued" && result.Input.State != "retrying" ||
			result.Outcome == "ok" && (result.After.State != "done" || result.After.Error != "") ||
			result.Outcome != "ok" && (result.After.State != "failed" && result.After.State != "retrying" || result.After.Error != result.Outcome) ||
			result.After.State == "retrying" && !result.After.Due.After(result.At) ||
			result.After.State != "retrying" && !result.After.Due.Equal(result.Input.Due) {
			return result, fmt.Errorf("work result has an invalid delivery outcome")
		}
	}
	if len(result.Changes) > 0 {
		changes, _, err := resultformat.DecodeBatch(result.Changes)
		if err != nil || changes.Tenant != result.Tenant || !changes.At.Equal(result.At) || result.Notices != nil || len(result.Intents) > 0 || len(result.States) > 0 || result.Control != nil ||
			result.Outcome != "ok" {
			return result, fmt.Errorf("work result has invalid business changes")
		}
	}
	return result, nil
}

func (t *Tenant) applyAcceptedWork(raw []byte) (bool, error) {
	result, err := decodeAcceptedWork(raw)
	if err != nil || result.Tenant != t.ID || t.app(result.App) == nil {
		return false, fmt.Errorf("work result cannot be applied: %v", err)
	}
	finished := new(pb.Work)
	_ = protojson.Unmarshal(result.Work, finished)
	previous, previousErr := t.works.Get(result.Input.ID)
	if previousErr == nil && previous.GetGeneration() >= finished.GetGeneration() {
		if previous.GetGeneration() == finished.GetGeneration() && !proto.Equal(previous, finished) {
			return false, fmt.Errorf("work result generation differs")
		}
		return false, nil
	}
	var task *Task
	t.opsMu.Lock()
	if result.Input.Kind == "job" {
		task = t.work.job(result.Input.ID)
	} else {
		task = t.work.delivery(result.App, result.Input.ID)
	}
	t.opsMu.Unlock()
	if task == nil {
		return false, fmt.Errorf("work result needs missing work %s", result.Input.ID)
	}
	before, err := stateOfTask(task)
	var prior json.RawMessage
	if previousErr == nil {
		prior, _ = protojson.Marshal(previous)
	}
	hash, hashErr := workInputHash(before, prior)
	if err != nil || hashErr != nil || hash != result.RequestHash {
		return false, fmt.Errorf("work result predecessor differs")
	}
	// Kernel validation and notification validation precede every promotion.
	works := t.forkWorkState()
	generation, _, refusal := works.Start(task.ID, "host")
	if refusal != nil || int(generation) != result.After.Attempts ||
		works.Finish(task.ID, generation, result.Outcome != "ok") != nil {
		return false, fmt.Errorf("work result K9 generation differs")
	}
	expected, _ := works.Get(task.ID)
	if !proto.Equal(expected, finished) {
		return false, fmt.Errorf("work result K9 state differs")
	}
	prepared, err := t.prepareAcceptedChanges(result.ChangePlan())
	if err != nil {
		return false, err
	}
	if err := prepared.apply(t); err != nil {
		return false, err
	}
	t.installAcceptedWork(finished)
	t.opsMu.Lock()
	task.restoreOutcome(result.After)
	if task.Kind == "delivery" {
		switch task.State {
		case "done", "failed":
			t.work.settled(task)
		}
	}
	t.opsMu.Unlock()
	if task.Kind == "delivery" {
		s := task.event.Record.GetSubmission()
		t.delivered(Delivery{At: result.At, App: task.event.App, Action: s.GetSchema().GetName(), Target: target(s),
			Subscriber: task.App, Outcome: result.Outcome, Attempt: task.Attempts})
	}
	prepared.publish(t)
	return true, nil
}

func (task *Task) restoreOutcome(saved Task) {
	event, job, since := task.event, task.job, task.since
	*task = saved
	task.event, task.job, task.since = event, job, since
}

func (t *Tenant) forkWorkState() platform.Works {
	items := t.works.All()
	for i, item := range items {
		items[i] = proto.Clone(item).(*pb.Work)
	}
	draft := t.services.Kernel.Works()
	draft.Restore(items)
	return draft
}

// Completing one accepted input must not restore its earlier full K9 fork:
// its child decision may already have cancelled another running generation.
func (t *Tenant) installAcceptedWork(finished *pb.Work) {
	all := t.works.All()
	found := false
	for i, work := range all {
		if work.GetWorkId() == finished.GetWorkId() {
			all[i], found = finished, true
			break
		}
	}
	if !found {
		all = append(all, finished)
	}
	t.works.Restore(all)
}

// An agent answer includes the tool decision, resulting run/evaluation record
// and measured spend in one accepted result; recovery never invokes the tool.
type acceptedAgent struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	Tenant      string          `json:"tenant"`
	App         string          `json:"app"`
	Member      string          `json:"member"`
	Key         string          `json:"key"`
	At          time.Time       `json:"at"`
	RequestHash string          `json:"requestHash"`
	Digest      string          `json:"digest"`
	Step        stepBody        `json:"step"`
	UsageBefore string          `json:"usageBefore,omitempty"`
	Changes     json.RawMessage `json:"changes"`
}

func digestAgent(r acceptedAgent) (string, error) { r.Digest = ""; return acceptance.Digest(r) }
func agentHash(r acceptedAgent) (string, error) {
	return acceptance.Digest([]any{r.Tenant, r.Member, r.Step, r.UsageBefore})
}
func decodeAcceptedAgent(raw []byte) (acceptedAgent, error) {
	var r acceptedAgent
	if len(raw) == 0 || len(raw) > acceptance.MaxResultBytes {
		return r, fmt.Errorf("agent result is empty or too large")
	}
	if err := acceptance.DecodeJSON(raw, &r, "agent result"); err != nil {
		return r, err
	}
	hash, err := agentHash(r)
	digest, digestErr := digestAgent(r)
	batch, receipt, batchErr := resultformat.DecodeBatch(r.Changes)
	if err != nil || digestErr != nil || batchErr != nil || r.Version != 1 || r.Kind != "agent-result" || r.Tenant == "" || r.App != AgentApp || r.Member == "" || r.At.IsZero() || r.Step.Run == "" || r.RequestHash != hash || r.Digest != digest ||
		batch.Tenant != r.Tenant || batch.App != AgentApp || !batch.At.Equal(r.At) || receipt.GetSubmission().GetPrincipalId() != r.Member || r.Key != "agent:"+receipt.GetSubmission().GetIdempotencyKey() {
		return r, fmt.Errorf("agent result has inconsistent identity or changes")
	}
	if r.Step.Evaluation == nil && r.Step.Usage.Model != "" {
		if !resultformat.ValidUsage(r.Step.Usage) || r.UsageBefore == "" || r.Step.Usage.Member != r.Member {
			return r, fmt.Errorf("agent result has invalid usage")
		}
	} else if r.UsageBefore != "" {
		return r, fmt.Errorf("agent result has unexpected usage")
	}
	return r, nil
}

func (t *Tenant) acceptAgentStep(b stepBody, at time.Time) {
	defer func() {
		if p := recover(); p != nil {
			t.quarantine(fmt.Errorf("agent answer cannot stage: %v", p))
		}
	}()
	draft := t.newStagedDecision()
	draft.at = at
	agent := draft.stateApp(t.agents).(AgentRuntime)
	// Authorization and source fences have been rechecked by agentStep while
	// holding the tenant lock, after the outside model call has completed.
	run, _ := platform.Get[AgentRunRecord](t.automation(AgentApp, false), b.Run)
	if b.Evaluation != nil {
		run.Agent = b.Evaluation.Agent
	}
	member := agent.AgentPrincipal(run.Agent)
	if b.Evaluation != nil && b.Evaluation.Candidate != "" {
		// A candidate's agent need not be installed as a principal yet. Only
		// its original owner records the dry report; it executes no AI tool.
		member = platform.Member{ID: "app:" + AgentApp, Tenant: t.ID}
	}
	c := platform.NewCaller(draft, member, AgentApp, false, true)
	r := acceptedAgent{Version: 1, Kind: "agent-result", Tenant: t.ID, App: AgentApp, Member: member.ID, At: at.UTC(), Step: b}
	var err error
	if b.Evaluation == nil && b.Usage.Model != "" {
		r.UsageBefore, err = t.usagePredecessor()
		if err != nil {
			t.quarantine(err)
			return
		}
	}
	refusal := agent.ApplyAgentStepCaller(c, b, at)
	if refusal == nil {
		refusal = draft.failure
	}
	if refusal == nil {
		refusal = draft.answerRequests(at)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	if refusal != nil || len(draft.events) == 0 {
		t.quarantine(fmt.Errorf("agent answer has no complete decision: %v", refusal))
		return
	}
	last := draft.events[len(draft.events)-1]
	r.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, at)
	if err != nil {
		t.quarantine(err)
		return
	}
	r.Key = "agent:" + last.Record.GetSubmission().GetIdempotencyKey()
	r.RequestHash, err = agentHash(r)
	if err == nil {
		r.Digest, err = digestAgent(r)
	}
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(r)
	}
	if err == nil {
		_, err = decodeAcceptedAgent(raw)
	}
	if err != nil {
		t.quarantine(err)
		return
	}
	principal, _ := json.Marshal(member)
	committed, err := t.acceptResult(Entry{App: AgentApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: draft.flowVersions()}, r.Key, r.RequestHash)
	if err != nil {
		return
	}
	saved, err := decodeAcceptedAgent(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed agent answer differs: %v", err))
		return
	}
	if _, err = t.applyAcceptedAgent(committed); err != nil {
		t.quarantine(err)
		return
	}
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	t.changedOwner(AgentApp)
}

func (t *Tenant) applyAcceptedAgent(raw []byte) (acceptedAgent, error) {
	r, err := decodeAcceptedAgent(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != t.ID || t.agents == nil {
		return r, fmt.Errorf("agent result belongs to another tenant")
	}
	if r.UsageBefore != "" {
		before, err := t.usagePredecessor()
		if err != nil || before != r.UsageBefore {
			return r, fmt.Errorf("agent usage predecessor differs: %v", err)
		}
	}
	batch, _, _ := resultformat.DecodeBatch(r.Changes)
	if applied, err := t.applyAcceptedBatch(t.agents.AcceptedLedger(), r.Changes); err != nil || !applied {
		return r, fmt.Errorf("agent result application: %v (applied=%t)", err, applied)
	}
	if r.UsageBefore != "" {
		t.ai.Meter(r.Step.Usage)
	}
	t.publishAcceptedBatch(batch)
	return r, nil
}

// A direct model call has no owned effect. Its immutable usage observation is
// accepted before changing the meter, using the same predecessor as effects.

func (t *Tenant) usagePredecessor() (string, error) {
	if t.ai == nil {
		return "", fmt.Errorf("usage needs an AI meter")
	}
	raw, err := t.ai.Snapshot()
	if err != nil {
		return "", err
	}
	return acceptance.Digest(raw)
}

func (t *Tenant) acceptUsage(m platform.Member, u platform.AIUsage) *kernel.Error {
	// Usage observes a completed call. Its principal is the meter owner; a
	// later revocation of the attributed caller cannot erase an incurred cost.
	c := platform.NewCaller(runtime{t}, platform.Member{ID: "app:" + platform.AIApp, Tenant: m.Tenant}, platform.AIApp, false, true)
	decision := t.authorizeNative(c, "usage:observe", "derive", nil, m.ID != "" && m.ID == u.Member && resultformat.ValidUsage(u))
	if refusal := authorizationRefusal(decision); refusal != nil {
		return refusal
	}
	before, err := t.usagePredecessor()
	if err != nil || m.Tenant != t.ID || m.ID != u.Member || !resultformat.ValidUsage(u) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	r := resultformat.Usage{Version: 1, Kind: "usage-result", Tenant: t.ID, App: platform.AIApp, Member: m.ID, At: u.At.UTC(), Before: before, Usage: u}
	r.Authorizations = []resultformat.AuthorizationEvidence{{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Action: "usage:observe"}, Decision: decision, Roles: slices.Clone(c.RolesHere())}}
	r.RequestHash, err = resultformat.UsageHash(before, u)
	if err != nil {
		return unknown()
	}
	r.Key = "usage:" + r.RequestHash
	r.Digest, err = resultformat.DigestUsage(r)
	if err != nil {
		return unknown()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return unknown()
	}
	if _, err := resultformat.DecodeUsage(raw); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	principal, _ := json.Marshal(m)
	committed, err := t.acceptResult(Entry{App: platform.AIApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage was not accepted")
	}
	saved, err := resultformat.DecodeUsage(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed model usage differs: %v", err))
		return unknown()
	}
	if _, err := t.applyAcceptedUsage(committed); err != nil {
		t.quarantine(err)
		return unknown()
	}
	t.acceptedResultApplied()
	t.operationsChanged(platform.AIApp)
	return nil
}

func (t *Tenant) applyAcceptedUsage(raw []byte) (resultformat.Usage, error) {
	r, err := resultformat.DecodeUsage(raw)
	if err != nil {
		return r, err
	}
	before, err := t.usagePredecessor()
	if err != nil || r.Tenant != t.ID || before != r.Before {
		return r, fmt.Errorf("model usage predecessor differs: %v", err)
	}
	t.ai.Meter(r.Usage)
	return r, nil
}
