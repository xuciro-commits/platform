package main

import (
	"flag"
	"log"
	"os"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	_ "time/tzdata"

	platformserver "platformserver/host"
	"platformserver/host/composition"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8495")
	tenant := flag.String("tenant", "workspace", "default tenant when -tenants is not configured")
	admin := flag.String("admin", "", "explicit administrator subject for the default tenant")
	allowLoopback := flag.Bool("allow-loopback-packages", false, "allow HTTP package and catalog addresses on loopback hosts for tests")
	connectorOrigin := flag.String("test-connector-origin", os.Getenv("PLATFORM_TRUNK_CONNECTOR_ORIGIN"), "exact HTTPS loopback origin of the connector trunk stub")
	connectorCert := flag.String("test-connector-cert", os.Getenv("PLATFORM_TRUNK_CONNECTOR_CERT"), "certificate of the connector trunk stub")
	flag.Parse()
	if err := actions.ConfigureConnectorTrunk(*connectorOrigin, *connectorCert); err != nil {
		log.Fatal(err)
	}
	if *allowLoopback {
		packages.SetAllowLoopback(true)
	}
	configureStandalone(deployment, admin)
	deployment.RebuildWithCapabilities = func(id string, services runtimeprotocol.RuntimeCapabilities) (*platformserver.Tenant, error) {
		return composition.NewTenantWithCapabilities(id, services, deployment.SeatsFor(id)...)
	}
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		return deployment.RebuildWithCapabilities(id, runtimeprotocol.DefaultRuntimeCapabilities(security.Standard{}))
	}
	development := runtimeprotocol.TenantSpec{ID: *tenant}
	if *admin != "" {
		roles := map[string]string{
			controlowner.PlatformApp: controlowner.Admin,
			build.ID:                 build.Builder, ai.ID: ai.Admin,
			work.ID: work.Admin, runtimeprotocol.AgentApp: runtimeprotocol.AgentAdmin,
		}
		development.Seats = []runtimeprotocol.Seat{{Subjects: []string{*admin}, Member: platform.Member{
			ID: "builder", Roles: roles,
		}}}
	}
	if err := deployment.Run(development); err != nil {
		log.Fatal(err)
	}
}
