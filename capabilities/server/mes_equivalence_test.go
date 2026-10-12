package platformserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
)

// TestBuilderMESEquivalence proves that the full MES domain model, business
// decision invariants, lot splitting, capability matching, step progression,
// 21 CFR Part 11 dual-signature disposition, automatic parent order completion
// with yield/scrap rollup, and application record routing can be constructed
// and executed purely through Builder definitions without native MES Go code
// (ADR-0098).
func TestBuilderMESEquivalence(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	makeTenant := func() *Tenant {
		return composeTenant(t, "mes-eq", []Seat{
			seatOf("builder", build.ID+":"+build.Builder),
			seatOf("sup", build.ID+":supervisor"),
			seatOf("op1", build.ID+":operator"),
			seatOf("qa1", build.ID+":quality"),
			seatOf("qa2", build.ID+":quality"),
		}, build.New("mes-eq"), work.New("mes-eq"), flow.New("mes-eq"))
	}
	tn := makeTenant()
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		journal = append(journal, e)
		return e.Body, nil
	}

	allAccess := []map[string]any{
		{"role": "supervisor", "read": "all", "create": true, "edit": true},
		{"role": "operator", "read": "all", "create": true, "edit": true},
		{"role": "quality", "read": "all", "create": true, "edit": true},
	}

	// 1. Publish Craft Master Data Objects: workcenter, resource (with tags), routing, operation (with tags + lines), product, planned.
	wcType := publishObject(t, tn, "builder", "OBJ-WC", map[string]any{
		"name": "workcenter", "title": "Work center", "access": allAccess,
		"fields": []map[string]any{
			{"name": "name", "title": "Name", "type": "text", "required": true, "search": true},
			{"name": "line", "title": "Line", "type": "text", "required": true},
		},
	}, now)

	resType := publishObject(t, tn, "builder", "OBJ-RES", map[string]any{
		"name": "resource", "title": "Resource", "access": allAccess,
		"fields": []map[string]any{
			{"name": "name", "title": "Name", "type": "text", "required": true, "search": true},
			{"name": "workcenter", "title": "Work center", "type": "reference", "ref": wcType, "required": true},
			{"name": "capabilities", "title": "Capabilities", "type": "tags"},
		},
	}, now)

	rtType := publishObject(t, tn, "builder", "OBJ-RT", map[string]any{
		"name": "routing", "title": "Routing", "access": allAccess,
		"fields": []map[string]any{
			{"name": "name", "title": "Name", "type": "text", "required": true, "search": true},
			{"name": "version", "title": "Version", "type": "integer", "required": true},
		},
	}, now)

	opType := publishObject(t, tn, "builder", "OBJ-OP", map[string]any{
		"name": "operation", "title": "Operation", "access": allAccess,
		"fields": []map[string]any{
			{"name": "routing", "title": "Routing", "type": "reference", "ref": rtType, "inverse": "operations", "required": true},
			{"name": "number", "title": "Number", "type": "integer", "required": true},
			{"name": "name", "title": "Name", "type": "text", "required": true, "search": true},
			{"name": "workcenter", "title": "Work center", "type": "reference", "ref": wcType, "required": true},
			{"name": "requires", "title": "Requires", "type": "tags"},
			{"name": "parameters", "title": "Parameters", "type": "lines", "columns": []map[string]any{
				{"name": "name", "title": "Parameter", "type": "text", "required": true},
				{"name": "kind", "title": "Kind", "type": "choice", "choices": "setpoint,range,limit,note"},
				{"name": "unit", "title": "Unit", "type": "text"},
				{"name": "target", "title": "Target", "type": "decimal"},
				{"name": "min", "title": "Min", "type": "decimal"},
				{"name": "max", "title": "Max", "type": "decimal"},
			}},
		},
	}, now)

	prodType := publishObject(t, tn, "builder", "OBJ-PROD", map[string]any{
		"name": "product", "title": "Product", "access": allAccess,
		"fields": []map[string]any{
			{"name": "name", "title": "Name", "type": "text", "required": true, "search": true},
			{"name": "routing", "title": "Routing", "type": "reference", "ref": rtType, "required": true},
		},
	}, now)

	planType := publishObject(t, tn, "builder", "OBJ-PLAN", map[string]any{
		"name": "planned", "title": "Planned order", "access": allAccess,
		"fields": []map[string]any{
			{"name": "number", "title": "Number", "type": "text", "required": true, "search": true},
			{"name": "product", "title": "Product", "type": "reference", "ref": prodType, "required": true},
			{"name": "quantity", "title": "Quantity", "type": "integer", "required": true},
		},
	}, now)

	// 2. Publish Order first (so SFC can reference it), then SFC, then republish Order with the `release` creation action creating SFCs.
	orderBase := map[string]any{
		"name": "order", "title": "Shop order", "access": allAccess,
		"states": []map[string]any{
			{"name": "released", "title": "Released", "tone": "info"},
			{"name": "completed", "title": "Completed", "tone": "success"},
		},
		"fields": []map[string]any{
			{"name": "product", "title": "Product", "type": "reference", "ref": prodType, "required": true},
			{"name": "routing", "title": "Routing", "type": "reference", "ref": rtType},
			{"name": "routingversion", "title": "Routing version", "type": "integer"},
			{"name": "quantity", "title": "Quantity", "type": "integer", "required": true},
			{"name": "planned", "title": "Planned order", "type": "reference", "ref": planType},
			{"name": "yield", "title": "Yield", "type": "integer"},
			{"name": "scrap", "title": "Scrap", "type": "integer"},
		},
	}
	orderType := publishObject(t, tn, "builder", "OBJ-ORDER", orderBase, now)

	sfcType := publishObject(t, tn, "builder", "OBJ-SFC", map[string]any{
		"name": "sfc", "title": "SFC", "access": allAccess,
		"states": []map[string]any{
			{"name": "queued", "title": "Queued", "tone": "info"},
			{"name": "active", "title": "In work", "tone": "warning"},
			{"name": "hold", "title": "On hold", "tone": "danger"},
			{"name": "done", "title": "Done", "tone": "success"},
			{"name": "scrapped", "title": "Scrapped", "tone": "neutral"},
		},
		"fields": []map[string]any{
			{"name": "order", "title": "Order", "type": "reference", "ref": orderType, "inverse": "sfcs", "required": true},
			{"name": "product", "title": "Product", "type": "reference", "ref": prodType, "required": true},
			{"name": "routing", "title": "Routing", "type": "reference", "ref": rtType, "required": true},
			{"name": "routingversion", "title": "Routing version", "type": "integer", "required": true},
			{"name": "operation", "title": "Operation", "type": "reference", "ref": opType, "required": true},
			{"name": "quantity", "title": "Quantity", "type": "integer", "required": true},
			{"name": "resource", "title": "Resource", "type": "reference", "ref": resType},
			{"name": "ncs", "title": "Nonconformances", "type": "lines", "columns": []map[string]any{
				{"name": "operation", "title": "Operation", "type": "text"},
				{"name": "code", "title": "Code", "type": "text"},
				{"name": "by", "title": "By", "type": "text"},
			}},
			{"name": "signatures", "title": "Signatures", "type": "lines", "columns": []map[string]any{
				{"name": "action", "title": "Action", "type": "text"},
				{"name": "meaning", "title": "Meaning", "type": "text"},
				{"name": "by", "title": "By", "type": "text"},
			}},
		},
		"actions": []map[string]any{
			{
				"name": "start", "title": "Start operation", "from": []string{"queued"}, "to": "active", "roles": []string{"operator"},
				"inputs": []map[string]any{
					{"name": "resource", "title": "Resource", "type": "reference", "ref": resType, "required": true, "filter": []map[string]any{
						{"field": "workcenter", "operator": "=", "valueField": "record.operation.workcenter", "message": "Resource belongs to another work center"},
						{"field": "capabilities", "operator": "contains all", "valueField": "record.operation.requires", "message": "Resource lacks required capabilities"},
					}},
				},
				"conditions": []map[string]any{
					{"field": "input.resource.workcenter", "operator": "=", "valueField": "record.operation.workcenter", "message": "Resource is not in the operation's work center"},
					{"field": "input.resource.capabilities", "operator": "contains all", "valueField": "record.operation.requires", "message": "Resource lacks required capabilities for this operation"},
				},
				"sets": []map[string]any{
					{"field": "resource", "from": "input.resource"},
				},
			},
			{
				"name": "complete", "title": "Complete operation", "from": []string{"active"}, "to": "done", "roles": []string{"operator"},
				"sets": []map[string]any{
					{"field": "resource", "from": "="},
				},
				"branches": []map[string]any{
					{
						"when": []map[string]any{{"field": "step:next(record.routing.operations,number,record.operation)", "operator": "not empty"}},
						"to":   "queued",
						"sets": []map[string]any{{"field": "operation", "from": "step:next(record.routing.operations,number,record.operation)"}},
					},
				},
				"updates": []map[string]any{
					{
						"via": "order",
						"when": []map[string]any{
							{"quantifier": "all", "collection": "sfcs", "where": []map[string]any{{"field": "state", "operator": "in", "value": "done,scrapped"}}},
						},
						"to": "completed",
						"sets": []map[string]any{
							{"field": "yield", "from": "rollup:sum(sfcs.quantity?state=done)"},
							{"field": "scrap", "from": "rollup:sum(sfcs.quantity?state=scrapped)"},
						},
					},
				},
			},
			{
				"name": "nc", "title": "Log nonconformance", "from": []string{"queued", "active"}, "to": "hold", "roles": []string{"operator", "quality"},
				"inputs": []map[string]any{
					{"name": "code", "title": "NC code", "type": "choice", "choices": "POROSITY,DIMENSION,SURFACE,LEAK", "required": true},
				},
				"sets": []map[string]any{
					{"field": "resource", "from": "="},
					{"field": "ncs", "from": "append:operation=record.operation,code=input.code,by=$me"},
				},
			},
			{
				"name": "sign", "title": "Sign disposition", "from": []string{"hold"}, "to": "hold", "roles": []string{"quality"},
				"inputs": []map[string]any{
					{"name": "action", "title": "Disposition", "type": "choice", "choices": "rework,scrap,use-as-is", "required": true},
					{"name": "meaning", "title": "Meaning", "type": "choice", "choices": "reviewed,approved", "required": true},
					{"name": "reworkoperation", "title": "Rework operation", "type": "reference", "ref": opType, "required": true, "when": "action=rework"},
				},
				"conditions": []map[string]any{
					{"quantifier": "none", "collection": "signatures", "where": []map[string]any{{"field": "by", "operator": "=", "value": "$me"}}, "message": "One signature per person"},
					{"quantifier": "none", "collection": "signatures", "where": []map[string]any{{"field": "action", "operator": "=", "valueField": "input.action"}, {"field": "meaning", "operator": "=", "valueField": "input.meaning"}}, "message": "Signature meaning already recorded for this disposition"},
					{"when": map[string]any{"field": "input.action", "operator": "=", "value": "rework"}, "field": "input.reworkoperation.number", "operator": "<=", "valueField": "record.operation.number", "message": "Cannot rework to a future operation"},
				},
				"sets": []map[string]any{
					{"field": "signatures", "from": "append:action=input.action,meaning=input.meaning,by=$me"},
				},
				"branches": []map[string]any{
					{
						"when": []map[string]any{
							{"quantifier": "count", "collection": "signatures", "where": []map[string]any{{"field": "action", "operator": "=", "valueField": "input.action"}}, "operator": ">=", "value": "2"},
							{"field": "input.action", "operator": "=", "value": "rework"},
						},
						"to":   "queued",
						"sets": []map[string]any{{"field": "operation", "from": "input.reworkoperation"}, {"field": "signatures", "from": "="}},
					},
					{
						"when": []map[string]any{
							{"quantifier": "count", "collection": "signatures", "where": []map[string]any{{"field": "action", "operator": "=", "valueField": "input.action"}}, "operator": ">=", "value": "2"},
							{"field": "input.action", "operator": "=", "value": "scrap"},
						},
						"to":   "scrapped",
						"sets": []map[string]any{{"field": "signatures", "from": "="}},
					},
					{
						"when": []map[string]any{
							{"quantifier": "count", "collection": "signatures", "where": []map[string]any{{"field": "action", "operator": "=", "valueField": "input.action"}}, "operator": ">=", "value": "2"},
							{"field": "input.action", "operator": "=", "value": "use-as-is"},
						},
						"to":   "queued",
						"sets": []map[string]any{{"field": "signatures", "from": "="}},
					},
				},
				"updates": []map[string]any{
					{
						"via": "order",
						"when": []map[string]any{
							{"quantifier": "all", "collection": "sfcs", "where": []map[string]any{{"field": "state", "operator": "in", "value": "done,scrapped"}}},
						},
						"to": "completed",
						"sets": []map[string]any{
							{"field": "yield", "from": "rollup:sum(sfcs.quantity?state=done)"},
							{"field": "scrap", "from": "rollup:sum(sfcs.quantity?state=scrapped)"},
						},
					},
				},
			},
		},
	}, now)

	// Add `release` custom creation action on Order that splits into N SFCs and checks `planned` uniqueness (`fits`).
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "OBJ-ORDER", map[string]any{
		"actions": []map[string]any{
			{
				"new": true, "name": "release", "title": "Release shop order", "to": "released", "roles": []string{"supervisor"},
				"inputs": []map[string]any{
					{"name": "product", "title": "Product", "type": "reference", "ref": prodType, "required": true},
					{"name": "quantity", "title": "Quantity", "type": "integer", "required": true},
					{"name": "sfcs", "title": "SFCs", "type": "integer", "required": true},
					{"name": "planned", "title": "Planned order", "type": "reference", "ref": planType, "required": true},
				},
				"conditions": []map[string]any{
					{"field": "input.planned.product", "operator": "=", "valueField": "input.product", "message": "Planned order is for another product"},
					{"field": "input.planned.quantity", "operator": ">=", "valueField": "input.quantity", "message": "Planned order quantity is fewer than requested"},
					{"quantifier": "none", "collection": orderType, "where": []map[string]any{
						{"field": "planned", "operator": "=", "valueField": "input.planned"},
						{"field": "id", "operator": "!=", "valueField": "record.id"},
					}, "message": "Planned order is already fulfilled by another shop order"},
				},
				"sets": []map[string]any{
					{"field": "product", "from": "input.product"},
					{"field": "quantity", "from": "input.quantity"},
					{"field": "planned", "from": "input.planned"},
					{"field": "routing", "from": "input.product.routing"},
					{"field": "routingversion", "from": "input.product.routing.version"},
				},
				"creates": []map[string]any{
					{
						"object": sfcType, "via": "order", "count": "input.sfcs",
						"sets": []map[string]any{
							{"field": "product", "from": "input.product"},
							{"field": "routing", "from": "input.product.routing"},
							{"field": "routingversion", "from": "input.product.routing.version"},
							{"field": "operation", "from": "step:first(input.product.routing.operations,number)"},
							{"field": "quantity", "from": "split:input.quantity:input.sfcs"},
						},
					},
				},
			},
		},
	}, now)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "OBJ-ORDER", map[string]any{}, now)

	// Publish application with `opens` mapping for custom record view routing (ADR-0098 D5).
	decide(t, tn, "builder", build.ID, "build.app.create", build.AppType, "APP-MES", map[string]any{
		"name": "mesapp", "title": "Zero-Code MES", "icon": "factory",
		"pages": []string{"order", "sfc"},
		"opens": map[string]string{sfcType: "sfc", orderType: "order"},
	}, now)
	decide(t, tn, "builder", build.ID, "build.app.publish", build.AppType, "APP-MES", map[string]any{}, now)

	// 3. Seed Master Data records through standard record creation actions (no hardcoded Go structs!).
	decide(t, tn, "sup", build.ID, wcType+".create", wcType, "WC-CAST", map[string]any{"name": "Casting Center", "line": "L1"}, now)
	decide(t, tn, "sup", build.ID, wcType+".create", wcType, "WC-MACH", map[string]any{"name": "Machining Center", "line": "L1"}, now)
	decide(t, tn, "sup", build.ID, wcType+".create", wcType, "WC-ASM", map[string]any{"name": "Assembly Center", "line": "L1"}, now)

	decide(t, tn, "sup", build.ID, resType+".create", resType, "CAST-1", map[string]any{"name": "Caster 1", "workcenter": "WC-CAST", "capabilities": []string{"casting"}}, now)
	decide(t, tn, "sup", build.ID, resType+".create", resType, "CNC-1", map[string]any{"name": "CNC Mill 1", "workcenter": "WC-MACH", "capabilities": []string{"milling", "drilling"}}, now)
	decide(t, tn, "sup", build.ID, resType+".create", resType, "ASM-1", map[string]any{"name": "Assembler 1", "workcenter": "WC-ASM", "capabilities": []string{"assembly", "torque"}}, now)
	decide(t, tn, "sup", build.ID, resType+".create", resType, "ASM-2", map[string]any{"name": "Assembler 2", "workcenter": "WC-ASM", "capabilities": []string{"assembly"}}, now)

	decide(t, tn, "sup", build.ID, rtType+".create", rtType, "RT-100", map[string]any{"name": "Pump Housing Routing", "version": 1}, now)
	decide(t, tn, "sup", build.ID, opType+".create", opType, "OP-10", map[string]any{
		"routing": "RT-100", "number": 10, "name": "Cast", "workcenter": "WC-CAST", "requires": []string{"casting"},
		"parameters": []map[string]any{{"name": "temp", "kind": "range", "unit": "C", "target": 700, "min": 680, "max": 720}},
	}, now)
	decide(t, tn, "sup", build.ID, opType+".create", opType, "OP-20", map[string]any{
		"routing": "RT-100", "number": 20, "name": "Machine", "workcenter": "WC-MACH", "requires": []string{"milling", "drilling"},
	}, now)
	decide(t, tn, "sup", build.ID, opType+".create", opType, "OP-30", map[string]any{
		"routing": "RT-100", "number": 30, "name": "Assemble", "workcenter": "WC-ASM", "requires": []string{"assembly", "torque"},
	}, now)

	decide(t, tn, "sup", build.ID, prodType+".create", prodType, "P-100", map[string]any{"name": "Pump Housing", "routing": "RT-100"}, now)
	decide(t, tn, "sup", build.ID, planType+".create", planType, "PO-1", map[string]any{"number": "PO-2026-01", "product": "P-100", "quantity": 10}, now)

	// 4. Supervisor releases Shop Order SO-1 (quantity 10 split into 3 SFCs -> 4, 3, 3) against PO-1.
	decide(t, tn, "sup", build.ID, orderType+".release", orderType, "SO-1", map[string]any{
		"product": "P-100", "quantity": 10, "sfcs": 3, "planned": "PO-1",
	}, now)

	// Releasing a second order against the same planned order PO-1 is refused by the `none` quantifier (`fits` invariant).
	if got := refuse(t, tn, "sup", build.ID, orderType+".release", orderType, "SO-2", map[string]any{
		"product": "P-100", "quantity": 5, "sfcs": 1, "planned": "PO-1",
	}, now); !strings.Contains(got, "already fulfilled") {
		t.Fatalf("expected duplicate planned order refusal, got %s", got)
	}

	readField := func(typ, id, field string) any {
		t.Helper()
		rec, ok := tn.Held(typ + "/" + id)
		if !ok {
			t.Fatalf("missing record %s/%s", typ, id)
		}
		raw, _ := json.Marshal(rec)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return m[field]
	}

	// Verify 10 split across 3 lots is 4, 3, 3 and all start at OP-10 in state queued.
	for i, wantQty := range []float64{4, 3, 3} {
		sfcID := []string{"SO-1-001", "SO-1-002", "SO-1-003"}[i]
		if got := readField(sfcType, sfcID, "quantity"); got != wantQty {
			t.Fatalf("%s quantity: got %v, want %v", sfcID, got, wantQty)
		}
		if got := readField(sfcType, sfcID, "operation"); got != "OP-10" {
			t.Fatalf("%s initial operation: got %v, want OP-10", sfcID, got)
		}
		if got := readField(sfcType, sfcID, "state"); got != "queued" {
			t.Fatalf("%s initial state: got %v, want queued", sfcID, got)
		}
	}

	// 5. Execute SO-1-001 through OP-10 -> OP-20 -> OP-30 -> done, verifying capability matching (`canDo`).
	// Wrong workcenter resource is refused.
	if got := refuse(t, tn, "op1", build.ID, sfcType+".start", sfcType, "SO-1-001", map[string]any{"resource": "CNC-1"}, now); got == "ok" {
		t.Fatal("expected starting OP-10 on CNC-1 to be refused")
	}
	decide(t, tn, "op1", build.ID, sfcType+".start", sfcType, "SO-1-001", map[string]any{"resource": "CAST-1"}, now)
	decide(t, tn, "op1", build.ID, sfcType+".complete", sfcType, "SO-1-001", map[string]any{}, now)
	if got := readField(sfcType, "SO-1-001", "operation"); got != "OP-20" {
		t.Fatalf("after OP-10 complete, expected OP-20, got %v", got)
	}

	decide(t, tn, "op1", build.ID, sfcType+".start", sfcType, "SO-1-001", map[string]any{"resource": "CNC-1"}, now)
	decide(t, tn, "op1", build.ID, sfcType+".complete", sfcType, "SO-1-001", map[string]any{}, now)
	if got := readField(sfcType, "SO-1-001", "operation"); got != "OP-30" {
		t.Fatalf("after OP-20 complete, expected OP-30, got %v", got)
	}

	// At OP-30 (WC-ASM, requires ["assembly", "torque"]), ASM-2 (only ["assembly"]) is refused!
	if got := refuse(t, tn, "op1", build.ID, sfcType+".start", sfcType, "SO-1-001", map[string]any{"resource": "ASM-2"}, now); got == "ok" {
		t.Fatal("expected ASM-2 lacking torque capability to be refused at OP-30")
	}
	decide(t, tn, "op1", build.ID, sfcType+".start", sfcType, "SO-1-001", map[string]any{"resource": "ASM-1"}, now)
	decide(t, tn, "op1", build.ID, sfcType+".complete", sfcType, "SO-1-001", map[string]any{}, now)
	if got := readField(sfcType, "SO-1-001", "state"); got != "done" {
		t.Fatalf("after last operation complete, expected done, got %v", got)
	}
	// Order SO-1 is still released because SO-1-002 and SO-1-003 are not finished.
	if got := readField(orderType, "SO-1", "state"); got != "released" {
		t.Fatalf("expected SO-1 to remain released, got %v", got)
	}

	// 6. Move SO-1-002 to OP-20, log NC, and execute 21 CFR Part 11 dual-signature rework back to OP-10.
	decide(t, tn, "op1", build.ID, sfcType+".start", sfcType, "SO-1-002", map[string]any{"resource": "CAST-1"}, now)
	decide(t, tn, "op1", build.ID, sfcType+".complete", sfcType, "SO-1-002", map[string]any{}, now)
	decide(t, tn, "op1", build.ID, sfcType+".nc", sfcType, "SO-1-002", map[string]any{"code": "DIMENSION"}, now)
	if got := readField(sfcType, "SO-1-002", "state"); got != "hold" {
		t.Fatalf("expected SO-1-002 on hold after NC, got %v", got)
	}

	// Reworking to a future operation OP-30 (> OP-20) is refused (`reworkable` rule).
	if got := refuse(t, tn, "qa1", build.ID, sfcType+".sign", sfcType, "SO-1-002", map[string]any{
		"action": "rework", "meaning": "reviewed", "reworkoperation": "OP-30",
	}, now); !strings.Contains(got, "future operation") {
		t.Fatalf("expected rework to future operation OP-30 to be refused, got %s", got)
	}
	// First signature by qa1 (reviewed): stays in hold.
	decide(t, tn, "qa1", build.ID, sfcType+".sign", sfcType, "SO-1-002", map[string]any{
		"action": "rework", "meaning": "reviewed", "reworkoperation": "OP-10",
	}, now)
	if got := readField(sfcType, "SO-1-002", "state"); got != "hold" {
		t.Fatalf("expected SO-1-002 to stay in hold after 1 signature, got %v", got)
	}
	// qa1 cannot sign a second time.
	if got := refuse(t, tn, "qa1", build.ID, sfcType+".sign", sfcType, "SO-1-002", map[string]any{
		"action": "rework", "meaning": "approved", "reworkoperation": "OP-10",
	}, now); !strings.Contains(got, "One signature per person") {
		t.Fatalf("expected duplicate person refusal, got %s", got)
	}
	// qa2 signing with approved completes quorum -> moves SO-1-002 to queued at OP-10.
	decide(t, tn, "qa2", build.ID, sfcType+".sign", sfcType, "SO-1-002", map[string]any{
		"action": "rework", "meaning": "approved", "reworkoperation": "OP-10",
	}, now)
	if got := readField(sfcType, "SO-1-002", "state"); got != "queued" {
		t.Fatalf("expected SO-1-002 to return to queued after rework quorum, got %v", got)
	}
	if got := readField(sfcType, "SO-1-002", "operation"); got != "OP-10" {
		t.Fatalf("expected SO-1-002 operation to be OP-10 after rework, got %v", got)
	}

	// Complete SO-1-002 through OP-10 -> OP-20 -> OP-30 -> done.
	for _, res := range []string{"CAST-1", "CNC-1", "ASM-1"} {
		decide(t, tn, "op1", build.ID, sfcType+".start", sfcType, "SO-1-002", map[string]any{"resource": res}, now)
		decide(t, tn, "op1", build.ID, sfcType+".complete", sfcType, "SO-1-002", map[string]any{}, now)
	}
	if got := readField(sfcType, "SO-1-002", "state"); got != "done" {
		t.Fatalf("expected SO-1-002 done, got %v", got)
	}

	// 7. SO-1-003 (qty 3) logs NC at OP-10 and is scrapped by dual signature -> automatically completes parent Order SO-1 with yield=7, scrap=3!
	decide(t, tn, "op1", build.ID, sfcType+".nc", sfcType, "SO-1-003", map[string]any{"code": "POROSITY"}, now)
	decide(t, tn, "qa1", build.ID, sfcType+".sign", sfcType, "SO-1-003", map[string]any{"action": "scrap", "meaning": "reviewed"}, now)
	decide(t, tn, "qa2", build.ID, sfcType+".sign", sfcType, "SO-1-003", map[string]any{"action": "scrap", "meaning": "approved"}, now)

	if got := readField(sfcType, "SO-1-003", "state"); got != "scrapped" {
		t.Fatalf("expected SO-1-003 scrapped, got %v", got)
	}
	if got := readField(orderType, "SO-1", "state"); got != "completed" {
		t.Fatalf("expected parent order SO-1 to automatically transition to completed, got %v", got)
	}
	if got := readField(orderType, "SO-1", "yield"); got != float64(7) {
		t.Fatalf("expected parent order SO-1 yield=7, got %v", got)
	}
	if got := readField(orderType, "SO-1", "scrap"); got != float64(3) {
		t.Fatalf("expected parent order SO-1 scrap=3, got %v", got)
	}

	// 8. Verify full replay and snapshot consistency across the entire lifecycle.
	CheckReplay(t, tn, journal, makeTenant)
}
