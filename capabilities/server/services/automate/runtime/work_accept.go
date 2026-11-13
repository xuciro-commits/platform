package automation

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type WorkDraft struct {
	Decide   func() (*kernel.Error, []acceptedformat.AuthorizationEvidence)
	Reset    func()
	Failed   func(title, why, id string)
	Decision func(*acceptedformat.Work) error
	Commit   func(acceptedformat.Work) string
}

type WorkAcceptance struct {
	Tenant string
	Fork   func() *kernel.Works
	Retry  func(app string) platform.Retry
	Draft  func(task *Task, now time.Time) WorkDraft
	Bind   func(app string) ([]platform.AssetBinding, error)
}

func (a WorkAcceptance) Accept(task *Task, now time.Time) string {
	conflict := pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	input, err := StateOfTask(task)
	if err != nil {
		return conflict
	}
	result := acceptedformat.Work{Version: 3, Kind: "work-result", Tenant: a.Tenant, App: task.App,
		At: now.UTC(), Input: input, Outcome: "ok"}
	works := a.Fork()
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
		return conflict
	}
	draft := a.Draft(task, now)
	refusal, result.Authorizations = draft.Decide()
	if refusal != nil {
		result.Outcome = refusal.Error()
		draft.Reset()
	}
	if err := works.Finish(task.ID, generation, result.Outcome != "ok"); err != nil {
		return err.Error()
	}
	finished, _ := works.Get(task.ID)
	result.Work, _ = protojson.Marshal(finished)
	result.After = task.Settle(int(generation), result.Outcome, a.Retry(task.App), now)
	if result.After.State == "failed" {
		draft.Failed("Delivery to "+task.App+" failed: "+task.Title, result.Outcome, task.ID)
	}
	if err := draft.Decision(&result); err != nil {
		return conflict
	}
	if task.Kind == "job" && result.Outcome == "ok" && len(result.Changes) == 0 &&
		result.Notices == nil && len(result.Intents) == 0 && len(result.States) == 0 && result.Control == nil && len(result.Unique) == 0 {
		return result.Outcome
	}
	if task.Kind == "job" {
		bindings, err := a.Bind(task.App)
		if err != nil {
			return conflict
		}
		result.After.Applications = bindings
	}
	return draft.Commit(result)
}

func (b *WorkBoard) Restorer(delivered func(platform.Delivery)) func(acceptedformat.Work) (platform.ScheduledTaskState, string, func(*pb.Work), error) {
	return func(result acceptedformat.Work) (platform.ScheduledTaskState, string, func(*pb.Work), error) {
		var task *Task
		b.Lock()
		if result.Input.Kind == "job" {
			task = b.Job(result.Input.ID)
		} else {
			task = b.Delivery(result.App, result.Input.ID)
		}
		b.Unlock()
		if task == nil {
			return platform.ScheduledTaskState{}, "", nil, fmt.Errorf("work result needs missing work %s", result.Input.ID)
		}
		before, err := StateOfTask(task)
		if err != nil {
			return before, "", nil, fmt.Errorf("work result predecessor differs")
		}
		return before, task.ID, func(*pb.Work) {
			b.Lock()
			task.RestoreOutcome(Task{ScheduledTask: result.After})
			if task.Kind == "delivery" {
				switch task.State {
				case "done", "failed":
					b.Settled(task)
				}
			}
			b.Unlock()
			if task.Kind == "delivery" {
				s := task.Event.Record.GetSubmission()
				delivered(platform.Delivery{At: result.At, App: task.Event.App, Action: s.GetSchema().GetName(), Target: target(s),
					Subscriber: task.App, Outcome: result.Outcome, Attempt: task.Attempts})
			}
		}, nil
	}
}
