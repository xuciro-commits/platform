package erplink

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
	"production"
)

func build(t *testing.T) *platformserver.Tenant {
	seat := func(id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}}
	}
	tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t",
		seat("admin", map[string]string{platformserver.PlatformApp: platformserver.Admin}),
		seat("erp", map[string]string{ID: Connector}), seat("pat", map[string]string{ID: Planner})), New("t"))
	if err == nil {
		err = tn.Connect(Poll("erp"))
	}
	if err != nil {
		t.Fatal(err)
	}
	tn.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	return tn
}

// ADR-0024 7d: the ERP's planned orders arrive by polling, exactly once per
// page; a confirmation through production.orders/1 goes to the ERP as an
// effect, and its answer — a confirmation number, or a refusal — shows on the
// order where a consumer reads it. A refused order is confirmed again under a
// new key; a stale answer changes nothing. Replay never calls the ERP.
func TestConfirmedToTheERP(t *testing.T) {
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var c struct{ Data Confirmation }
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &c)
		w.Header().Set("Content-Type", "application/json")
		if c.Data.Yield+c.Data.Scrap > 4 { // the fake ERP's own rule
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprintf(w, `{"error":"%s: more than the ERP has open"}`, c.Data.Order)
			return
		}
		fmt.Fprintf(w, `{"confirmation":"CONF-%s-%g"}`, c.Data.Order, c.Data.Yield)
	}))
	defer fake.Close()
	var journal []platformserver.Entry
	tn := build(t)
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	expect := func(what string, got any, want string) {
		t.Helper()
		if fmt.Sprint(got) != want {
			t.Fatalf("%s: got %v, want %s", what, got, want)
		}
	}
	poll := func(who string, page Page) string {
		raw, _ := json.Marshal(page)
		_, err := tn.Input(member(who), "planned-orders", raw, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	keys := 0
	confirm := func(who, id string, yield, scrap float64) string {
		keys++
		raw, _ := json.Marshal(production.Confirmation{ShopOrder: "SO-" + id, Yield: yield, Scrap: scrap})
		if _, _, err := tn.Invoke(member(who), production.ID, "confirm", id, raw, fmt.Sprint("k", keys), now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	orders := func() map[string]production.Order {
		out, err := tn.Query(member("pat"), production.ID, "orders")
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]production.Order{}
		for _, o := range out.([]production.Order) {
			byID[o.ID] = o
		}
		return byID
	}
	send := func() {
		now = now.Add(time.Second)
		tn.Dispatch(now)
	}

	page := Page{CursorTo: "p1", Orders: []Planned{{ID: "PO-9001", Product: "P-100", Quantity: 4, Due: "2026-10-09"}, {ID: "PO-9002", Product: "P-200", Quantity: 8}}}
	expect("a planner does not poll", poll("pat", page), "ERROR_CODE_POLICY_DENIED")
	expect("poll", poll("erp", page), "ok")
	expect("the same page again", poll("erp", page), "ERROR_CODE_CONFLICT")
	expect("planned", orders()["PO-9001"], "{PO-9001 PO-9001 P-100 4 2026-10-09 released   }")

	// No endpoint bound yet: the confirmation cannot reach the ERP.
	expect("confirm without an endpoint", confirm("pat", "PO-9001", 2, 2), "ok")
	expect("failed", orders()["PO-9001"].State+": "+orders()["PO-9001"].Detail, "failed: no endpoint is bound to ERP confirmations")
	raw, _ := json.Marshal(map[string]any{"url": fake.URL, "secret": "erp", "effects": []string{ID + "/" + EffectConfirmation}, "allowPrivate": true})
	if _, err := tn.Submit(member("admin"), &pb.Submission{TenantId: "t", PrincipalId: "admin", Authority: platformserver.PlatformApp, IdempotencyKey: "e1",
		Target: &pb.EntityRef{Type: platformserver.EndpointType, Id: "erp-api"}, Schema: &pb.SchemaRef{Name: platformserver.SchemaEndpointAdd, Version: 1}, Payload: raw}, now); err != nil {
		t.Fatal(err)
	}

	expect("unknown order", confirm("pat", "PO-404", 1, 0), "ERROR_CODE_NOT_FOUND")
	expect("more than planned", confirm("pat", "PO-9001", 4, 1), "ERROR_CODE_INVALID_ARGUMENT")
	expect("a connector does not confirm", confirm("erp", "PO-9001", 2, 2), "ERROR_CODE_POLICY_DENIED")
	expect("confirm", confirm("pat", "PO-9001", 2, 2), "ok")
	expect("sent", orders()["PO-9001"].State, "sent")
	expect("once in flight", confirm("pat", "PO-9001", 2, 2), "ERROR_CODE_CONFLICT")
	send()
	o := orders()["PO-9001"]
	expect("the ERP's number", o.State+" "+o.ShopOrder+" "+o.Confirmation, "confirmed SO-PO-9001 CONF-PO-9001-2")
	expect("confirmed once", confirm("pat", "PO-9001", 1, 0), "ERROR_CODE_CONFLICT")

	// The ERP refuses; the order is confirmed again, and the ERP accepts it.
	expect("confirm PO-9002", confirm("pat", "PO-9002", 5, 0), "ok")
	send()
	o = orders()["PO-9002"]
	expect("refused", o.State+": "+o.Detail, "refused: PO-9002: more than the ERP has open")
	expect("again", confirm("pat", "PO-9002", 4, 0), "ok")
	send()
	expect("confirmed again", orders()["PO-9002"].State+" "+orders()["PO-9002"].Confirmation, "confirmed CONF-PO-9002-4")
	send()
	expect("the ERP was called once per confirmation", calls, "3")
	history, _ := tn.RecordOf(member("pat"), OrderType, "PO-9002", now)
	expect("history", len(history.History), "5") // polled, sent, refused, sent again, confirmed

	// A later page changes the demand, not where an order stands.
	expect("next page", poll("erp", Page{CursorFrom: "p1", CursorTo: "p2", Orders: []Planned{{ID: "PO-9001", Product: "P-100", Quantity: 6}}}), "ok")
	expect("updated", orders()["PO-9001"].State+fmt.Sprint(" ", orders()["PO-9001"].Quantity), "confirmed 6")

	platformserver.CheckReplay(t, tn, journal, func() *platformserver.Tenant { return build(t) })
	expect("replay called no ERP", calls, "3")
}

func TestChinese(t *testing.T) {
	if missing := build(t).Untranslated(ID, "zh-CN"); len(missing) > 0 {
		t.Errorf("add to i18n/zh-CN.json: %q", missing)
	}
}
