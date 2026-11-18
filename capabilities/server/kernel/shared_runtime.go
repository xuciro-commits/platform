package kernel

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type SharedRuntime struct{}

func (SharedRuntime) Decide(c platform.Caller, app platform.Service, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error) {
	if !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A new decision requires a private runtime")
	}
	return app.Submit(c, s, at)
}

func (SharedRuntime) Attempt(decide func() (*pb.ChangeRecord, *platform.Error)) (*pb.ChangeRecord, *platform.Error) {
	return decide()
}

func (SharedRuntime) StagedDecision() platform.StagedDecision { return nil }

func (SharedRuntime) InputRefused(*platform.Error, []platform.FieldIssue) {}

func (SharedRuntime) RequestApproval(platform.Caller, string, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error) {
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Approval requests require a private decision")
}

func (SharedRuntime) RequestOperation(platform.Caller, *pb.ChangeRecord, platform.OperationRequest) (platform.OperationCall, *platform.Error) {
	return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation requests require a private decision")
}

func (SharedRuntime) CancelOperation(platform.Caller, *pb.ChangeRecord, string) *platform.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation cancellation requires a private decision")
}

func (SharedRuntime) RequestCompilation(platform.Caller, *pb.ChangeRecord, string, string, platform.CodeBuildRequest) (platform.OperationCall, *platform.Error) {
	return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compilation requests require a private decision")
}

func (SharedRuntime) RequestFunction(platform.Caller, *pb.ChangeRecord, platform.FunctionRequest) (platform.FunctionCall, *platform.Error) {
	return platform.FunctionCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Function requests require a private decision")
}
