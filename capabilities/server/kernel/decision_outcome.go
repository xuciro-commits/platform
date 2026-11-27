package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type OutcomeHooks struct {
	Explain func(*platform.Error) *platform.Error
	Claims  func(*pb.ChangeRecord, *ClaimSet) error
}

func (d *Decision) Settled(record *pb.ChangeRecord, refusal *platform.Error, unconsumed int) (*pb.ChangeRecord, *platform.Error) {
	if d.Failure != nil {
		return nil, d.Failure
	}
	if refusal == nil && unconsumed > 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A prepared Compute input was not consumed by this accepted Flow decision")
	}
	return record, refusal
}

func (d *Decision) Outcome(src ResultSources, h OutcomeHooks, app string, s *pb.Submission, record *pb.ChangeRecord, refusal *platform.Error, binding *platform.SchemaVersion, source *ClaimSet, now time.Time) ([]byte, *platform.Error, error) {
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
	if err == nil {
		source = d.mergeAISourceClaims(raw, s, record, source, now)
	}
	if err == nil && source != nil {
		raw, err = acceptedformat.AttachSourceClaims(raw, source.Origin, source.Claims)
		if err == nil {
			err = h.Claims(record, source)
		}
	}
	if err != nil {
		refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The action could not be accepted: {reason}", err.Error())
		var preparationRefusal *platform.Error
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

func (d *Decision) AnswerWithOrigin(origin *platform.Effect, answer func() *platform.Error) *platform.Error {
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

func (d *Decision) TakeTxnStep() *platform.Error {
	if d != nil && d.Failure != nil {
		return d.Failure
	}
	if d == nil || d.TransactionSteps >= platform.TxnMaxExecutionSteps {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction execution exceeds its step budget")
	}
	d.TransactionSteps++
	return nil
}

func (d *Decision) mergeAISourceClaims(raw []byte, s *pb.Submission, record *pb.ChangeRecord, source *ClaimSet, now time.Time) *ClaimSet {
	if len(d.AIWrites) == 0 || record == nil {
		return source
	}
	var env struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return source
	}
	at := now.UTC()
	topTarget := Target(s)
	var aiClaims []acceptedformat.SourceClaim
	if env.Kind != "record-batch" {
		w, ok := d.AIWrites[topTarget+"\x00"+record.GetChangeId()]
		if !ok || w.Caller.Kind == "" {
			return source
		}
		aiClaims = append(aiClaims, acceptedformat.SourceClaim{
			Attribute:  "$ai",
			Connector:  "ai:" + w.Caller.Kind,
			SourceTime: at,
			Payload:    platform.Raw(w.Caller),
		})
	} else {
		usedRoot := false
		for _, k := range slices.Sorted(maps.Keys(d.AIWrites)) {
			w := d.AIWrites[k]
			if w.Caller.Kind == "" {
				continue
			}
			if !usedRoot && w.Target == topTarget && w.Change == record.GetChangeId() {
				usedRoot = true
				aiClaims = append(aiClaims, acceptedformat.SourceClaim{
					Attribute:  "$ai",
					Connector:  "ai:" + w.Caller.Kind,
					SourceTime: at,
					Payload:    platform.Raw(w.Caller),
				})
				continue
			}
			sum := sha256.Sum256([]byte(w.Target + "\x00" + w.Change))
			aiClaims = append(aiClaims, acceptedformat.SourceClaim{
				Attribute:  "$ai/" + w.Change + "/" + hex.EncodeToString(sum[:4]),
				Connector:  "ai:" + w.Caller.Kind,
				SourceTime: at,
				Payload:    platform.Raw(AIBatchClaim{Target: w.Target, Change: w.Change, Caller: w.Caller}),
			})
		}
	}
	if len(aiClaims) == 0 {
		return source
	}
	if source == nil {
		return &ClaimSet{Claims: aiClaims}
	}
	merged := *source
	merged.Claims = append(slices.Clone(source.Claims), aiClaims...)
	return &merged
}
