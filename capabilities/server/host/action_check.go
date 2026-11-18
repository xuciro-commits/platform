package host

import (
	"platformserver/services/actions"
	pageowner "platformserver/services/apps/pages"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (t *Tenant) actionCheck(m platform.Member, schema string, request pageowner.ActionCheckRequest, at time.Time, lang string) (pageowner.ActionCheckResult, *platform.Error) {
	appID, action, ok := hostView{t: t}.Action(schema)
	if !ok || action.Target != request.Target.Object {
		return pageowner.ActionCheckResult{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	caller := t.objectCaller(m, appID)
	owner, say := hostView{t: t}.actionOwner(schema), func(message string) string { return t.i18n.SayInApp(appID, lang, message) }
	result, refusal := actions.CheckAction(owner, caller, action, request.Target.ID, request.Inputs, at, t.Entity, say)
	result, refusal = actions.WithPublishCheck(result, refusal, caller, owner, schema, request.Target.ID, at, say, t.objectOwner(t.Projection), func(sub *pb.Submission) *platform.Error {
		if problem := t.delegatedBound(m, sub); problem != nil {
			return problem
		}
		_, problem := decideAcceptedAs(owner, t.newStagedDecision(), m, t.CurrentSchema(owner, sub), at, false)
		return problem
	})
	result.Issues = t.localizedFieldIssues(m, result.Issues, lang)
	return result, refusal
}
