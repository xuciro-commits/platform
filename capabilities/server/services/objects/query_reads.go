package objects

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type QueryReads struct {
	Store         *records.Store
	Named         func(app, name string) (platform.NamedQuery, bool)
	Version       func(m platform.Member, ref platform.AssetRef, version int) (platform.NamedQuery, bool)
	Schema        func(q platform.NamedQuery) platform.ValueSchema
	Entity        func(typ string) (platform.EntityInfo, bool)
	Authorization platform.AuthorizationEngine
	Run           func(m platform.Member, q platform.NamedQuery, of string, now time.Time) (RecordPage, *kernel.Error)
}

func (r QueryReads) Query(c platform.Caller, app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	q, ok := r.Named(app, name)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Named query is unavailable")
	}
	return r.declared(c, q, inputs, now)
}

func (r QueryReads) QueryVersion(c platform.Caller, app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	q, ok := r.Version(c.Member, platform.AssetRef{App: app, Kind: platform.AssetQuery, Name: name}, version)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Retained query is unavailable to this member")
	}
	schema := r.Schema(q)
	if err := schema.Validate(inputs, 32<<10); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return r.declared(c, q, inputs, now)
}

func (r QueryReads) RuleQuery(c platform.Caller, set platform.RuleCollection, input json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	q, ok := r.Version(c.Member, platform.AssetRef{App: set.App, Kind: platform.AssetQuery, Name: set.Query}, set.Version)
	if !ok || q.Object != set.Object || (q.By == "") != (set.Of == "") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule collection declaration is unavailable")
	}
	info, known := r.Entity(q.Object)
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule collection object is unavailable")
	}
	if set.Policy == "complete" && (info.Scope.LevelFor(c.RolesIn(info.App), r.Authorization) != platform.ScopeTenant || info.Scope.Through != nil) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Complete collection requires permission to read its whole object / 完整集合需要整个对象的读取权限")
	}
	return r.QueryVersion(c, set.App, set.Query, set.Version, input, now)
}

func (r QueryReads) declared(c platform.Caller, q platform.NamedQuery, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	target, refusal := DeclaredQueryFor(inputs)
	if refusal != nil {
		return nil, refusal
	}
	page, refusal := r.Run(c.Member, q, target, now)
	if refusal != nil {
		return nil, refusal
	}
	r.Store.Lock()
	et := r.Store.Type(q.Object)
	r.Store.Unlock()
	var info platform.EntityInfo
	if et != nil {
		info = et.Info()
	}
	return DeclaredQueryAnswer(c, q, info, et != nil, page.Records, page.Total)
}
