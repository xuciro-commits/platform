package manufacturing

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"erp"
	"mes"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A shop order lists its released SFCs under the declared inverse relation "sfcs"
// (ADR-0040 21b D1). A member with no MES role cannot read the order.
func TestNamedRelationOnShopOrder(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	seatSup := Seat("sup", "sup-1", map[string]string{mes.ID: string(mes.Supervisor), erp.ID: erp.Controller, platformserver.PlatformApp: platformserver.Admin}, "plant-sz")
	seatOther := Seat("other", "other-1", map[string]string{erp.ID: erp.Accountant}, "plant-sz")
	tn, err := NewTenant("plant-sz", erp.New("plant-sz"), seatSup, seatOther)
	if err != nil {
		t.Fatal(err)
	}
	if err := Seed(tn, "sup-1", now); err != nil {
		t.Fatal(err)
	}
	sup, _ := tn.Member("sup-1")
	other, _ := tn.Member("other-1")

	raw, _ := json.Marshal(map[string]any{"product": "P-100", "quantity": 4, "sfcs": 2})
	if _, err := tn.Submit(sup, &pb.Submission{
		TenantId: tn.ID, PrincipalId: sup.ID, Authority: mes.ID,
		IdempotencyKey: "rel-so-1", Target: &pb.EntityRef{Type: mes.OrderType, Id: "SO-1"},
		Schema: &pb.SchemaRef{Name: mes.SchemaRelease, Version: 1}, Payload: raw,
	}, now); err != nil {
		t.Fatalf("release shop order: %v", err)
	}

	view, kerr := tn.RecordOf(sup, mes.OrderType, "SO-1", now)
	if kerr != nil {
		t.Fatalf("record of SO-1: %v", kerr)
	}
	found := false
	for _, r := range view.Related {
		if r.Type == mes.SFCType {
			found = true
			if r.Relation != "sfcs" {
				t.Errorf("got relation %q, want %q", r.Relation, "sfcs")
			}
			if r.Total != 2 {
				t.Errorf("got total %d, want 2", r.Total)
			}
		}
	}
	if !found {
		t.Fatalf("view.Related missing mes.sfc: %+v", view.Related)
	}

	// Member with no MES role gets an error (not found/denied) reading the shop order.
	if _, kerr := tn.RecordOf(other, mes.OrderType, "SO-1", now); kerr == nil {
		t.Fatal("expected error reading SO-1 without MES role, got nil")
	}
}

// A builder object "inspection" refers to mes.order, and "finding" refers to
// "inspection". Taking the inspection's "fail" action as an operator creates a
// finding with its reference set under the same change, verified by CheckReplay
// (ADR-0040 21c D2).
func TestInspectionActionCreatesFinding(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	seatSup := Seat("sup", "sup-1", map[string]string{
		mes.ID: string(mes.Supervisor), erp.ID: erp.Controller,
		platformserver.PlatformApp: platformserver.Admin, build.ID: build.Builder,
	}, "plant-sz")
	seatOp := Seat("op", "op-1", map[string]string{
		mes.ID: string(mes.Operator), build.ID: build.User,
	}, "plant-sz")

	var journal []platformserver.Entry
	compose := func() *platformserver.Tenant {
		tn, err := NewTenant("plant-sz", erp.New("plant-sz"), seatSup, seatOp)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	if err := Seed(tn, "sup-1", now); err != nil {
		t.Fatal(err)
	}
	sup, _ := tn.Member("sup-1")
	op, _ := tn.Member("op-1")

	keys := 0
	do := func(m platform.Member, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(m, &pb.Submission{
			TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID,
			IdempotencyKey: fmt.Sprint("m-act-", keys), Target: &pb.EntityRef{Type: typ, Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw,
		}, now)
		if err != nil {
			return err.Code.String() + ": " + err.Message
		}
		return "ok"
	}
	must := func(got string) {
		t.Helper()
		if got != "ok" {
			t.Fatal(got)
		}
	}

	inspectionType := build.TypeOf("inspection")
	findingType := build.TypeOf("finding")

	// 1. Create and publish Inspection object referencing mes.order
	must(do(sup, build.ObjectType+".create", build.ObjectType, "O-INSP", map[string]any{
		"name": "inspection", "title": "Inspection",
		"fields": []map[string]any{
			{"name": "order", "title": "Order", "type": "reference", "ref": mes.OrderType, "inverse": "inspections"},
			{"name": "result", "title": "Result", "type": "text"},
		},
		"states": []map[string]any{
			{"name": "open", "title": "Open"},
			{"name": "failed", "title": "Failed"},
		},
	}))
	must(do(sup, build.SchemaPublish, build.ObjectType, "O-INSP", map[string]any{}))

	// 2. Create and publish Finding object referencing inspection with inverse "findings"
	must(do(sup, build.ObjectType+".create", build.ObjectType, "O-FIND", map[string]any{
		"name": "finding", "title": "Finding",
		"fields": []map[string]any{
			{"name": "inspection", "title": "Inspection", "type": "reference", "ref": inspectionType, "inverse": "findings"},
			{"name": "detail", "title": "Detail", "type": "text", "required": true},
		},
	}))
	must(do(sup, build.SchemaPublish, build.ObjectType, "O-FIND", map[string]any{}))

	// 3. Edit and publish Inspection to add "fail" action creating finding
	must(do(sup, build.ObjectType+".edit", build.ObjectType, "O-INSP", map[string]any{
		"name": "inspection", "title": "Inspection",
		"fields": []map[string]any{
			{"name": "order", "title": "Order", "type": "reference", "ref": mes.OrderType, "inverse": "inspections"},
			{"name": "result", "title": "Result", "type": "text"},
		},
		"states": []map[string]any{
			{"name": "open", "title": "Open"},
			{"name": "failed", "title": "Failed"},
		},
		"actions": []map[string]any{
			{
				"name": "fail", "title": "Fail", "from": []string{"open"}, "to": "failed",
				"inputs": []map[string]any{{"name": "detail", "title": "Detail", "type": "text"}},
				"creates": []map[string]any{
					{"object": findingType, "via": "inspection", "sets": []map[string]any{{"field": "detail", "from": "detail"}}},
				},
			},
		},
	}))
	must(do(sup, build.SchemaPublish, build.ObjectType, "O-INSP", map[string]any{}))

	// 3. Release shop order SO-1 so the reference target exists
	rawSO, _ := json.Marshal(map[string]any{"product": "P-100", "quantity": 4, "sfcs": 2})
	if _, err := tn.Submit(sup, &pb.Submission{
		TenantId: tn.ID, PrincipalId: sup.ID, Authority: mes.ID,
		IdempotencyKey: "so-insp-1", Target: &pb.EntityRef{Type: mes.OrderType, Id: "SO-1"},
		Schema: &pb.SchemaRef{Name: mes.SchemaRelease, Version: 1}, Payload: rawSO,
	}, now); err != nil {
		t.Fatalf("release shop order: %v", err)
	}

	// 4. Create inspection on shop order SO-1 as operator
	must(do(op, inspectionType+".create", inspectionType, "INSP-1", map[string]any{
		"order": "SO-1",
	}))

	// 5. Operator takes "fail" action with finding detail
	must(do(op, inspectionType+".fail", inspectionType, "INSP-1", map[string]any{
		"detail": "housing tolerance exceeded",
	}))

	// 5. Assert inspection is in failed state
	view, kerr := tn.RecordOf(op, inspectionType, "INSP-1", now)
	if kerr != nil {
		t.Fatalf("record of INSP-1: %v", kerr)
	}
	raw, _ := json.Marshal(view.Record)
	var r struct{ State string }
	json.Unmarshal(raw, &r)
	if r.State != "failed" {
		t.Fatalf("inspection state = %q, want failed", r.State)
	}

	// 6. Assert finding exists with its reference set to the inspection
	page, err := tn.Records(op, findingType, platform.Query{}, now)
	if err != nil {
		t.Fatalf("query findings: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("got %d findings, want 1", page.Total)
	}
	var finding struct {
		Inspection string `json:"inspection"`
		Detail     string `json:"detail"`
	}
	fRaw, _ := json.Marshal(page.Records[0])
	json.Unmarshal(fRaw, &finding)
	if finding.Inspection != "INSP-1" {
		t.Errorf("finding.inspection = %q, want INSP-1", finding.Inspection)
	}
	if finding.Detail != "housing tolerance exceeded" {
		t.Errorf("finding.detail = %q, want 'housing tolerance exceeded'", finding.Detail)
	}

	// 7. Assert relation on inspection lists the finding
	hasFindingRel := false
	for _, rel := range view.Related {
		if rel.Type == findingType && rel.Relation == "findings" && rel.Total == 1 {
			hasFindingRel = true
		}
	}
	if !hasFindingRel {
		t.Fatalf("inspection view.Related missing findings relation: %+v", view.Related)
	}

	// 8. CheckReplay
	platformserver.CheckReplay(t, tn, journal, compose)
}
