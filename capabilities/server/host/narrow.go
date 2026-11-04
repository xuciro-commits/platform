package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
	"reflect"
	"strings"
	"time"
)

func (t *Tenant) admits(m platform.Member) *kernel.Error {
	if m.Tenant != t.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return nil
}

func (t *Tenant) mayRead(m platform.Member, now time.Time, held bool) func(ref string) bool {
	return t.mayReadIn(t.records, m, now, held)
}

func (t *Tenant) mayReadIn(store *records.Store, m platform.Member, now time.Time, held bool) func(ref string) bool {
	seen := map[string]bool{}
	walk := newAuthorizationWalk()
	return func(ref string) bool {
		if ref == "" {
			return true
		}
		if ok, known := seen[ref]; known {
			return ok
		}
		if name, isRead := strings.CutPrefix(ref, "read:"); isRead {
			ok := t.mayCallRead(m, name)
			seen[ref] = ok
			return ok
		}
		record, field, _ := strings.Cut(ref, "#")
		typ, id, _ := strings.Cut(record, "/")
		if !held {
			store.Lock()
		}
		et := store.Type(typ)
		if !held {
			store.Unlock()
		}
		app := ""
		if et != nil {
			app = et.Info().App
		}
		c := platform.NewCaller(runtime{t}, m, app, false, false)
		fields := []string{}
		if field != "" {
			fields = append(fields, field)
		}
		decision := t.authorizeDepth(store, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "derive", Type: typ, Record: id, Fields: fields}, now, held, 0, walk)
		ok := decision.Allow && len(decision.Withheld) == 0
		seen[ref] = ok
		return ok
	}
}

func (t *Tenant) mayCallRead(m platform.Member, name string) bool {
	a := t.owner["read:"+name]
	return a != nil && t.authorizeEntry(m, a.Manifest().ID, name, "read", Now()).Allow
}

func (t *Tenant) narrowable(et *records.Type) []string {
	return t.objectOwner(t.records).Narrowable(et)
}

func fieldName(typ reflect.Type, path []int) string {
	return objects.FieldName(typ, path)
}

func (t *Tenant) narrowedSource(m platform.Member, v any, now time.Time, held bool, source string) any {
	return t.objectOwner(t.records).NarrowedSource(m, v, now, held, source)
}

func (t *Tenant) narrower(m platform.Member, now time.Time, held bool) *objects.Narrower {
	return t.objectOwner(t.records).Narrower(m, now, held)
}

func (t *Tenant) narrowerIn(store *records.Store, m platform.Member, now time.Time, held bool) *objects.Narrower {
	return t.objectOwner(store).NarrowerIn(store, m, now, held)
}

func (t *Tenant) refsIn(v any) []string {
	return t.objectOwner(t.records).RefsIn(v)
}
