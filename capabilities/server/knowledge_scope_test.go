package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/files"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/relations"
	"platformserver/apps/work"
	"platformserver/platform"
)

// A passage must be at most as visible as the record it came from. A person
// who can open the subject does not automatically see another person's task or
// flow merely because it names that subject.
func TestKnowledgeAndContextScope(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tn := stockTenant(t, knowledge.New("t-1"), work.New("t-1"), flow.New("t-1"), files.New("t-1"), relations.New("t-1"))
	member := func(id string) platform.Member {
		m, _ := tn.app(PlatformApp).(*Console).Member(id)
		return m
	}
	for i, x := range []struct{ who, id, line string }{{"ana", "I1", "L1"}, {"bo", "I2", "L2"}} {
		body, _ := json.Marshal(map[string]string{"name": "Machine " + x.id, "line": x.line, "note": "hydraulic inspection " + x.id})
		_, err := tn.Submit(member(x.who), &pb.Submission{TenantId: "t-1", PrincipalId: x.who, Authority: "stock", IdempotencyKey: fmt.Sprint("scope-", i),
			Target: &pb.EntityRef{Type: "stock.item", Id: x.id}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1}, Payload: body}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{
		"ana": "stock.item/I1#note", "bo": "stock.item/I2#note",
		"lead": "stock.item/I1#note stock.item/I2#note", "op": "stock.item/I1#note", "boss": "",
	}
	for who, expected := range want {
		m := member(who)
		found := tn.Knowledge(&m, "", "hydraulic inspection", 10, now)
		var documents []string
		for _, p := range found {
			documents = append(documents, p.Document)
		}
		slices.Sort(documents)
		got := strings.Join(documents, " ")
		if got != expected {
			t.Errorf("%s knowledge: %q, want %q", who, got, expected)
		}
	}
	foreign := member("lead")
	foreign.Tenant = "t-2"
	if found := tn.Knowledge(&foreign, "", "hydraulic inspection", 10, now); len(found) != 0 {
		t.Errorf("other tenant found %+v", found)
	}
	if view, err := tn.Context(&foreign, "stock.item", "I1", now); err == nil {
		t.Errorf("other tenant read context %+v", view)
	}
	if found := tn.Search(&foreign, "Machine", now); len(found) != 0 {
		t.Errorf("other tenant searched %+v", found)
	}
	if found := tn.Knowledge(nil, "stock", "hydraulic inspection", 10, now); len(found) != 2 {
		t.Errorf("stock's automation found %+v", found)
	}
	up, _, uploadErr := tn.Upload(member("ana"), "coolant.txt", "", strings.NewReader("Coolant recirculation procedure."), now)
	if uploadErr != nil {
		t.Fatal(uploadErr)
	}
	fileBody, _ := json.Marshal(map[string]any{"hash": up.Hash, "name": up.Name, "contentType": up.ContentType, "size": up.Size, "target": "stock.item/I1"})
	_, fileErr := tn.Submit(member("ana"), &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: files.ID, IdempotencyKey: "scope-file",
		Target: &pb.EntityRef{Type: files.FileType, Id: "F1"}, Schema: &pb.SchemaRef{Name: files.SchemaAttach, Version: 1}, Payload: fileBody}, now)
	if fileErr != nil {
		t.Fatal(fileErr)
	}
	for _, x := range []struct {
		who     string
		visible bool
	}{{"ana", true}, {"bo", false}, {"lead", true}, {"op", true}, {"boss", false}} {
		m := member(x.who)
		found := tn.Knowledge(&m, "", "coolant recirculation", 5, now)
		if got := len(found) == 1 && found[0].Document == files.FileType+"/F1"; got != x.visible {
			t.Errorf("%s attachment knowledge: %+v, want visible=%t", x.who, found, x.visible)
		}
	}

	if err := tn.automation(flow.ID, false).PutAt(now, "test.flow", flow.FlowInstance{Record: platform.Record{ID: "F1"}, Key: "I1", Flow: "stock.private", State: "waiting", OnBehalf: "lead",
		Trace: []flow.TraceLine{{Detail: "private flow reason"}}}); err != nil {
		t.Fatal(err)
	}
	if err := tn.automation(work.ID, false).PutAt(now, "test.task", work.WorkTask{Record: platform.Record{ID: "T1"}, Ref: "stock.item/I1", Title: "Private task", Answer: "private answer", Candidates: []string{"lead"}, State: "open"}); err != nil {
		t.Fatal(err)
	}
	ref := func(id string) *pb.EntityRef { return &pb.EntityRef{Type: "stock.item", Id: id} }
	if err := tn.caller(member("lead"), tn.app(relations.ID), false).Link(ref("I1"), ref("I2"), "private-link", now); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		who                 string
		flows, tasks, links int
	}{{"ana", 0, 0, 0}, {"lead", 1, 1, 1}} {
		m := member(x.who)
		context, err := tn.Context(&m, "stock.item", "I1", now)
		if err != nil {
			t.Fatalf("%s context: %v", x.who, err)
		}
		if len(context.Flows) != x.flows || len(context.Tasks) != x.tasks || len(context.Links) != x.links {
			t.Errorf("%s context leaked or lost details: flows=%+v tasks=%+v links=%+v", x.who, context.Flows, context.Tasks, context.Links)
		}
	}
}

// The host's public record endpoint caps one page at 500. Knowledge must
// traverse every page before claiming to search an entity's knowledge fields.
func TestKnowledgeIndexesPastFirstPage(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tn := stockTenant(t, knowledge.New("t-1"))
	for i := range 501 {
		id := fmt.Sprintf("I%03d", i)
		if err := tn.automation("stock", false).PutAt(now, "test.seed", Item{Record: platform.Record{ID: id}, Name: "Machine " + id, Note: "inspection needle " + id, Owner: "ana", Line: "L1"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(tn.sources()); got != 501 {
		t.Fatalf("indexed %d of 501 knowledge fields", got)
	}
	ana, _ := tn.app(PlatformApp).(*Console).Member("ana")
	found := tn.Knowledge(&ana, "", "inspection needle I500", 3, now)
	if len(found) == 0 || found[0].Document != "stock.item/I500#note" {
		t.Fatalf("last page was not searchable: %+v", found)
	}
}
