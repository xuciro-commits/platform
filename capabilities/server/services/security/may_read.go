package security

import (
	"strings"
	"time"

	"platformserver/kernel/records"
	"platformserver/platform"
)

type DerivationReach struct {
	CallRead  func(platform.Member, string) bool
	Caller    func(platform.Member, string) platform.Caller
	Authorize func(*records.Store, platform.Caller, platform.AuthzRequest, time.Time, bool, int, *Traversal) platform.AuthzDecision
}

func (r DerivationReach) MayRead(store *records.Store, m platform.Member, now time.Time, held bool) func(ref string) bool {
	seen := map[string]bool{}
	walk := NewTraversal()
	return func(ref string) bool {
		if ref == "" {
			return true
		}
		if ok, known := seen[ref]; known {
			return ok
		}
		if name, isRead := strings.CutPrefix(ref, "read:"); isRead {
			ok := r.CallRead(m, name)
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
		fields := []string{}
		if field != "" {
			fields = append(fields, field)
		}
		decision := r.Authorize(store, r.Caller(m, app), platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "derive", Type: typ, Record: id, Fields: fields}, now, held, 0, walk)
		ok := decision.Allow && len(decision.Withheld) == 0
		seen[ref] = ok
		return ok
	}
}

func DeclaredPrincipal(id string, noIdentity bool, appExists func(string) bool, agent func(string) (declared, tracked bool)) bool {
	declared := noIdentity || id == "host:maintenance"
	if app, service := strings.CutPrefix(id, "app:"); service {
		declared = appExists(app)
	}
	if name, generated := strings.CutPrefix(id, "agent:"); generated {
		if known, tracked := agent(name); tracked {
			declared = known
		}
	}
	return declared
}
