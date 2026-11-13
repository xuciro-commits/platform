package actions

import (
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type EffectAnswer struct {
	Tenant     string
	Draft      platform.Runtime
	Authorize  func(c platform.Caller, permission string) *kernel.Error
	Model      func() (platform.App, *pb.Submission)
	Decide     func(c platform.Caller, a platform.App, s *pb.Submission, at time.Time) *kernel.Error
	Operation  func(platform.Effect)
	Completion func() platform.OperationCompletion
	Answerer   func(app string) (platform.Answerer, bool)
	Source     func(id string) (Queued, bool)
	Origin     func(*platform.Effect, func() *kernel.Error) *kernel.Error
	Agents     func(c platform.Caller, x platform.Effect, out platform.Outcome, at time.Time)
	Finish     func(time.Time) *kernel.Error
}

func (a EffectAnswer) Stage(x platform.Effect, out platform.Outcome, at time.Time) error {
	caller := func(app string) platform.Caller {
		return platform.NewCaller(a.Draft, platform.Member{ID: "app:" + app, Tenant: a.Tenant}, app, false, true)
	}
	if x.App != "" {
		if refusal := a.Authorize(caller(x.App), "effect:"+x.Endpoint); refusal != nil {
			return refusal
		}
	}
	if x.Endpoint == acceptedformat.ModelEndpoint {
		app, reply := a.Model()
		if app == nil || reply == nil {
			return nil
		}
		if refusal := a.Decide(caller(x.App), app, reply, at); refusal != nil {
			return refusal
		}
		return nil
	}
	if x.App == "" {
		return nil
	}
	if x.Endpoint == acceptedformat.OperationEndpoint {
		a.Operation(x)
		if processes := a.Completion(); processes != nil {
			if err := processes.OperationEnded(caller("flow"), x.ID, at); err != nil {
				return err
			}
		}
	}
	c := caller(x.App)
	if app, ok := a.Answerer(x.App); ok {
		source, _ := a.Source(x.ID)
		if source.ID == "" || source.App != x.App {
			return fmt.Errorf("effect callback has no accepted source")
		}
		if refusal := a.Origin(&source.Effect, func() *kernel.Error { return app.Answer(c, x, out, at) }); refusal != nil {
			return refusal
		}
	}
	a.Agents(c, x, out, at)
	if refusal := a.Finish(at); refusal != nil {
		return refusal
	}
	return nil
}
