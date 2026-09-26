// Package plant is the plant's software as a solution (ADR-0024 7c, 7d): the
// platform's directory, organisation, AI, work, flows, agents and knowledge,
// the MES, and its books — the ERP app, or the adapter to an ERP outside —
// composed without a bridge. The books provide production.orders/1 and the MES
// consumes it: they release production orders, the plant executes them and
// confirms what it made, and the books take its cost. Neither app knows the other.
package plant

import (
	"encoding/json"
	"fmt"
	"time"

	"erp"
	"mes"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

// NewTenant composes the plant for one tenant with its books, the provider of
// production.orders/1 (erp.New or erplink.New); seats belong to the plant's units.
func NewTenant(id string, books platform.App, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	t, err := platformserver.NewTenant(id, platformserver.NewConsole(id, seats...),
		platformserver.NewOrganization(id, mes.DemoOrganization(platformserver.Memberships(seats))), platformserver.NewAI(id),
		platformserver.NewWork(id), platformserver.NewFlows(id), platformserver.NewAgents(id), platformserver.NewKnowledge(id),
		mes.NewPlant(id, mes.DemoMaster()), books)
	if err == nil {
		err = t.Connect(mes.DemoConnectors(id)...)
	}
	return t, err
}

// Seat signs in as subject with roles by app, and belongs to units of the site structure (ADR-0012).
func Seat(subject, id string, roles map[string]string, units ...string) platformserver.Seat {
	s := platformserver.Seat{Subjects: []string{subject}, Member: platform.Member{ID: id, Roles: roles}}
	for _, u := range units {
		s.Units = append(s.Units, platform.Membership{Unit: u, Role: roles["mes"]})
	}
	return s
}

// Seed gives the ERP its books in CNY, the demo chart of accounts, the month
// of now open, and the plant's products with their standard costs: steel, and
// the pump housing and valve body made of it. controller holds the platform's
// administration and the ERP's controller role.
func Seed(t *platformserver.Tenant, controller string, now time.Time) error {
	m, ok := t.Member(controller)
	if !ok {
		return fmt.Errorf("seed: no member %s", controller)
	}
	submit := func(authority, schema, typ, id string, payload any) error {
		raw, _ := json.Marshal(payload)
		_, err := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: authority, IdempotencyKey: "seed:" + schema + ":" + id,
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return fmt.Errorf("seed %s %s: %v", schema, id, err)
		}
		return nil
	}
	if err := submit(platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType,
		platformserver.PlatformApp+"/"+platformserver.SettingCurrency, map[string]string{"value": "CNY"}); err != nil {
		return err
	}
	for _, a := range erp.Chart() {
		if err := submit(erp.ID, erp.AccountType+".create", erp.AccountType, a.ID, map[string]string{"name": a.Name, "kind": a.Kind}); err != nil {
			return err
		}
	}
	if err := submit(erp.ID, erp.SchemaPeriodOpen, erp.PeriodType, now.Format("2006-01"), map[string]any{}); err != nil {
		return err
	}
	cny := func(v int64) map[string]any { return map[string]any{"amount": v, "currency": "CNY"} }
	steel := func(kg float64) []any { return []any{map[string]any{"product": "M-STEEL", "quantity": kg}} }
	for _, p := range []struct {
		id      string
		product map[string]any
	}{
		{"M-STEEL", map[string]any{"name": "Steel", "kind": "material", "unit": "kg", "cost": cny(500)}},
		{"P-100", map[string]any{"name": "Pump housing", "kind": "finished", "unit": "pcs", "cost": cny(1200), "components": steel(2)}},
		{"P-200", map[string]any{"name": "Valve body", "kind": "finished", "unit": "pcs", "cost": cny(800), "components": steel(1.5)}},
	} {
		if err := submit(erp.ID, erp.ProductType+".create", erp.ProductType, p.id, p.product); err != nil {
			return err
		}
	}
	return nil
}
