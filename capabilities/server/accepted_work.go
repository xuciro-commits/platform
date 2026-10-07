package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// A job or delivery is a top-level input in its own right. Its immutable
// result includes the K9 generation, scheduling outcome, business changes and
// notices. No runner/listener executes when this result is recovered.
type acceptedWork struct {
	Version     int               `json:"version"`
	Kind        string            `json:"kind"`
	Tenant      string            `json:"tenant"`
	App         string            `json:"app"`
	Key         string            `json:"key"`
	At          time.Time         `json:"at"`
	RequestHash string            `json:"requestHash"`
	Digest      string            `json:"digest"`
	Input       taskState         `json:"input"`
	PriorWork   json.RawMessage   `json:"priorWork,omitempty"`
	Work        json.RawMessage   `json:"work"`
	Outcome     string            `json:"outcome"`
	After       Task              `json:"after"`
	Changes     json.RawMessage   `json:"changes,omitempty"`
	Notices     *acceptedNotices  `json:"notices,omitempty"`
	Intents     []platform.Effect `json:"intents,omitempty"`
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
	return canonicalDigest([]any{input, prior})
}

func (t *Tenant) acceptWork(task *Task, now time.Time) (outcome string) {
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
	result := acceptedWork{Version: 1, Kind: "work-result", Tenant: t.ID, App: task.App,
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
		len(draft.states) > 0 && len(draft.stateBases) > 0 {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	} else {
		result.Notices = draft.savedNotices()
		result.Intents = slices.Clone(draft.intents)
	}
	if t.procs != nil {
		t.procs.Versions() // saved record images already pin each chosen version
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
	principal, _ := json.Marshal(c.Member)
	committed, err := t.AcceptResult(Entry{App: task.App, Kind: "accepted-result", Principal: principal, Body: raw, At: now},
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
	t.enqueue(saved.At)
	if len(saved.Changes) == 0 && saved.Notices == nil {
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
	if task.Kind == "job" {
		refusal = t.app(task.App).(platform.Runner).Run(c, task.job.Name, now)
	} else {
		t.hops = task.event.hops + 1
		if listener, ok := t.app(task.App).(host.Listener); ok {
			names := append([]string{task.event.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(task.event.Event)...)
			if task.event.plan != nil {
				names = task.event.plan.Names
			}
			refusal = listener.Listen(c, task.event.Event, names, now)
		} else {
			refusal = t.app(task.App).(platform.Subscriber).Handle(c, task.event.Event)
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
	return canonicalDigest(result)
}

func decodeAcceptedWork(raw []byte) (acceptedWork, error) {
	var result acceptedWork
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return result, fmt.Errorf("work result is empty or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("work result has trailing data")
	}
	digest, err := digestAcceptedWork(result)
	hash, hashErr := workInputHash(result.Input, result.PriorWork)
	finished := new(pb.Work)
	workErr := protojson.Unmarshal(result.Work, finished)
	if err != nil || hashErr != nil || result.Digest != digest || result.RequestHash != hash || workErr != nil ||
		result.Version != 1 || result.Kind != "work-result" || result.Tenant == "" || result.App == "" || result.At.IsZero() ||
		result.Input.ID == "" || result.Input.App != result.App || result.Input.Kind != "delivery" && result.Input.Kind != "job" ||
		result.After.ID != result.Input.ID || result.After.App != result.App || result.After.Kind != result.Input.Kind ||
		result.After.Title != result.Input.Title || result.After.Attempts != result.Input.Attempts+1 ||
		!result.After.Last.Equal(result.At) || result.Key != fmt.Sprintf("work:%s:%d", result.Input.ID, result.After.Attempts) ||
		finished.GetWorkId() != result.Input.ID || finished.GetOwnerId() != "host" ||
		int(finished.GetGeneration()) != result.After.Attempts || result.Outcome == "" {
		return result, fmt.Errorf("work result has inconsistent identity, outcome or digest")
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
		changes, _, err := decodeAcceptedBatch(result.Changes)
		if err != nil || changes.Tenant != result.Tenant || !changes.At.Equal(result.At) || result.Notices != nil || len(result.Intents) > 0 ||
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
	if err := t.validateAcceptedNotices(result.Notices); err != nil {
		return false, err
	}
	if err := t.validateAcceptedIntents(result.Intents); err != nil {
		return false, err
	}
	if len(result.Changes) > 0 {
		batch, _, _ := decodeAcceptedBatch(result.Changes)
		owner, ok := t.app(batch.App).(platform.ResultApp)
		if !ok {
			return false, fmt.Errorf("work result needs missing business authority")
		}
		if applied, err := t.applyAcceptedBatch(owner.AcceptedLedger(), result.Changes); err != nil || !applied {
			return false, fmt.Errorf("work result business application: %v", err)
		}
	}
	t.works = works
	t.applyAcceptedNotices(result.Notices)
	t.installAcceptedIntents(result.Intents)
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
	if len(result.Changes) > 0 {
		batch, _, _ := decodeAcceptedBatch(result.Changes)
		t.publishAcceptedBatch(batch)
	}
	return true, nil
}

func (task *Task) restoreOutcome(saved Task) {
	event, job, since := task.event, task.job, task.since
	*task = saved
	task.event, task.job, task.since = event, job, since
}

func (t *Tenant) forkWorkState() *kernel.Works {
	items := t.works.All()
	for i, item := range items {
		items[i] = proto.Clone(item).(*pb.Work)
	}
	draft := kernel.NewWorks()
	draft.Restore(items)
	return draft
}
