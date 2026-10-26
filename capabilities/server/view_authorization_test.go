package platformserver

import (
	"testing"

	"platformserver/apps/ai"
	"platformserver/apps/system"
	"platformserver/internal/systemscope"
	"platformserver/platform"
	"platformserver/platform/authz"
)

type principalAuthorization struct{ authz.Standard }

func (p principalAuthorization) Decide(q authz.Request, policies ...authz.Policy) authz.Verdict {
	if q.Subject.ID == "restricted" {
		return authz.Verdict{Reason: "principal restricted"}
	}
	return p.Standard.Decide(q, policies...)
}

func TestCatalogCacheKeepsTheSelectedAuthorizationsPrincipalBoundary(t *testing.T) {
	services := DefaultRuntimeCapabilities()
	services.Authorization = principalAuthorization{}
	services.AuthModule.ID = "test.principal-authorization"
	seats := []Seat{}
	for _, id := range []string{"builder", "restricted"} {
		seats = append(seats, Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{system.ID: system.Builder}}})
	}
	tenant, err := NewTenantWithCapabilities(systemscope.Tenant, services, NewConsole(systemscope.Tenant, seats...), NewAgents(systemscope.Tenant), ai.New(systemscope.Tenant), system.New(systemscope.Tenant, systemTestSource()))
	if err != nil {
		t.Fatal(err)
	}
	builder, _ := tenant.Member("builder")
	restricted, _ := tenant.Member("restricted")
	if len(tenant.Catalog(builder)) == 0 {
		t.Fatal("builder has no actions")
	}
	if len(tenant.Catalog(restricted)) != 0 {
		t.Fatal("cached actions crossed the selected provider's principal boundary")
	}
	if len(tenant.Catalog(builder)) == 0 {
		t.Fatal("restricted result replaced builder's catalog")
	}
}
