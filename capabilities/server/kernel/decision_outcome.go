package kernel

import (
	"errors"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type OutcomeHooks struct {
	Explain func(*kernel.Error) *kernel.Error
	Claims  func(*pb.ChangeRecord, *ClaimSet) error
}

func (d *Decision) Outcome(src ResultSources, h OutcomeHooks, app string, s *pb.Submission, record *pb.ChangeRecord, refusal *kernel.Error, binding *platform.SchemaVersion, source *ClaimSet, now time.Time) ([]byte, *kernel.Error, error) {
	var raw []byte
	var err error
	if refusal != nil {
		refusal = h.Explain(refusal)
		fact, business := d.BusinessRefusals[refusal]
		var businessFact *platform.BusinessRefusalFact
		if business {
			businessFact = &fact
		}
		raw, err = acceptedformat.EncodeBusinessRefusal(s, now, refusal, binding, businessFact, d.InputIssues[refusal]...)
		return raw, refusal, err
	}
	raw, err = d.SealResult(src, app, s, record, now)
	if err == nil && source != nil {
		raw, err = acceptedformat.AttachSourceClaims(raw, source.Origin, source.Claims)
		if err == nil {
			err = h.Claims(record, source)
		}
	}
	if err != nil {
		refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The action could not be accepted: {reason}", err.Error())
		var preparationRefusal *kernel.Error
		if errors.As(err, &preparationRefusal) {
			refusal = preparationRefusal
		}
		var budget *acceptedformat.ResultBudgetError
		if errors.As(err, &budget) {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Accepted result needs {bytes} bytes; limit is {limit} bytes / 接受结果超出字节预算", budget.Bytes, budget.Limit)
		}
		raw, err = acceptedformat.EncodeSchemaRefusal(s, now, refusal, binding)
	}
	return raw, refusal, err
}

func (d *Decision) AnswerWithOrigin(origin *platform.Effect, answer func() *kernel.Error) *kernel.Error {
	previous := d.CallbackOrigin
	d.CallbackOrigin = origin
	defer func() { d.CallbackOrigin = previous }()
	return answer()
}

func (d *Decision) EffectResult(src ResultSources, result *acceptedformat.Effect, app string, at time.Time, prepare func(acceptedformat.ChangePlan) error) error {
	result.Authorizations = append(result.Authorizations, d.Authorizations...)
	result.After.CallbackError = result.CallbackError
	if d.Failure != nil {
		return d.Failure
	}
	if err := d.EffectDecision(src, result, app, at); err != nil {
		return err
	}
	return prepare(result.ChangePlan())
}

func (d *Decision) TakeTxnStep() *kernel.Error {
	if d != nil && d.Failure != nil {
		return d.Failure
	}
	if d == nil || d.TransactionSteps >= platform.TxnMaxExecutionSteps {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction execution exceeds its step budget")
	}
	d.TransactionSteps++
	return nil
}
