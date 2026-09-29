package mes

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaAdvice = "mes.order.advise"
const SchemaAdviceAnswer = "mes.order.advice-answer"

func adviceActions() []platform.Action {
	return []platform.Action{{Schema: SchemaAdvice, Target: OrderType, Capability: "orders", Title: "Request review advice",
		Description: "Ask the declared AI function for a suggestion; business actions still require your decision.", Payload: []platform.Field{}, Roles: []string{string(Supervisor)}},
		{Schema: SchemaAdviceAnswer, Target: OrderType, Capability: "orders", Title: "Record review advice",
			Description: "Keep the validated function answer or refusal on its source record.", Payload: platform.FunctionAnswerFields(), Automation: true}}
}

func advice(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	if !c.Staging() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "AI functions require an accepted decision")
	}
	o, ok := platform.Get[Order](c, s.GetTarget().GetId())
	if !ok {
		return nil, notFound
	}
	if s.GetSchema().GetName() == SchemaAdvice {
		if o.AdviceState == "pending" {
			return nil, conflict
		}
		return func(r *pb.ChangeRecord) {
			call, err := c.RequestFunction(r, platform.FunctionRequest{Name: "record-advice", Reply: SchemaAdviceAnswer})
			if err != nil {
				return
			}
			o.AdviceState, o.Advice, o.AdviceCategory, o.AdviceReview = "pending", "", "", false
			o.AdviceDefinition, o.AdviceModel, o.AdviceSources = call.Definition, call.Model, call.Sources
			c.Put(r, o)
		}, nil
	}
	var a platform.Answer
	if json.Unmarshal(s.GetPayload(), &a) != nil || o.AdviceState != "pending" {
		return nil, invalid
	}
	o.AdviceState = "rejected"
	if a.Outcome == "accepted" {
		var result platform.RecordAdvice
		if json.Unmarshal([]byte(a.Text), &result) != nil {
			return nil, invalid
		}
		o.Advice, o.AdviceCategory, o.AdviceReview, o.AdviceState = result.Summary, result.Category, result.Review, "ready"
	}
	return func(r *pb.ChangeRecord) { c.Put(r, o) }, nil
}
