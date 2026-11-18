package objects

import (
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"time"
)

type Owner struct {
	Store                  *records.Store
	BaseStore              *records.Store
	TenantID               string
	Admit                  func(platform.Member) *platform.Error
	AuthorizationMember    func(platform.Member) platform.Member
	Caller                 func(platform.Member, string) platform.Caller
	Authorize              func(*records.Store, platform.Caller, platform.AuthzRequest, time.Time, bool) platform.AuthzDecision
	AuthorizeRecordSet     func(*records.Store, platform.Caller, time.Time) func(platform.AuthzRequest) platform.AuthzDecision
	Visible                func(*records.Store, platform.Member, *records.Type, time.Time) (func(reflect.Value) bool, *platform.Error)
	VisibleForCaller       func(*records.Store, platform.Caller, *records.Type, time.Time) (func(reflect.Value) bool, *platform.Error)
	MayRead                func(*records.Store, platform.Member, time.Time, bool) func(string) bool
	MayCallRead            func(platform.Member, string) bool
	Definitions            func(platform.Member) []platform.Definition
	Entity                 func(string) (platform.RecordTypeInfo, bool)
	App                    func(string) platform.Service
	BindInterface          func(platform.NamedQuery, ...platform.RecordTypeInfo) (platform.NamedQuery, error)
	ValidateInterfaceQuery func(platform.NamedQuery) error
	VisibleLink            func(platform.Member, platform.AssetBinding) *platform.LinkType
	HasProcesses           func() bool
	Follows                func(platform.Member, string) bool
	Deliveries             func(string, time.Time) []RecordDelivery
	AuditPersonal          func(protocol.PersonalRead)
	RecordSetBinding       func(platform.AssetBinding) (*platform.Definition, bool)
	Claims                 *protocol.FactLog
}

func (o *Owner) authorizationMember(m platform.Member) platform.Member {
	return o.AuthorizationMember(m)
}

func (o *Owner) caller(m platform.Member, app string) platform.Caller {
	return o.Caller(m, app)
}

func (o *Owner) admits(m platform.Member) *platform.Error {
	return o.Admit(m)
}

func (o *Owner) authorizeFrom(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool) platform.AuthzDecision {
	return o.Authorize(store, c, q, at, held)
}

func (o *Owner) visible(m platform.Member, et *records.Type, at time.Time) (func(reflect.Value) bool, *platform.Error) {
	return o.Visible(o.Store, m, et, at)
}

func (o *Owner) visibleForCallerIn(store *records.Store, c platform.Caller, et *records.Type, at time.Time) (func(reflect.Value) bool, *platform.Error) {
	return o.VisibleForCaller(store, c, et, at)
}

func (o *Owner) mayReadIn(store *records.Store, m platform.Member, at time.Time, held bool) func(string) bool {
	return o.MayRead(store, m, at, held)
}

func (o *Owner) entity(typ string) (platform.RecordTypeInfo, bool) {
	return o.Entity(typ)
}

func (o *Owner) app(id string) platform.Service {
	return o.App(id)
}

func (o *Owner) definitions(m platform.Member) []platform.Definition {
	return o.Definitions(m)
}

func (o *Owner) bindInterfaceQuery(q platform.NamedQuery, drafts ...platform.RecordTypeInfo) (platform.NamedQuery, error) {
	return o.BindInterface(q, drafts...)
}

func (o *Owner) validateInterfaceQuery(q platform.NamedQuery) error {
	return o.ValidateInterfaceQuery(q)
}

func (o *Owner) visibleLink(m platform.Member, binding platform.AssetBinding) *platform.LinkType {
	return o.VisibleLink(m, binding)
}

func (o *Owner) recordSetBinding(binding platform.AssetBinding) (*platform.Definition, bool) {
	return o.RecordSetBinding(binding)
}
