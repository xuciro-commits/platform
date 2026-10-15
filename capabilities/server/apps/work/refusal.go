package work

import (
	"encoding/json"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const schemaRefusalEscalation = TaskType + ".refusal"

func refusalEscalationAction() platform.Action {
	return platform.Action{Schema: schemaRefusalEscalation, Target: TaskType, Capability: "tasks", Title: "Escalate repeated business refusal", Description: "Open a normal review task from distinct durable business refusals.", Automation: true, Payload: []platform.Field{}}
}

func (w *Work) runRefusalEscalations(c platform.Caller, at time.Time) *kernel.Error {
	pending, err := platform.PendingRefusalEscalations(c, at)
	if err != nil || len(pending) == 0 {
		return err
	}
	request := pending[0]
	_, err = platform.Decide(c, w, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "refusal-escalation:" + request.Key, Target: &pb.EntityRef{Type: TaskType, Id: request.TaskID()}, Schema: &pb.SchemaRef{Name: schemaRefusalEscalation, Version: 1}, Payload: platform.Raw(request)}, at)
	return err
}

func (w *Work) escalateRefusal(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var request platform.RefusalEscalationRequest
	if json.Unmarshal(s.GetPayload(), &request) != nil || s.GetTarget().GetId() != request.TaskID() || s.GetIdempotencyKey() != "refusal-escalation:"+request.Key {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A refusal escalation needs its original durable business facts")
	}
	pending, err := platform.PendingRefusalEscalations(c, at)
	if err != nil {
		return nil, err
	}
	verified := false
	for _, candidate := range pending {
		if reflect.DeepEqual(request, candidate) {
			verified = true
			break
		}
	}
	if !verified {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The business refusals have no pending declared escalation")
	}
	return func(receipt *pb.ChangeRecord) {
		task := WorkTask{Record: platform.Record{ID: request.TaskID()}, Title: "Review repeated business refusal", Body: request.Code + ": " + request.Message, Ref: request.Subject, App: request.App, Key: "refusal:" + request.Key, Candidates: request.Candidates, State: "open"}
		if err := c.Put(receipt, task); err != nil {
			c.FailDecision(err)
			return
		}
		to := make([]platform.Recipient, 0, len(task.Candidates))
		for _, member := range task.Candidates {
			to = append(to, platform.Recipient{Member: member})
		}
		c.Notify(platform.Notification{Title: task.Title, Body: task.Body, Ref: task.Ref, Key: "task:" + task.ID}, at, to...)
	}, nil
}
