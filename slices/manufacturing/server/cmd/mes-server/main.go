// Command mes-server runs the manufacturing app ("mes") on the platform host for
// one demo plant. Without flags it keeps state in memory and accepts the demo
// tokens; with -database and -oidc-issuer it is the production path (ADR-0007).
package main

import (
	"flag"
	"log"
	"strings"

	"mes"
	"platformserver"
)

const tenant = "plant-sz"

// demo is the directory without -directory; a development token is the subject.
var demo = []platformserver.Seat{
	seat("supervisor", "sup-1", mes.Supervisor, "L1", "L2"),
	seat("operator-l1", "op-l1", mes.Operator, "L1"),
	seat("operator-l2", "op-l2", mes.Operator, "L2"),
	seat("quality-1", "qa-1", mes.Quality),
	seat("quality-2", "qa-2", mes.Quality),
	seat("gateway-l1", "gateway-l1", mes.Gateway),
	seat("erp", "erp", mes.ERP),
}

func seat(subject, id string, role mes.Role, lines ...string) platformserver.Seat {
	s := platformserver.Seat{Subjects: []string{subject}, Member: platformserver.Member{ID: id, Roles: map[string]string{"mes": string(role)}}}
	if subject == "supervisor" {
		s.Roles[platformserver.PlatformApp] = platformserver.Admin
	}
	if len(lines) > 0 {
		s.Attributes = map[string][]string{"lines": lines}
	}
	return s
}

func main() {
	deployment := platformserver.Flags("127.0.0.1:8490")
	disable := flag.String("disable", "", "comma-separated capabilities to deactivate (ADR-0008)")
	flag.Parse()
	plant := mes.NewPlant(tenant, mes.DemoMaster())
	for _, d := range mes.DemoConnectors(tenant) {
		plant.RegisterConnector(d)
	}
	for _, c := range strings.FieldsFunc(*disable, func(r rune) bool { return r == ',' }) {
		if !plant.Disable(c) {
			log.Fatalf("-disable: no capability %q", c)
		}
	}
	t, err := platformserver.NewTenant(tenant, platformserver.NewDirectory(tenant, deployment.Seats(demo)...), plant)
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
