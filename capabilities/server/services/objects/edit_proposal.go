package objects

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type EditProposalSource struct {
	Operation func() (platform.OperationResult, *kernel.Error)
	AI        func() (platform.OperationResult, *kernel.Error)
	FlowRun   func() (platform.FlowInstance, *kernel.Error)
}

func (s EditProposalSource) Read(engine, step string) (platform.EditProposal, *kernel.Error) {
	var result platform.OperationResult
	var refusal *kernel.Error
	switch engine {
	case "compute":
		result, refusal = s.Operation()
	case "ai":
		result, refusal = s.AI()
	case "flow":
		run, refusal := s.FlowRun()
		if refusal != nil {
			return platform.EditProposal{}, refusal
		}
		if run.State != "done" || step == "" {
			return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Choose an edit proposal from a completed flow step")
		}
		output := run.Outputs[step]
		if platform.EditProposalSchema().Validate(output, 48<<10) != nil {
			return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This flow step did not return an edit proposal")
		}
		var proposal platform.EditProposal
		if err := json.Unmarshal(output, &proposal); err != nil {
			return proposal, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		return proposal, nil
	default:
		return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a function call")
	}
	if refusal != nil {
		return platform.EditProposal{}, refusal
	}
	if result.State != "completed" {
		return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Wait for the function to finish before reviewing edits")
	}
	if err := platform.EditProposalSchema().Validate(result.Output, 48<<10); err != nil {
		return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This function did not return an edit proposal")
	}
	var proposal platform.EditProposal
	if err := json.Unmarshal(result.Output, &proposal); err != nil {
		return proposal, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return proposal, nil
}
