package host

import (
	"platformserver/services/actions"
	pageowner "platformserver/services/apps/pages"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (t *Tenant) actionCheck(m platform.Member, schema string, request pageowner.ActionCheckRequest, at time.Time, lang string) (actions.ActionCheckResult, *platform.Error) {
	appID, action, ok := hostView{t: t}.Action(schema)
	if !ok || action.Target != request.Target.Object {
		return actions.ActionCheckResult{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	caller := t.objectCaller(m, appID)
	owner, say := hostView{t: t}.actionOwner(schema), func(message string) string { return t.i18n.SayInApp(appID, lang, message) }
	if problem := actions.Admission(t.businessStopped(), m.Agent && t.suspended(m.ID), m.ID, t.delegatedBound(m, &pb.Submission{Target: &pb.EntityRef{Type: action.Target, Id: request.Target.ID}, Schema: &pb.SchemaRef{Name: schema}})); problem != nil {
		return actions.ActionCheckResult{Message: say(actions.Explained(problem, owner, schema, action.Target+"/"+request.Target.ID).Message)}, nil
	}
	result, refusal := actions.CheckAction(owner, caller, action, request.Target.ID, request.Inputs, at, t.Entity, say)
	result, refusal = actions.WithPublishCheck(result, refusal, caller, owner, schema, request.Target.ID, at, say, t.objectOwner(t.Projection), func(sub *pb.Submission) *platform.Error {
		_, problem := decideAcceptedAs(owner, t.newStagedDecision(), m, t.CurrentSchema(owner, sub), at, false)
		return problem
	})
	result.Issues = t.localizedFieldIssues(m, result.Issues, lang)
	return result, refusal
}
