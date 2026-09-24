package mes

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
)

// #101: a finished order is confirmed to the ERP as an outbound effect; the
// ERP's answer comes back as an observation on the order (ADR-0014 D4), and a
// refusal reaches the line's supervisors. The journal replays all of it without
// calling the ERP (newPlant's cleanup).
func TestOrderConfirmedToTheERP(t *testing.T) {
	calls := 0
	erpAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var c struct{ Data Confirmation }
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &c)
		w.Header().Set("Content-Type", "application/json")
		if c.Data.Planned == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"error":"no planned order to confirm against"}`))
			return
		}
		fmt.Fprintf(w, `{"confirmation":"CONF-%s-%d"}`, c.Data.Planned, c.Data.Yield)
	}))
	defer erpAPI.Close()
	p := newPlant(t)
	p.tenant.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	admin := platformserver.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin}}
	raw, _ := json.Marshal(map[string]any{"url": erpAPI.URL, "secret": "erp", "effects": []string{"mes/" + EffectConfirmation}, "allowPrivate": true})
	if _, err := p.tenant.Submit(admin, &pb.Submission{TenantId: tenant, PrincipalId: "admin", Authority: platformserver.PlatformApp, IdempotencyKey: "e1",
		Target: &pb.EntityRef{Type: platformserver.EndpointType, Id: "erp-api"}, Schema: &pb.SchemaRef{Name: platformserver.SchemaEndpointAdd, Version: 1}, Payload: raw}, t0); err != nil {
		t.Fatal(err)
	}
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, PlannedPage{CursorTo: "page-1", Orders: []PlannedOrder{{ERPID: "PO-9001", Product: "P-100", Quantity: 4}}}, t0)), "<nil>")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 4, SFCs: 2, Planned: "PO-9001"}, p.Planned()[0].FactID), "ok")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-2", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok")
	run := func(id string) {
		for _, resource := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
			expect(t, submit(p, op1, SchemaStart, SFCType, id, sfcPayload{Resource: resource}), "ok")
			expect(t, submit(p, op1, SchemaComplete, SFCType, id, sfcPayload{}), "ok")
		}
	}
	order := func(id string) Order {
		for _, o := range p.Orders() {
			if o.ID == id {
				return o
			}
		}
		return Order{}
	}
	run("SO-1-001")
	expect(t, order("SO-1").ERP, "") // one SFC still open
	// The second lot is scrapped by two signatures; the order has ended.
	expect(t, submit(p, op1, SchemaNC, SFCType, "SO-1-002", sfcPayload{Code: "POROSITY"}), "ok")
	expect(t, submit(p, qa1, SchemaSign, SFCType, "SO-1-002", signPayload{Action: "scrap", Meaning: "reviewed"}), "ok")
	expect(t, submit(p, qa2, SchemaSign, SFCType, "SO-1-002", signPayload{Action: "scrap", Meaning: "approved"}), "ok")
	expect(t, order("SO-1").ERP, "sent")
	p.tenant.Dispatch(t0)
	o := order("SO-1")
	expect(t, o.ERP+" "+o.Confirmation, "confirmed CONF-PO-9001-2") // yield 2 of 4
	last := p.facts.Records(tenant)[len(p.facts.Records(tenant))-1].GetFact()
	expect(t, last.GetSchema().GetName()+" "+last.GetProvenance().GetConnectorId(), schemaAnswer+" erp-api")
	// An order the ERP did not plan is refused; the supervisors hear of it.
	run("SO-2-001")
	p.tenant.Dispatch(t0)
	o = order("SO-2")
	expect(t, o.ERP+": "+o.ERPDetail, "refused: no planned order to confirm against")
	notes, _ := p.tenant.Read(sup.Member, "notifications")
	expect(t, notes.([]platformserver.Notification)[0].Title, "ERP refused the confirmation of SO-2")
	p.tenant.Dispatch(t0) // settled effects are not sent again
	expect(t, fmt.Sprint(calls), "2")
}
