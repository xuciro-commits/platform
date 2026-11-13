package kernel

import (
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	contract "platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func (c *Committed) ReusedPrior(apps []platform.App, tenant, key, principal, correlation string) (*pb.ChangeRecord, *contract.Error, bool) {
	for _, app := range apps {
		var original *pb.Submission
		var receipt *pb.ChangeRecord
		prior := c.Prior(app.Manifest().ID, app.AcceptedLedger(), tenant, key)
		if len(prior.Answer) > 0 {
			result, saved, err := acceptedformat.DecodeBatch(prior.Answer)
			if err != nil {
				return nil, observationReplayUnavailable(err), true
			}
			receipt = saved
			original, err = acceptedformat.BatchSubmission(result, saved)
			if err != nil {
				return nil, observationReplayUnavailable(err), true
			}
		} else if prior.Record != nil {
			receipt, original = prior.Record, prior.Record.GetSubmission()
		}
		if original != nil {
			if original.GetPrincipalId() != principal || original.GetCorrelationId() != correlation {
				return nil, &contract.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
			}
			return receipt, nil, true
		}
		if saved := prior.Refusal; saved != nil {
			var original pb.Submission
			if len(saved.Submission) == 0 || protojson.Unmarshal(saved.Submission, &original) != nil || original.GetPrincipalId() != principal || original.GetCorrelationId() != correlation {
				return nil, &contract.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}, true
			}
			refusal := saved.Error
			return nil, &refusal, true
		}
	}
	return nil, nil, false
}

func observationReplayUnavailable(err error) *contract.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Observation read is unavailable: {reason}", err.Error())
}
