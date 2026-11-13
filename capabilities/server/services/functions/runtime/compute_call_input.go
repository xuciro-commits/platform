package compute

import (
	"bytes"
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func CheckDirectCall(q platform.OperationRequest) *kernel.Error {
	if q.App == "" || q.Key == "" || len(q.Key) > 256 || q.Version < 0 || q.Target != "" || q.OnBehalf != "" || q.Release != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A direct call requires an owner and stable key")
	}
	return nil
}

func PriorSealedDefinition(body string) (platform.Operation, int, bool) {
	var previous OperationBinding
	if json.Unmarshal([]byte(body), &previous) == nil && previous.SealedInput != nil {
		return previous.Definition, previous.Call.Version, true
	}
	return platform.Operation{}, 0, false
}

func (c *Channel) SealCallInput(call, member string, q platform.OperationRequest, op platform.Operation, version int) (platform.OperationRequest, platform.OperationInputArtifact, string, *kernel.Error) {
	if len(q.Inputs) > op.Limits.DataInputBytes {
		return q, platform.OperationInputArtifact{}, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The input exceeds its declared data channel budget")
	}
	raw := bytes.Clone(q.Inputs)
	if err := op.Input.Validate(raw, op.Limits.DataInputBytes); err != nil {
		return q, platform.OperationInputArtifact{}, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	hash, _ := platform.CanonicalDigest(json.RawMessage(raw))
	definition, _ := platform.CanonicalDigest([]any{q.App, op, version})
	ref, err := c.SealInput(call, member, definition, hash, raw, op.Limits.DataInputBytes)
	if err != nil {
		return q, platform.OperationInputArtifact{}, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	q.Inputs = platform.Raw(map[string]any{"sealedInput": map[string]any{"digest": ref.Digest, "size": ref.Size, "inputHash": ref.InputHash, "definition": ref.Definition}})
	requestHash, _ := platform.CanonicalDigest(q)
	return q, ref, requestHash, nil
}

func AcceptedCall(body, ticket string, sealed bool) (platform.OperationCall, bool, bool) {
	var binding OperationBinding
	if json.Unmarshal([]byte(body), &binding) != nil {
		return platform.OperationCall{}, false, false
	}
	return binding.Call, sealed && binding.SealedInput != nil && binding.SealedInput.Ticket == ticket, true
}
