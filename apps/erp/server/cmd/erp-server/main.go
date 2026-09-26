// Command erp-server runs the ERP app on a development host (docs/Apps.md
// step 6) with a demo chart of accounts and the current month open. With -web
// the host serves the workspace's build; the development tokens "controller",
// "accountant" and "buyer" sign in.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"erp"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8499")
	flag.Parse()
	seat := func(token, id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{token}, Member: platform.Member{ID: id, Roles: roles}}
	}
	seats := deployment.Seats([]platformserver.Seat{
		seat("controller", "controller-1", map[string]string{erp.ID: erp.Controller, platformserver.PlatformApp: platformserver.Admin,
			platformserver.WorkApp: platformserver.WorkAdmin, platformserver.FlowApp: platformserver.FlowAdmin}),
		seat("accountant", "accountant-1", map[string]string{erp.ID: erp.Accountant}),
		seat("buyer", "buyer-1", map[string]string{erp.ID: erp.Buyer}),
	})
	t, err := platformserver.NewTenant("dev", platformserver.NewConsole("dev", seats...), platformserver.NewWork("dev"), platformserver.NewFlows("dev"), erp.New("dev"))
	if err == nil && deployment.Database == "" {
		err = seed(t, time.Now())
	}
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}

// seed gives a development host its books in CNY, the demo chart of accounts,
// this month open, a supplier and three products.
func seed(t *platformserver.Tenant, now time.Time) error {
	m, _ := t.Member("controller-1")
	submit := func(schema, typ, id string, payload any) error {
		raw, _ := json.Marshal(payload)
		_, err := t.Submit(m, &pb.Submission{TenantId: "dev", PrincipalId: m.ID, Authority: erp.ID, IdempotencyKey: "seed:" + schema + ":" + id,
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return fmt.Errorf("seed %s %s: %v", schema, id, err)
		}
		return nil
	}
	if _, err := t.Submit(m, &pb.Submission{TenantId: "dev", PrincipalId: m.ID, Authority: platformserver.PlatformApp, IdempotencyKey: "seed:currency",
		Target: &pb.EntityRef{Type: platformserver.SettingType, Id: platformserver.PlatformApp + "/" + platformserver.SettingCurrency},
		Schema: &pb.SchemaRef{Name: platformserver.SchemaSettingSet, Version: 1}, Payload: []byte(`{"value":"CNY"}`)}, now); err != nil {
		return fmt.Errorf("seed the currency: %v", err)
	}
	for _, a := range erp.Chart() {
		if err := submit(erp.AccountType+".create", erp.AccountType, a.ID, map[string]string{"name": a.Name, "kind": a.Kind}); err != nil {
			return err
		}
	}
	if err := submit(erp.SchemaPeriodOpen, erp.PeriodType, now.Format("2006-01"), map[string]any{}); err != nil {
		return err
	}
	if err := submit(erp.PartnerType+".create", erp.PartnerType, "BP-STEEL", map[string]string{"name": "Suzhou Steel Co.", "role": "supplier"}); err != nil {
		return err
	}
	for _, p := range []struct {
		id, name, kind, unit string
		cost                 int64
	}{
		{"M-STEEL", "Steel sheet", "material", "kg", 500}, {"M-BOLT", "Bolt M8", "material", "pcs", 20}, {"P-100", "Bracket", "finished", "pcs", 4000},
	} {
		if err := submit(erp.ProductType+".create", erp.ProductType, p.id, map[string]any{"name": p.name, "kind": p.kind, "unit": p.unit,
			"cost": map[string]any{"amount": p.cost, "currency": "CNY"}}); err != nil {
			return err
		}
	}
	return nil
}
