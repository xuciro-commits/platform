package platformserver

import agentowner "platformserver/engine/agents"

import (
	"platformserver/apps/ai"
	"platformserver/apps/system"

	"testing"
)

func TestSystemTenantFactoryKeepsTheConfiguredHostAdministrator(t *testing.T) {
	d := Deployment{HostAdmins: "operator-subject"}
	d.RebuildWithCapabilities = func(id string, services RuntimeCapabilities) (*Tenant, error) {
		return newTestTenantWithCapabilities(id, services, newConsole(id, d.SeatsFor(id)...), agentowner.New(id), ai.New(id), system.New(id, systemTestSource()))
	}
	d.Tenants(TenantSpec{ID: "workspace"})
	tenant, err := d.rebuildTenant("system")
	if err != nil {
		t.Fatal(err)
	}
	member, found := tenant.identity.MemberBySubject("operator-subject")
	if !found || !member.Holds("system", system.Publisher) {
		t.Fatal("rebuilt system tenant lost its configured publisher")
	}
}
