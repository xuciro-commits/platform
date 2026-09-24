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
	"platformserver/platform"
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
	admin := platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin}}
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
	expect(t, notes.([]platform.Notification)[0].Title, "ERP refused the confirmation of SO-2")
	p.tenant.Dispatch(t0) // settled effects are not sent again
	expect(t, fmt.Sprint(calls), "2")

	// The supervisor corrects the refused order: it fulfils a planned order the
	// ERP sent since, and its confirmation goes again under a new key.
	type resend struct {
		Planned string `json:"planned,omitempty"`
	}
	expect(t, submit(p, op1, SchemaResend, OrderType, "SO-2", resend{}), "ERROR_CODE_POLICY_DENIED")
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-1", resend{}), "ERROR_CODE_CONFLICT") // confirmed already
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-2", resend{Planned: "PO-404"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, PlannedPage{CursorFrom: "page-1", CursorTo: "page-2", Orders: []PlannedOrder{{ERPID: "PO-9002", Product: "P-100", Quantity: 1}}}, t0)), "<nil>")
	expect(t, submit(p, sup, SchemaResend, OrderType, "SO-2", resend{Planned: "PO-9002"}), "ok")
	expect(t, order("SO-2").ERP, "sent")
	p.tenant.Dispatch(t0)
	o = order("SO-2")
	expect(t, fmt.Sprint(o.ERP, " ", o.Confirmation, " ", o.Resent, " ", calls), "confirmed CONF-PO-9002-1 1 3")
	var keys []string
	for _, e := range p.tenant.Effects(t0) {
		keys = append(keys, e.Key+":"+e.State)
	}
	expect(t, fmt.Sprint(keys), "[SO-2#2:delivered SO-2:rejected SO-1:delivered]")
}
