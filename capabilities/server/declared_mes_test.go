package platformserver

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"platformserver/apps/enterprise"
	"platformserver/platform"
)

func TestDeclaredManufacturingPinsRoutingsAndMatchesEveryCapability(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("planner", "build:supervisor"), seatOf("operator", "build:operator"), seatOf("other", "build:operator"), seatOf("quality", "build:quality"), seatOf("quality2", "build:quality"), seatOf("outsider")}
	seats[1].Units = []platform.Membership{{Unit: "plant", Role: "supervisor"}}
	for _, i := range []int{2, 3} {
		seats[i].Units = []platform.Membership{{Unit: "L1", Role: "operator"}, {Unit: "L2", Role: "operator"}}
	}
	lineOne, lineTwo := seatOf("lineone", "build:operator"), seatOf("linetwo", "build:operator")
	lineOne.Units, lineTwo.Units = []platform.Membership{{Unit: "L1", Role: "operator"}}, []platform.Membership{{Unit: "L2", Role: "operator"}}
	plannerTwo := seatOf("plannertwo", "build:supervisor")
	plannerTwo.Units = []platform.Membership{{Unit: "L2", Role: "supervisor"}}
	seats = append(seats, lineOne, lineTwo, plannerTwo)
	organization := declaredPlantOrganization(seats, "L1", "L2")
	var entries []Entry
	tn := declaredIndustryTenant(t, "mes-declared", "manufacturing", seats, at, declaredFixtureOptions{Entries: &entries, Organization: &organization})
	currentUse := func(unit, ref string, want bool) {
		t.Helper()
		usage, failure := tn.EnterpriseUsage(memberOf(t, tn, "builder"), unit, "2026-10-23", at)
		if failure != nil {
			t.Fatal(failure)
		}
		for _, item := range usage.Items {
			if item.Ref == "record:"+ref {
				if item.Current != want || usage.CanDelete {
					t.Fatalf("enterprise use of %s: %+v", ref, usage)
				}
				return
			}
		}
		t.Fatalf("enterprise history lost %s: %+v", ref, usage)
	}
	create := func(kind, id string, values map[string]any) {
		t.Helper()
		decide(t, tn, "planner", "build", "build.mes"+kind+".create", "build.mes"+kind, id, values, at)
	}
	create("center", "assembly", map[string]any{"name": "Assembly", "line": "L1"})
	create("center", "machine", map[string]any{"name": "Machining", "line": "L2"})
	for _, capability := range []string{"torque", "safety"} {
		create("capability", capability, map[string]any{"name": capability, "code": capability})
	}
	for _, id := range []string{"qualified", "partial"} {
		create("resource", id, map[string]any{"name": id, "center": "assembly", "active": true})
		create("skill", id+"torque", map[string]any{"resource": id, "capability": "torque"})
	}
	create("resource", "cnc", map[string]any{"name": "CNC station", "center": "machine", "active": true})
	for _, capability := range []string{"torque", "safety"} {
		create("skill", "cnc"+capability, map[string]any{"resource": "cnc", "capability": capability})
	}
	create("skill", "qualifiedsafety", map[string]any{"resource": "qualified", "capability": "safety"})
	routing := func(id string, version int, firstName string) {
		t.Helper()
		center := "assembly"
		if version == 2 {
			center = "machine"
		}
		create("routing", id, map[string]any{"code": "R100", "name": "Assembly procedure", "version": version, "from": "2026-01-01"})
		create("operation", id+"op10", map[string]any{"routing": id, "number": 10, "name": firstName, "center": center})
		create("operation", id+"op20", map[string]any{"routing": id, "number": 20, "name": "Inspect", "center": center})
		decide(t, tn, "planner", "build", "build.mesoperation.edit", "build.mesoperation", id+"op10", map[string]any{"next": id + "op20"}, at)
		for _, capability := range []string{"torque", "safety"} {
			create("requirement", id+capability, map[string]any{"operation": id + "op10", "capability": capability})
		}
		decide(t, tn, "planner", "build", "build.mesrouting.edit", "build.mesrouting", id, map[string]any{"first": id + "op10"}, at)
		decide(t, tn, "planner", "build", "build.mesrouting.release", "build.mesrouting", id, nil, at)
	}
	routing("v1", 1, "Assemble with torque")
	order := func(id, route string, quantity int) {
		t.Helper()
		create("order", id, map[string]any{"number": id, "product": "Assembly P1", "routing": route, "quantity": quantity})
		create("batch", id+"batch", map[string]any{"order": id, "name": id + "-A", "routing": route, "operation": route + "op10", "quantity": quantity})
		if id == "old" {
			before := txnState(tn)
			if got := refuse(t, tn, "plannertwo", "build", "build.mesorder.release", "build.mesorder", id, nil, at); !strings.Contains(got, "POLICY_DENIED") || txnState(tn) != before {
				t.Fatal("a supervisor released work on another line")
			}
		}
		decide(t, tn, "planner", "build", "build.mesorder.release", "build.mesorder", id, nil, at)
	}
	order("old", "v1", 3)
	if _, failure := tn.RecordOf(memberOf(t, tn, "linetwo"), "build.mesbatch", "oldbatch", at); failure == nil {
		t.Fatal("the same operator role read another line's batch")
	}
	if _, failure := tn.RecordOf(memberOf(t, tn, "lineone"), "build.mesbatch", "oldbatch", at); failure != nil {
		t.Fatal("the original line member could not read its batch")
	}
	unchanged := txnState(tn)
	if got := refuse(t, tn, "linetwo", "build", "build.mesbatch.start", "build.mesbatch", "oldbatch", map[string]any{"operation": "v1op10", "resource": "qualified"}, at); !strings.Contains(got, "POLICY_DENIED") || txnState(tn) != unchanged {
		t.Fatal("a same-role member changed another line's production")
	}
	routing("v2", 2, "Assemble with new instructions")
	order("new", "v2", 2)
	if got := declaredRecord(t, tn, "operator", "build.mesbatch", "oldbatch", at); got["routingversion"] != float64(1) || got["operation"] != "v1op10" {
		t.Fatalf("new routing moved an old batch: %+v", got)
	}
	if got := declaredRecord(t, tn, "operator", "build.mesbatch", "newbatch", at); got["routingversion"] != float64(2) {
		t.Fatalf("new batch did not choose its published routing: %+v", got)
	}
	before := txnState(tn)
	if got := refuse(t, tn, "planner", "build", "build.mesoperation.edit", "build.mesoperation", "v1op10", map[string]any{"name": "Changed in flight"}, at); !strings.Contains(got, "declared records") || txnState(tn) != before {
		t.Fatalf("fixed old operation changed: %s", got)
	}
	for _, input := range []map[string]any{{"operation": "v1op10", "resource": "partial"}, {"operation": "v2op10", "resource": "qualified"}} {
		if got := refuse(t, tn, "operator", "build", "build.mesbatch.start", "build.mesbatch", "oldbatch", input, at); got == "ok" || txnState(tn) != before {
			t.Fatalf("missing capability or wrong pinned operation started a batch: %s", got)
		}
	}
	if got := refuse(t, tn, "outsider", "build", "build.mesbatch.start", "build.mesbatch", "oldbatch", map[string]any{"operation": "v1op10", "resource": "qualified"}, at); got == "ok" {
		t.Fatal("an ungranted member started production")
	}
	decide(t, tn, "operator", "build", "build.mesbatch.start", "build.mesbatch", "oldbatch", map[string]any{"operation": "v1op10", "resource": "qualified"}, at)
	if got := refuse(t, tn, "other", "build", "build.mesbatch.complete", "build.mesbatch", "oldbatch", nil, at); got == "ok" {
		t.Fatal("another operator completed work assigned to the original operator")
	}
	decide(t, tn, "operator", "build", "build.mesbatch.complete", "build.mesbatch", "oldbatch", nil, at)
	if got := declaredRecord(t, tn, "operator", "build.mesbatch", "oldbatch", at); got["state"] != "queued" || got["operation"] != "v1op20" {
		t.Fatalf("business operation advancement: %+v", got)
	}
	decide(t, tn, "operator", "build", "build.mesbatch.start", "build.mesbatch", "oldbatch", map[string]any{"operation": "v1op20", "resource": "qualified"}, at)
	decide(t, tn, "operator", "build", "build.mesbatch.complete", "build.mesbatch", "oldbatch", nil, at)
	if got := declaredRecord(t, tn, "operator", "build.mesbatch", "oldbatch", at); got["state"] != "done" || got["good"] != float64(3) {
		t.Fatalf("finished batch quantities: %+v", got)
	}
	for _, operation := range []string{"v2op10", "v2op20"} {
		decide(t, tn, "operator", "build", "build.mesbatch.start", "build.mesbatch", "newbatch", map[string]any{"operation": operation, "resource": "cnc"}, at)
		decide(t, tn, "operator", "build", "build.mesbatch.complete", "build.mesbatch", "newbatch", nil, at)
	}
	if got := declaredRecord(t, tn, "operator", "build.mesbatch", "newbatch", at); got["state"] != "done" || got["good"] != float64(2) {
		t.Fatalf("the independently configured work center could not run: %+v", got)
	}
	create("order", "qualityorder", map[string]any{"number": "Q100", "product": "Machined assembly", "routing": "v2", "quantity": 5})
	create("batch", "rework", map[string]any{"order": "qualityorder", "name": "Q100-A", "routing": "v2", "operation": "v2op10", "quantity": 3})
	if got := refuse(t, tn, "planner", "build", "build.mesorder.release", "build.mesorder", "qualityorder", nil, at); got == "ok" {
		t.Fatal("an incomplete quantity split was released")
	}
	create("batch", "scrap", map[string]any{"order": "qualityorder", "name": "Q100-B", "routing": "v2", "operation": "v2op10", "quantity": 2})
	decide(t, tn, "planner", "build", "build.mesorder.release", "build.mesorder", "qualityorder", nil, at)
	currentUse("L2", "build.mesorder/qualityorder", true)
	currentUse("L2", "build.mesbatch/rework", true)
	if got := refuse(t, tn, "planner", "build", "build.mesbatch.create", "build.mesbatch", "late", map[string]any{"order": "qualityorder", "name": "late", "routing": "v2", "operation": "v2op10", "quantity": 1}, at); got == "ok" {
		t.Fatal("a released quantity split accepted another batch")
	}
	decide(t, tn, "operator", "build", "build.mesbatch.start", "build.mesbatch", "rework", map[string]any{"operation": "v2op10", "resource": "cnc"}, at)
	tasks, taskErr := tn.Records(memberOf(t, tn, "operator"), platform.WorkTaskType, platform.Query{Domain: platform.Raw([]any{[]any{"ref", "=", "build.mesbatch/rework"}, []any{"state", "=", "open"}})}, at)
	var taskRows []struct{ Candidates []string }
	_ = json.Unmarshal(platform.Raw(tasks.Records), &taskRows)
	if taskErr != nil || tasks.Total != 1 || len(taskRows) != 1 || len(taskRows[0].Candidates) != 1 || taskRows[0].Candidates[0] != "operator" {
		t.Fatalf("the real operator did not receive its own execution task: %s %v", platform.Raw(tasks), taskErr)
	}
	decide(t, tn, "operator", "build", "build.mesbatch.report", "build.mesbatch", "rework", map[string]any{"code": "Surface defect"}, at)
	for _, target := range []string{"v2op20", "v1op10"} {
		before = txnState(tn)
		if got := refuse(t, tn, "quality", "build", "build.mesbatch.review", "build.mesbatch", "rework", map[string]any{"decision": "rework", "target": target}, at); got == "ok" || txnState(tn) != before {
			t.Fatalf("a future or different routing operation became a rework target: %s", got)
		}
	}
	if got := refuse(t, tn, "operator", "build", "build.mesbatch.review", "build.mesbatch", "rework", map[string]any{"decision": "scrap"}, at); got == "ok" {
		t.Fatal("an operator could sign as quality")
	}
	decide(t, tn, "quality", "build", "build.mesbatch.review", "build.mesbatch", "rework", map[string]any{"decision": "rework", "target": "v2op10"}, at)
	before = txnState(tn)
	if got := refuse(t, tn, "quality", "build", "build.mesbatch.approve", "build.mesbatch", "rework", nil, at); got == "ok" || txnState(tn) != before {
		t.Fatal("one person supplied both signature meanings")
	}
	approve := submission(tn, memberOf(t, tn, "quality2"), "build", "build.mesbatch.approve", "build.mesbatch", "rework", map[string]any{})
	persist := tn.AcceptResult
	fail := true
	tn.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		if fail {
			fail = false
			return nil, errors.New("injected disposition append failure")
		}
		return persist(entry, key, hash)
	}
	before = txnState(tn)
	if _, failure := tn.Submit(memberOf(t, tn, "quality2"), approve, at); failure == nil || fail || txnState(tn) != before {
		t.Fatal("an unaccepted signature or disposition escaped its private decision")
	}
	accepted, failure := tn.Submit(memberOf(t, tn, "quality2"), approve, at)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	again, failure := tn.Submit(memberOf(t, tn, "quality2"), approve, at)
	if failure != nil || again.GetChangeId() != accepted.GetChangeId() {
		t.Fatal("the approval retry lost its original receipt")
	}
	if got := declaredRecord(t, tn, "operator", "build.mesbatch", "rework", at); got["state"] != "queued" || got["operation"] != "v2op10" {
		t.Fatalf("approved rework did not return to its actual business operation: %+v", got)
	}
	for _, operation := range []string{"v2op10", "v2op20"} {
		decide(t, tn, "operator", "build", "build.mesbatch.start", "build.mesbatch", "rework", map[string]any{"operation": operation, "resource": "cnc"}, at)
		decide(t, tn, "operator", "build", "build.mesbatch.complete", "build.mesbatch", "rework", nil, at)
	}
	decide(t, tn, "operator", "build", "build.mesbatch.report", "build.mesbatch", "scrap", map[string]any{"code": "Unrecoverable material"}, at)
	decide(t, tn, "quality2", "build", "build.mesbatch.review", "build.mesbatch", "scrap", map[string]any{"decision": "scrap"}, at)
	decide(t, tn, "quality", "build", "build.mesbatch.approve", "build.mesbatch", "scrap", nil, at)
	decide(t, tn, "planner", "build", "build.mesorder.complete", "build.mesorder", "qualityorder", nil, at)
	currentUse("L2", "build.mesorder/qualityorder", false)
	currentUse("L2", "build.mesbatch/rework", false)
	currentUse("L2", "build.mesbatch/scrap", false)
	if got := declaredRecord(t, tn, "operator", "build.mesorder", "qualityorder", at); got["state"] != "completed" || got["good"] != float64(3) || got["scrap"] != float64(2) {
		t.Fatalf("shop completion lost its original batch quantities: %+v", got)
	}
	signed, readErr := tn.Records(memberOf(t, tn, "quality"), "build.messignature", platform.Query{}, at)
	if readErr != nil || signed.Total != 4 {
		t.Fatalf("accepted signature facts: %s %v", platform.Raw(signed), readErr)
	}
	var signatures []map[string]any
	if err := json.Unmarshal(platform.Raw(signed.Records), &signatures); err != nil {
		t.Fatal(err)
	}
	actors := map[string]map[string]string{}
	for _, signature := range signatures {
		issue := signature["issue"].(string)
		if actors[issue] == nil {
			actors[issue] = map[string]string{}
		}
		actors[issue][signature["meaning"].(string)] = signature["actor"].(string)
		if signature["actor"] != signature["created"].(map[string]any)["by"] {
			t.Fatal("a signature actor differs from its actual authenticated creator")
		}
	}
	for _, pair := range actors {
		if len(pair) != 2 || pair["reviewed"] == pair["approved"] {
			t.Fatalf("signature meanings did not retain two people: %+v", pair)
		}
	}
	recovered := composeDeclaredTenant(t, tn.ID, seats, enterprise.New(tn.ID, organization))
	if err := recovered.Replay(entries); err != nil || snapshot(tn) != snapshot(recovered) {
		t.Fatalf("routing, operator or work changed during recovery: %v", err)
	}
}

func declaredPlantOrganization(seats []Seat, lines ...string) platform.OrgSeed {
	seed := platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}}, Units: []platform.Unit{{ID: "plant", Name: "Plant", Kind: "plant"}}, Memberships: Memberships(seats)}
	for _, line := range lines {
		seed.Units = append(seed.Units, platform.Unit{ID: line, Name: line, Kind: "line"})
		seed.Edges = append(seed.Edges, platform.Edge{Structure: "site", Unit: line, Parent: "plant"})
	}
	return seed
}
