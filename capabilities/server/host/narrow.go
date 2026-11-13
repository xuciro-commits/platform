package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/security"
	"time"
)

func (t *Tenant) Admits(m platform.Member) *kernel.Error {
	if m.Tenant != t.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return nil
}

func (t *Tenant) mayReadIn(store *records.Store, m platform.Member, now time.Time, held bool) func(ref string) bool {
	return security.DerivationReach{CallRead: t.mayCallRead, Caller: t.objectCaller, Authorize: t.authorizeDepth}.MayRead(store, m, now, held)
}

func (t *Tenant) mayCallRead(m platform.Member, name string) bool {
	a := t.App(t.owner["read:"+name])
	return a != nil && t.authorizeEntry(m, a.Manifest().ID, name, "read", Now()).Allow
}

func (t *Tenant) narrowable(et *records.Type) []string {
	return t.objectOwner(t.Projection).Narrowable(et)
}

func (t *Tenant) narrower(m platform.Member, now time.Time, held bool) *objects.Narrower {
	return t.objectOwner(t.Projection).Narrower(m, now, held)
}

func (t *Tenant) narrowerIn(store *records.Store, m platform.Member, now time.Time, held bool) *objects.Narrower {
	return t.objectOwner(store).NarrowerIn(store, m, now, held)
}

func (t *Tenant) refsIn(v any) []string {
	return t.objectOwner(t.Projection).RefsIn(v)
}
