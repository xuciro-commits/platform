package evaluation

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/devops/descriptor"
)

func Prepare(tenant, app, key string, start descriptor.FunctionEvaluationStart) (*pb.Submission, string, error) {
	identity, err := platform.CanonicalDigest([]string{tenant, start.Candidate, start.Plan, key})
	if err != nil {
		return nil, "", err
	}
	id := "eval-" + identity[:32]
	return &pb.Submission{TenantId: tenant, PrincipalId: "app:" + app, Authority: app,
		IdempotencyKey: "evaluation:" + identity, Target: &pb.EntityRef{Type: EvaluationType, Id: id},
		Schema: &pb.SchemaRef{Name: SchemaEvaluationStart, Version: 1}, Payload: platform.Raw(start)}, id, nil
}

func BindPrepare(tenant, app, key string) func(descriptor.FunctionEvaluationStart) (*pb.Submission, string, error) {
	return func(start descriptor.FunctionEvaluationStart) (*pb.Submission, string, error) {
		return Prepare(tenant, app, key, start)
	}
}
