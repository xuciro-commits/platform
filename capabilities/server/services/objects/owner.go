package objects

import (
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/security"
	"reflect"
	"time"
)

type Owner struct {
	Store                  *records.Store
	BaseStore              *records.Store
	TenantID               string
	Admit                  func(platform.Member) *kernel.Error
	AuthorizationMember    func(platform.Member) platform.Member
	Caller                 func(platform.Member, string) platform.Caller
	Authorize              func(*records.Store, platform.Caller, platform.AuthzRequest, time.Time, bool) platform.AuthzDecision
	AuthorizeDepth         func(*records.Store, platform.Caller, platform.AuthzRequest, time.Time, bool, int, *security.Traversal) platform.AuthzDecision
	Visible                func(*records.Store, platform.Member, *records.Type, time.Time) (func(reflect.Value) bool, *kernel.Error)
	VisibleForCaller       func(*records.Store, platform.Caller, *records.Type, time.Time) (func(reflect.Value) bool, *kernel.Error)
	MayRead                func(*records.Store, platform.Member, time.Time, bool) func(string) bool
	MayCallRead            func(platform.Member, string) bool
	Definitions            func(platform.Member) []platform.Definition
	Entity                 func(string) (platform.EntityInfo, bool)
	App                    func(string) platform.App
	BindInterface          func(platform.NamedQuery, ...platform.EntityInfo) (platform.NamedQuery, error)
	ValidateInterfaceQuery func(platform.NamedQuery) error
	VisibleLink            func(platform.Member, platform.AssetBinding) *platform.LinkType
	OwnedRelationTargets   func(*records.Store, platform.Caller, platform.LinkType, string, string, time.Time) ([]string, bool, *kernel.Error)
	HasProcesses           func() bool
	Follows                func(platform.Member, string) bool
	Deliveries             func(string, time.Time) []RecordDelivery
	AuditPersonal          func(platform.PersonalRead)
	RecordSetBinding       func(platform.AssetBinding) (*platform.Definition, bool)
	Claims                 *kernel.FactLog
}

func (o *Owner) authorizationMember(m platform.Member) platform.Member {
	return o.AuthorizationMember(m)
}

func (o *Owner) caller(m platform.Member, app string) platform.Caller {
	return o.Caller(m, app)
}

func (o *Owner) admits(m platform.Member) *kernel.Error {
	return o.Admit(m)
}

func (o *Owner) authorizeFrom(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool) platform.AuthzDecision {
	return o.Authorize(store, c, q, at, held)
}

func (o *Owner) authorizeDepth(store *records.Store, c platform.Caller, q platform.AuthzRequest, at time.Time, held bool, depth int, walk *security.Traversal) platform.AuthzDecision {
	return o.AuthorizeDepth(store, c, q, at, held, depth, walk)
}

func (o *Owner) visible(m platform.Member, et *records.Type, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return o.Visible(o.Store, m, et, at)
}

func (o *Owner) visibleForCallerIn(store *records.Store, c platform.Caller, et *records.Type, at time.Time) (func(reflect.Value) bool, *kernel.Error) {
	return o.VisibleForCaller(store, c, et, at)
}

func (o *Owner) mayReadIn(store *records.Store, m platform.Member, at time.Time, held bool) func(string) bool {
	return o.MayRead(store, m, at, held)
}

func (o *Owner) entity(typ string) (platform.EntityInfo, bool) {
	return o.Entity(typ)
}

func (o *Owner) app(id string) platform.App {
	return o.App(id)
}

func (o *Owner) definitions(m platform.Member) []platform.Definition {
	return o.Definitions(m)
}

func (o *Owner) bindInterfaceQuery(q platform.NamedQuery, drafts ...platform.EntityInfo) (platform.NamedQuery, error) {
	return o.BindInterface(q, drafts...)
}

func (o *Owner) validateInterfaceQuery(q platform.NamedQuery) error {
	return o.ValidateInterfaceQuery(q)
}

func (o *Owner) visibleLink(m platform.Member, binding platform.AssetBinding) *platform.LinkType {
	return o.VisibleLink(m, binding)
}

func (o *Owner) ownedRelationTargets(store *records.Store, c platform.Caller, link platform.LinkType, direction, id string, at time.Time) ([]string, bool, *kernel.Error) {
	return o.OwnedRelationTargets(store, c, link, direction, id, at)
}

func (o *Owner) recordSetBinding(binding platform.AssetBinding) (*platform.Definition, bool) {
	return o.RecordSetBinding(binding)
}
