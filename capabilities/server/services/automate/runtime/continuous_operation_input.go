package automation

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type OperationInputSealer struct {
	Operation func(request platform.OperationRequest) (platform.Operation, int, *kernel.Error)
	Seal      func(app, key, member, definition, hash string, inputs json.RawMessage, budget int) (platform.OperationInputArtifact, error)
	Discard   func(platform.OperationInputArtifact)
}

type OperationInput struct {
	Artifact    platform.OperationInputArtifact
	RequestHash string
}

func (i *OperationInput) ArtifactPointer() *platform.OperationInputArtifact {
	if i == nil {
		return nil
	}
	return &i.Artifact
}

func (s OperationInputSealer) Prepare(member platform.Member, instance string, request platform.OperationRequest) (*OperationInput, *kernel.Error) {
	refuse := func(code pb.ErrorCode, message string) (*OperationInput, *kernel.Error) {
		return nil, platform.Refuse(code, message)
	}
	if request.App == "" || request.Name == "" || request.Key == "" || request.OnBehalf != member.ID || request.Target != platform.FlowInstanceType+"/"+instance {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The prepared window names an invalid Compute operation")
	}
	op, version, refusal := s.Operation(request)
	if refusal != nil {
		return nil, refusal
	}
	definition, err := platform.CanonicalDigest([]any{request.App, op, version})
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute definition cannot be bound")
	}
	budget := op.Limits.DataInputBytes
	if budget == 0 {
		return nil, nil
	}
	if err := op.Input.Validate(request.Inputs, budget); err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The window does not satisfy its Compute input: "+err.Error())
	}
	hash, err := platform.CanonicalDigest(request.Inputs)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The prepared window Compute input cannot be encoded")
	}
	ref, err := s.Seal(request.App, request.Key, member.ID, definition, hash, request.Inputs, budget)
	if err != nil {
		s.Discard(ref)
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute input could not be sealed: "+err.Error())
	}
	requestHash, err := platform.CanonicalDigest(request)
	if err != nil {
		s.Discard(ref)
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute request cannot be bound")
	}
	return &OperationInput{Artifact: ref, RequestHash: requestHash}, nil
}

func GroupOperationInputs(inputs []*OperationInput) map[string][]*OperationInput {
	if len(inputs) == 0 {
		return nil
	}
	grouped := make(map[string][]*OperationInput, len(inputs))
	for _, prepared := range inputs {
		if prepared != nil {
			grouped[prepared.RequestHash] = append(grouped[prepared.RequestHash], prepared)
		}
	}
	return grouped
}

func PendingOperationInputs(inputs map[string][]*OperationInput) int {
	pending := 0
	for _, prepared := range inputs {
		pending += len(prepared)
	}
	return pending
}

func TakeOperationInput(inputs map[string][]*OperationInput, c platform.Caller, q platform.OperationRequest, platformApp string) (*platform.OperationInputArtifact, *kernel.Error) {
	hash, _ := platform.CanonicalDigest(q)
	pending := inputs[hash]
	if len(pending) == 0 {
		return nil, nil
	}
	prepared := pending[0]
	platformOwner := c.App == platformApp && c.ID == prepared.Artifact.Member
	flowOwner := c.Automation && c.App == "flow" && c.ID == "app:flow" && q.OnBehalf == prepared.Artifact.Member
	if !platformOwner && !flowOwner || hash != prepared.RequestHash {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The prepared input belongs to another operation request")
	}
	if len(pending) == 1 {
		delete(inputs, hash)
	} else {
		inputs[hash] = pending[1:]
	}
	return &prepared.Artifact, nil
}
