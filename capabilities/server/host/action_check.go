package host

import (
	"encoding/json"
	pageowner "platformserver/services/apps/pages"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) actionCheck(m platform.Member, schema string, request pageowner.ActionCheckRequest, at time.Time, lang string) (pageowner.ActionCheckResult, *kernel.Error) {
	appID, action, ok := hostView{t: t}.Action(schema)
	if !ok || action.Target != request.Target.Object {
		return pageowner.ActionCheckResult{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	transition, lifecycle, hasTransition := t.modelTransition(platform.AssetRef{App: appID, Kind: platform.AssetAction, Name: schema})
	caller := platform.NewCaller(runtime{t: t, scoped: true}, m, appID, false, false)
	decision := t.authorizeFrom(t.Projection, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "action", Action: schema, Type: request.Target.Object, Record: request.Target.ID}, at, false)
	return pageowner.ActionChecks{Caller: caller, Transition: transition, Lifecycle: lifecycle, Moves: hasTransition, Entity: t.Entity,
		Read: func() (json.RawMessage, *kernel.Error) {
			return t.readRecordFrom(t.Projection, caller, request.Target.Object, request.Target.ID, at)
		},
		Say: func(message string) string { return t.i18n.SayInApp(appID, lang, message) },
		Localize: func(issues []platform.FieldIssue) []platform.FieldIssue {
			return t.localizedFieldIssues(m, issues, lang)
		},
	}.Check(action, request, decision, at)
}
