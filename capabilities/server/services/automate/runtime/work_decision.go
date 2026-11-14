package automation

import (
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type WorkDecision struct {
	Owner     func(app string) platform.App
	Listens   func(app string) bool
	Authorize func(permission string, roles []string, declared bool) *kernel.Error
	State     func() platform.App
	Routed    func(platform.Event) []string
	Hops      func(int)
	Finish    func() *kernel.Error
}

func (w WorkDecision) Decide(c platform.Caller, task *Task, now time.Time) *kernel.Error {
	owner := w.Owner(task.App)
	if owner == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", "verb", "The operation is unavailable in its owner declaration")
	}
	permission, declared := "job:"+task.Job.Name, false
	if task.Kind == "job" {
		declared = slices.ContainsFunc(owner.Manifest().Jobs, func(job platform.Job) bool { return job.Name == task.Job.Name })
	} else if task.Event != nil {
		permission = "delivery:" + task.Event.Record.GetSubmission().GetSchema().GetName()
		declared = len(owner.Manifest().Subscribes) > 0 || w.Listens(task.App)
	}
	if refusal := w.Authorize(permission, owner.Manifest().AllRoles(), declared); refusal != nil {
		return refusal
	}
	var refusal *kernel.Error
	if task.Kind == "job" {
		refusal = w.State().(platform.Runner).Run(c, task.Job.Name, now)
	} else {
		w.Hops(task.Event.Hops + 1)
		if listener, ok := w.State().(platform.Listener); ok {
			names := append([]string{task.Event.Record.GetSubmission().GetSchema().GetName()}, w.Routed(task.Event.Event)...)
			if task.Event.Plan != nil {
				names = task.Event.Plan.Names
			}
			refusal = listener.Listen(c, task.Event.Event, names, now)
		} else {
			refusal = w.State().(platform.Subscriber).Handle(c, task.Event.Event)
		}
	}
	if refusal == nil {
		refusal = w.Finish()
	}
	return refusal
}
