package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

func (b *Build) answered(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *kernel.Error {
	var req dataowner.WritebackRequest
	if json.Unmarshal([]byte(effect.Body), &req) != nil || req.Writeback == "" {
		return nil
	}
	if req.Dispatch != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "failed", "unknown": "unknown", "discarded": "unknown"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		if outcome == "" {
			return nil
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, dispatchSubmission(c, req.Dispatch, platform.DispatchAnswerSchema, key, map[string]any{"intent": effect.ID, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	if req.Confirmation != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "refused", "unknown": "unknown", "discarded": "refused"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		tag := req.Party
		if req.Compensates != "" {
			tag += ":compensate"
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, confirmationSubmission(c, req.Confirmation, confirmationAnswer, key, map[string]any{"tag": tag, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	w, ok := platform.Get[dataowner.Writeback](c, req.Writeback)
	if !ok {
		return nil
	}
	if req.Version == 1 {
		w.Object, w.Result = req.Object, slices.Clone(req.Result)
		if w.Object == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Writeback request has no frozen target")
		}
	} else if req.Version != 0 || w.DefinitionVersion != 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Historical writeback request requires its original unversioned definition")
	}
	ans := dataowner.WritebackAns{Effect: effect.ID, Target: effect.Target, Result: out.Result, Detail: out.Detail, Answer: dataowner.ClipAnswer(out.Answer)}
	if out.Result != "delivered" && out.Result != "rejected" && out.Result != "unknown" {
		ans.Result = "failed"
	}
	payload, _ := json.Marshal(map[string]any{"answer": ans})
	if _, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answered:" + effect.ID + map[bool]string{true: ":unknown"}[out.Result == "unknown"],
		Target: &pb.EntityRef{Type: dataowner.WritebackType, Id: w.ID}, Schema: &pb.SchemaRef{Name: dataowner.SchemaWritebackAnswered, Version: 1}, Payload: payload}, now); err != nil {
		return err
	}
	typ, id, ok := strings.Cut(effect.Target, "/")
	if out.Result != "delivered" || !ok || typ != w.Object || len(w.Result) == 0 {
		return nil
	}
	fields := w.Fields(out.Answer)
	if len(fields) == 0 {
		return nil
	}
	edit, _ := json.Marshal(fields)
	_, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answer-fields:" + effect.ID,
		Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".edit", Version: 1}, Payload: edit}, now)
	return err
}
