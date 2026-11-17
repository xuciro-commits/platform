package publication

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type FunctionEvaluationPlan struct {
	Revision   uint32
	Function   string
	Evaluation []platform.EvaluationPolicy
}

type FunctionEvaluationEnvironment struct {
	App, Builder string
	Authority    func() platform.Authority
	Function     func(string, int) (platform.AIFunction, int, bool)
	Plan         func(platform.Caller, string) (FunctionEvaluationPlan, bool)
}

type FunctionEvaluationBinding struct {
	Entity  func() platform.Entity
	Actions func() []platform.Action
	Handles func(string) bool
	Submit  func(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)
}
