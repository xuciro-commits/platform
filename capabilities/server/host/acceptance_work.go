package host

import (
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"
)

func (t *Tenant) usagePredecessor() (string, error) {
	if t.ai == nil {
		return "", fmt.Errorf("usage needs an AI meter")
	}
	raw, err := t.ai.Snapshot()
	if err != nil {
		return "", err
	}
	return platform.CanonicalDigest(raw)
}

func (t *Tenant) acceptUsage(m platform.Member, u platform.AIUsage) *kernel.Error {
	if u.Call != "" {
		for _, prior := range t.ai.Usage() {
			if prior.Call != u.Call {
				continue
			}
			a, _ := platform.CanonicalDigest(prior)
			b, _ := platform.CanonicalDigest(u)
			if a != b {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The model usage differs from its accepted reading")
			}
			return nil
		}
	}
	c := platform.NewCaller(runtime{t: t, scoped: true}, platform.Member{ID: "app:" + platform.AIApp, Tenant: m.Tenant}, platform.AIApp, false, true)
	decision := t.authorizeNative(c, "usage:observe", "derive", nil, m.Tenant == t.ID && m.ID != "" && m.ID == u.Member && acceptedformat.ValidUsage(u))
	if refusal := authorizationRefusal(decision); refusal != nil {
		return refusal
	}
	before, err := t.usagePredecessor()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	r := acceptedformat.Usage{Version: 1, Kind: "usage-result", Tenant: t.ID, App: platform.AIApp, Member: m.ID, At: u.At.UTC(), Before: before, Usage: u}
	r.Authorizations = []acceptedformat.AuthorizationEvidence{{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Action: "usage:observe"}, Decision: decision, Roles: slices.Clone(c.RolesHere())}}
	r.RequestHash, err = acceptedformat.UsageHash(before, u)
	if err != nil {
		return unknown()
	}
	r.Key = "usage:" + r.RequestHash
	r.Digest, err = acceptedformat.DigestUsage(r)
	if err != nil {
		return unknown()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return unknown()
	}
	if _, err := acceptedformat.DecodeUsage(raw); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	principal, _ := json.Marshal(m)
	candidate := preparedAccepted{entry: Entry{App: platform.AIApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, key: r.Key, hash: r.RequestHash}
	if t.preparedResult != nil {
		*t.preparedResult = candidate
		return nil
	}
	return t.finishUsage(candidate)
}

func (t *Tenant) finishUsage(candidate preparedAccepted) *kernel.Error {
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage was not accepted")
	}
	saved, err := acceptedformat.DecodeUsage(committed)
	if err != nil || saved.Key != candidate.key || saved.RequestHash != candidate.hash {
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

func (t *Tenant) applyAcceptedUsage(raw []byte) (acceptedformat.Usage, error) {
	r, err := acceptedformat.DecodeUsage(raw)
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

func (t *Tenant) acceptWork(task *automation.Task, now time.Time) (outcome string) {
	if task.Event != nil {
		end := t.begin("deliver "+task.Event.Record.GetSubmission().GetSchema().GetName()+" to "+task.App, task.Event.Span,
			attribute.String("platform.app", task.App), attribute.String("platform.target", target(task.Event.Record.GetSubmission())))
		defer func() { end(outcome); counted(t, "delivery", task.App, outcome) }()
	}
	defer func() {
		if p := recover(); p != nil {
			t.hops = 0
			t.quarantine(fmt.Errorf("accepted work: %v", p))
			outcome = pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	}()
	input, err := automation.StateOfTask(task)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	result := acceptedformat.Work{Version: 3, Kind: "work-result", Tenant: t.ID, App: task.App,
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
	result.RequestHash, err = acceptedformat.WorkInputHash(input, result.PriorWork)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	draft := t.newStagedDecision()
	draft.at = now
	if task.Event != nil {
		draft.hops = task.Event.Hops + 1
	}
	c := platform.NewCaller(draft, platform.Member{ID: "app:" + task.App, Tenant: t.ID}, task.App, false, true)
	refusal = decideWork(t, draft, c, task, now)
	result.Authorizations = slices.Clone(draft.authorizations)
	if refusal != nil {
		result.Outcome = refusal.Error()
		draft = t.newStagedDecision()
		draft.at = now
	}
	if err := works.Finish(task.ID, generation, result.Outcome != "ok"); err != nil {
		return err.Error()
	}
	finished, _ := works.Get(task.ID)
	result.Work, _ = protojson.Marshal(finished)
	result.After = task.ScheduledTask
	result.After.Attempts, result.After.Last, result.After.Error = int(generation), now, ""
	if task.Kind == "job" {
		result.After.Due = now.Add(task.Job.Every)
		if result.Outcome != "ok" {
			result.After.Error = result.Outcome
		}
	} else {
		retry := t.retryOf(task.App)
		switch {
		case result.Outcome == "ok":
			result.After.State = "done"
		case int(generation)-task.Since >= retry.Attempts:
			result.After.State, result.After.Error = "failed", result.Outcome
		default:
			result.After.State, result.After.Error = "retrying", result.Outcome
			result.After.Due = now.Add(retry.After(int(generation) - task.Since))
		}
	}
	if result.After.State == "failed" && t.App(controlowner.PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + controlowner.PlatformApp, Tenant: t.ID}, controlowner.PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Delivery to " + task.App + " failed: " + task.Title,
			Body: result.Outcome + ". Retry it in Settings → Automation once the cause is fixed.", Key: "failed:" + task.ID},
			now, platform.Recipient{AppRole: controlowner.Admin})
	}
	if len(draft.events) > 0 {
		last := draft.events[len(draft.events)-1]
		result.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, now)
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	} else if draft.records.WriteCount() > 0 || draft.failure != nil ||
		len(draft.observations) > 0 || len(draft.allocated) > 0 || len(draft.logs) > 0 || len(draft.operationCancels) > 0 || len(draft.requests) > 0 {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	} else {
		var plan acceptedformat.ChangePlan
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
		return result.Outcome
	}
	if task.Kind == "job" {
		bindings, err := t.bindApplications(platform.AssetRef{App: task.App, Kind: platform.AssetApp, Name: task.App}, "", "", task.App)
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
		result.After.Applications = bindings
	}
	result.Digest, err = acceptedformat.DigestWork(result)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := acceptedformat.DecodeWork(raw); err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := t.prepareAcceptedChanges(result.ChangePlan()); err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	principal, _ := json.Marshal(c.Member)
	entry := Entry{App: task.App, Kind: "accepted-result", Principal: principal, Body: raw, At: now, Versions: draft.flowVersions()}
	if t.preparedResult != nil {
		*t.preparedResult = preparedAccepted{entry: entry, key: result.Key, hash: result.RequestHash, uploads: t.preparedResult.uploads}
		return result.Outcome
	}
	committed, err := t.acceptResult(entry, result.Key, result.RequestHash)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	return t.finishCommittedWork(task.App, result.Key, result.RequestHash, committed)
}

func (t *Tenant) finishCommittedWork(app, key, hash string, committed []byte) string {
	saved, err := acceptedformat.DecodeWork(committed)
	if err != nil || saved.Tenant != t.ID || saved.App != app || saved.Key != key || saved.RequestHash != hash {
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

func decideWork(t *Tenant, d *stagedDecision, c platform.Caller, task *automation.Task, now time.Time) (refusal *kernel.Error) {
	defer func() {
		t.hops = 0
		if p := recover(); p != nil {
			refusal = platform.RefuseOutsideAcceptedBoundary()
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The owner failed while handling this work: {reason}", fmt.Sprint(p))
			}
		}
	}()
	owner := t.App(task.App)
	if owner == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", "verb", "The operation is unavailable in its owner declaration")
	}
	permission, declared := "job:"+task.Job.Name, false
	if task.Kind == "job" {
		declared = slices.ContainsFunc(owner.Manifest().Jobs, func(job platform.Job) bool { return job.Name == task.Job.Name })
	} else if task.Event != nil {
		permission = "delivery:" + task.Event.Record.GetSubmission().GetSchema().GetName()
		declared = len(owner.Manifest().Subscribes) > 0 || slices.Contains(t.listeners, task.App)
	}
	if refusal := d.authorizeNative(c, permission, "action", owner.Manifest().AllRoles(), declared); refusal != nil {
		return refusal
	}
	if task.Kind == "job" {
		refusal = d.stateApp(t.App(task.App)).(platform.Runner).Run(c, task.Job.Name, now)
	} else {
		t.hops = task.Event.Hops + 1
		if listener, ok := d.stateApp(t.App(task.App)).(platform.Listener); ok {
			names := append([]string{task.Event.Record.GetSubmission().GetSchema().GetName()}, t.router().Events(task.Event.Event)...)
			if task.Event.Plan != nil {
				names = task.Event.Plan.Names
			}
			refusal = listener.Listen(c, task.Event.Event, names, now)
		} else {
			refusal = d.stateApp(t.App(task.App)).(platform.Subscriber).Handle(c, task.Event.Event)
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

func (t *Tenant) applyAcceptedWork(raw []byte) (bool, error) {
	return t.ApplyWorkResult(raw, runtimekernel.WorkApply{Changes: t.changeApply(), Works: t.works, App: t.App, Published: t.publishAcceptedChanges,
		Task: func(result acceptedformat.Work) (platform.ScheduledTaskState, string, func(*pb.Work), error) {
			var task *automation.Task
			t.work.Lock()
			if result.Input.Kind == "job" {
				task = t.work.Job(result.Input.ID)
			} else {
				task = t.work.Delivery(result.App, result.Input.ID)
			}
			t.work.Unlock()
			if task == nil {
				return platform.ScheduledTaskState{}, "", nil, fmt.Errorf("work result needs missing work %s", result.Input.ID)
			}
			before, err := automation.StateOfTask(task)
			if err != nil {
				return before, "", nil, fmt.Errorf("work result predecessor differs")
			}
			return before, task.ID, func(*pb.Work) {
				t.work.Lock()
				task.RestoreOutcome(automation.Task{ScheduledTask: result.After})
				if task.Kind == "delivery" {
					switch task.State {
					case "done", "failed":
						t.work.Settled(task)
					}
				}
				t.work.Unlock()
				if task.Kind == "delivery" {
					s := task.Event.Record.GetSubmission()
					t.delivered(platform.Delivery{At: result.At, App: task.Event.App, Action: s.GetSchema().GetName(), Target: target(s),
						Subscriber: task.App, Outcome: result.Outcome, Attempt: task.Attempts})
				}
			}, nil
		}})
}

func (t *Tenant) forkWorkState() *kernel.Works { return runtimekernel.ForkWorks(t.works) }

func (t *Tenant) installAcceptedWork(finished *pb.Work) { runtimekernel.InstallWork(t.works, finished) }
