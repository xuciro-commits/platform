package platformserver

import agentowner "platformserver/engine/agents"

import (
	"encoding/json"
	controlowner "platformserver/kernel/control"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/apps/knowledge"
	"platformserver/platform"
)

func TestAgentKnowledgeCitationUsesCurrentDocumentGrant(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tn := stockTenant(t, knowledge.New("t-1"), agentowner.New("t-1"))
	if err := tn.agents.DeclareAgentApp(agentFixtureApp{App: tn.app("stock"), definition: platform.Agent{Name: "guide", Title: "Stock guide", Instructions: "Read stock"}}); err != nil {
		t.Fatal(err)
	}
	reader := memberOf(t, tn, "ana") // stock member, not a knowledge editor
	doc := platform.KnowledgeDocument{Record: platform.Record{ID: "D1"}, Title: "Inspection policy", Text: "Hydraulic inspection policy", Apps: []string{"stock"}}
	put := func() {
		t.Helper()
		if err := tn.automation(platform.KnowledgeApp, true).PutAt(now, "test.policy", doc); err != nil {
			t.Fatal(err)
		}
	}
	put()
	run := platform.AgentRunRecord{Record: platform.Record{ID: "R1"}, Agent: "stock.guide", OnBehalf: reader.ID, State: "done",
		Steps:     []platform.RunStep{{Tool: "knowledge", Sources: []string{"knowledge.document/D1"}, Outcome: doc.Text}},
		Citations: []platform.Citation{{Document: "knowledge.document/D1", Title: doc.Title}}, Result: "Use the inspection policy"}
	if err := tn.automation(platform.AgentApp, true).PutAt(now, "test.run", run); err != nil {
		t.Fatal(err)
	}
	check := func(visible bool) {
		t.Helper()
		found := tn.Knowledge(&reader, "", "Hydraulic inspection", 5, now)
		if (len(found) == 1) != visible {
			t.Fatalf("knowledge passages: %+v, want visible=%t", found, visible)
		}
		out, err := tn.Read(reader, "runs")
		if err != nil || len(out.([]platform.AgentRunRecord)) != 1 {
			t.Fatalf("runs: %+v, %v", out, err)
		}
		r := out.([]platform.AgentRunRecord)[0]
		if visible {
			if r.Withheld || r.Steps[0].Outcome != doc.Text || len(r.Citations) != 1 || r.Result == "" {
				t.Fatalf("readable policy lost from trace: %+v", r)
			}
		} else if !r.Withheld || r.Steps[0].Outcome != "" || len(r.Citations) != 0 || r.Result != "" {
			t.Fatalf("hidden policy escaped through trace: %+v", r)
		}
	}
	check(true)
	// Content citations do not grant the document editor's metadata endpoint.
	if _, err := tn.RecordOf(reader, platform.KnowledgeDocumentType, doc.ID, now); err == nil {
		t.Fatal("content grant opened generic document metadata")
	}
	may := tn.mayRead(reader, now, false)
	if !may("knowledge.document/D1#text") || may("knowledge.document/D1#apps") || may("knowledge.document/missing") {
		t.Fatal("content source grant escaped its field or identity boundary")
	}
	foreign := reader
	foreign.Tenant = "other"
	if tn.mayRead(foreign, now, false)("knowledge.document/D1") {
		t.Fatal("another tenant read policy provenance")
	}
	// Editing the sharing grant revokes historical citations immediately.
	doc.Apps = []string{"other"}
	put()
	check(false)
	// Public content remains usable without an editor role, until archived.
	doc.Apps = nil
	put()
	check(true)
	admin := platform.Member{ID: "audit", Tenant: tn.ID, Roles: map[string]string{platform.AgentApp: platform.AgentAdmin}}
	trace := func(who platform.Member) platform.AgentRunRecord {
		t.Helper()
		view, err := tn.RecordOf(who, platform.RunType, run.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		return view.Record.(platform.AgentRunRecord)
	}
	// A standalone public-policy run needs no business-record grant.
	if r := trace(admin); r.Withheld || len(r.Citations) != 1 || r.Result == "" {
		t.Fatalf("public policy lost without a business context: %+v", r)
	}
	// The policy is public, but its association with a private task is not.
	// Keep content grants independent of editor metadata, and declare that
	// association at the agent entity, not in a second host permission filter.
	decide(t, tn, reader.ID, "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Press", "line": "L1"}, now)
	run.Ref = "stock.item/I1"
	if err := tn.automation(platform.AgentApp, true).PutAt(now, "test.context", run); err != nil {
		t.Fatal(err)
	}
	check(true)
	// A former task reader can retain an agent-audit role, but no task grant.
	for _, who := range []platform.Member{admin, {ID: reader.ID, Tenant: tn.ID, Roles: map[string]string{platform.AgentApp: platform.AgentAdmin}}} {
		if who.ID == reader.ID {
			dir := tn.app(controlowner.PlatformApp).(*controlowner.Console)
			current, _ := dir.MemberByID(reader.ID)
			rewriteMember(t, dir, reader.ID, func(member *platform.Member) {
				member.Grants = append(current.Grants, platform.Grant{App: platform.AgentApp, Role: platform.AgentAdmin})
			})
		}
		r := trace(who)
		if !r.Withheld || len(r.Citations) != 0 || r.Result != "" {
			t.Fatalf("private task escaped via a public citation for %s: %+v", who.ID, r)
		}
		if r.Steps[0].Outcome != doc.Text || !tn.mayRead(who, now, false)("knowledge.document/D1") {
			t.Fatalf("private task restriction revoked independent public content for %s: %+v", who.ID, r)
		}
	}
	doc.Archived = true
	put()
	check(false)
}

// What an agent read, cited, drafted and concluded is journaled once and read
// many times. Authority changes in between, so every read checks the trace
// against its sources again: a record whose owner or unit moved, a field only
// another role reads, an administrator who never read the business record
// (see docs/Architecture.md).
func TestAgentTraceScope(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tn := stockTenant(t, agentowner.New("t-1"))
	if err := tn.agents.DeclareAgentApp(agentFixtureApp{App: tn.app("stock"), definition: platform.Agent{Name: "guide", Title: "Stock guide", Instructions: "Read stock"}}); err != nil {
		t.Fatal(err)
	}
	member := func(id string) platform.Member { return memberOf(t, tn, id) }
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Press", "line": "L1", "note": "hydraulic inspection due"}, now)
	item, _ := platform.Get[Item](tn.automation("stock", false), "I1")
	item.Secret = "the tolerance is 0.02mm" // only a lead reads it
	if err := tn.automation("stock", true).PutAt(now, "test.tolerance", item); err != nil {
		t.Fatal(err)
	}
	run := platform.AgentRunRecord{Record: platform.Record{ID: "R1"}, Agent: "stock.guide", OnBehalf: "ana", State: "done", Ref: "stock.item/I1",
		Seen:      `{"name":"Press","note":"hydraulic inspection due"}`,
		Steps:     []platform.RunStep{{Tool: "context", Sources: []string{"stock.item/I1"}, Outcome: `{"note":"hydraulic inspection due"}`}},
		Citations: []platform.Citation{{Document: "stock.item/I1#note", Title: "Press"}},
		Draft:     []platform.Draft{{Kind: "action", Action: "stock.item.count", Type: "stock.item", Target: "I1", Payload: `{"qty":6}`}},
		Result:    "the press is due for hydraulic inspection"}
	if err := tn.automation(platform.AgentApp, true).PutAt(now, "test.run", run); err != nil {
		t.Fatal(err)
	}
	tn.transcribe(Transcript{At: now, Member: "ana", Model: "test", Run: "R1", Request: json.RawMessage(`{"prompt":"hydraulic inspection due"}`)})
	if err := tn.automation(platform.AgentApp, true).PutAt(now, "test.memory", platform.Memory{Record: platform.Record{ID: "M1"}, Agent: "stock.guide",
		Fact: "the press needs a hydraulic inspection every month", For: "ana", Run: "R1", Sources: []string{"stock.item/I1"}, State: "active"}); err != nil {
		t.Fatal(err)
	}
	admin := platform.Member{ID: "admin", Tenant: tn.ID, Roles: map[string]string{platform.AgentApp: platform.AgentAdmin}}
	leadAdmin := member("lead")
	dir := tn.app(controlowner.PlatformApp).(*controlowner.Console)
	current, _ := dir.MemberByID(leadAdmin.ID)
	rewriteMember(t, dir, leadAdmin.ID, func(member *platform.Member) {
		member.Grants = append(current.Grants, platform.Grant{App: platform.AgentApp, Role: platform.AgentAdmin})
	})
	leadAdmin.Roles[platform.AgentApp] = platform.AgentAdmin

	read := func(who platform.Member) platform.AgentRunRecord {
		out, err := tn.Read(who, "runs")
		if err != nil {
			t.Fatalf("%s runs: %v", who.ID, err)
		}
		runs, ok := out.([]platform.AgentRunRecord)
		if !ok || len(runs) != 1 {
			t.Fatalf("%s runs: %+v", who.ID, out)
		}
		return runs[0]
	}
	whole := func(who platform.Member, r platform.AgentRunRecord) {
		if r.Withheld || r.Seen == "" || r.Result == "" || len(r.Citations) != 1 || len(r.Draft) != 1 || r.Steps[0].Outcome == "" {
			t.Errorf("%s lost a trace it may read: %+v", who.ID, r)
		}
	}
	narrowed := func(who platform.Member, r platform.AgentRunRecord) {
		if !r.Withheld || r.Seen != "" || r.Result != "" || len(r.Citations) != 0 || len(r.Draft) != 0 || r.Steps[0].Outcome != "" {
			t.Errorf("%s read a trace of a record it may not read: %+v", who.ID, r)
		}
	}

	// Its own person, who may read the record and the cited field, reads it whole.
	whole(member("ana"), read(member("ana")))

	// A fact an agent kept from what it read is read only while its sources are.
	fact := func(who platform.Member) platform.Memory {
		out, err := tn.Read(who, "memories")
		if err != nil {
			t.Fatalf("%s memories: %v", who.ID, err)
		}
		facts, ok := out.([]platform.Memory)
		if !ok || len(facts) != 1 {
			t.Fatalf("%s memories: %+v", who.ID, out)
		}
		return facts[0]
	}
	if kept := fact(member("ana")); kept.Fact == "" {
		t.Errorf("ana lost a fact about a record it may read: %+v", kept)
	}

	// An administrator of the agent app holds no role in the business app: the
	// run is theirs to see, its content is not, and neither are its transcripts.
	page, err := tn.Records(admin, platform.RunType, platform.Query{}, now)
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("admin runs: %+v, %v", page, err)
	}
	narrowed(admin, page.Records[0].(platform.AgentRunRecord))
	view, err := tn.RecordOf(admin, platform.RunType, "R1", now)
	if err != nil {
		t.Fatalf("admin run: %v", err)
	}
	narrowed(admin, view.Record.(platform.AgentRunRecord))
	if out, err := tn.TranscriptsFor(admin, "R1", 50, now); err == nil || len(out) != 0 {
		t.Errorf("admin read the model call of a record it may not read: %+v, %v", out, err)
	}
	if out, err := tn.TranscriptsFor(leadAdmin, "R1", 50, now); err != nil || len(out) != 1 {
		t.Errorf("an administrator who may read the record lost the transcript: %+v, %v", out, err)
	}
	if out, err := tn.TranscriptsFor(member("ana"), "R1", 50, now); err == nil || len(out) != 0 {
		t.Errorf("a member without the administrator role read transcripts: %+v, %v", out, err)
	}

	// A field only another role reads is withheld where the record itself is
	// readable: op reads the item on its line, not the tolerance the lead keeps.
	second := platform.AgentRunRecord{Record: platform.Record{ID: "R2"}, Agent: "stock.guide", OnBehalf: "op", State: "done", Ref: "stock.item/I1",
		Seen:      `{"name":"Press"}`,
		Steps:     []platform.RunStep{{Tool: "context", Sources: []string{"stock.item/I1#secret"}, Outcome: `{"secret":"the tolerance is 0.02mm"}`}},
		Citations: []platform.Citation{{Document: "stock.item/I1#note", Title: "Press"}, {Document: "stock.item/I1#secret", Title: "Press"}},
		Result:    "the tolerance is 0.02mm"}
	if err := tn.automation(platform.AgentApp, true).PutAt(now, "test.run", second); err != nil {
		t.Fatal(err)
	}
	opRun := read(member("op"))
	if !opRun.Withheld || opRun.Seen == "" || opRun.Result != "" || opRun.Steps[0].Outcome != "" ||
		len(opRun.Citations) != 1 || opRun.Citations[0].Document != "stock.item/I1#note" {
		t.Errorf("op read a restricted field through a journaled trace: %+v", opRun)
	}

	// Revocation: the item moves to another owner on another line. What was
	// journaled about it is no longer ana's to read; the lead above both lines
	// still reads it.
	detail := func(who platform.Member, id string) platform.AgentRunRecord {
		view, err := tn.RecordOf(who, platform.RunType, id, now)
		if err != nil {
			t.Fatalf("%s run %s: %v", who.ID, id, err)
		}
		return view.Record.(platform.AgentRunRecord)
	}
	whole(leadAdmin, detail(leadAdmin, "R1"))
	item, _ = platform.Get[Item](tn.automation("stock", false), "I1")
	item.Owner, item.Line = "bo", "L2"
	if err := tn.automation("stock", true).PutAt(now, "test.move", item); err != nil {
		t.Fatal(err)
	}
	if tn.Readable(member("ana"), "stock.item/I1", now) {
		t.Fatal("ana still reads the moved item")
	}
	narrowed(member("ana"), read(member("ana")))
	whole(leadAdmin, detail(leadAdmin, "R1"))

	if kept := fact(member("ana")); kept.Fact != "" || len(kept.Sources) != 0 {
		t.Errorf("ana read a fact taken from a record it may no longer read: %+v", kept)
	}

	// Another tenant's member reads no run at all.
	foreign := member("ana")
	foreign.Tenant = "t-2"
	if out, err := tn.Read(foreign, "runs"); err == nil && len(out.([]platform.AgentRunRecord)) != 0 {
		t.Errorf("another tenant read runs: %+v", out)
	}
}

// A declaration the type cannot honour is a manifest error: the tenant does not
// start, rather than serving content whose sources nobody checks.
func TestDerivedDeclarationIsChecked(t *testing.T) {
	type note struct {
		platform.Record
		Name     string   `json:"name" field:"search"`
		Owner    string   `json:"owner"`
		Text     string   `json:"text"`
		Sources  []string `json:"sources,omitempty"`
		Withheld bool     `json:"withheld,omitempty"`
	}
	for _, x := range []struct {
		why     string
		derived []platform.Derivation
		field   string
		want    string
	}{
		{"an unknown source field", []platform.Derivation{{From: "origin", Fields: []string{"text"}}}, "withheld", `has no field "origin"`},
		{"an unknown emptied field", []platform.Derivation{{From: "owner", Fields: []string{"summary"}}}, "withheld", `has no field "summary"`},
		{"a source path into a field that is no list", []platform.Derivation{{From: "name.sources", Fields: []string{"text"}}}, "withheld", "not a list of records"},
		{"no withheld field", []platform.Derivation{{From: "sources", Fields: []string{"text"}}}, "", "names a withheld field"},
		{"a withheld field that is no boolean", []platform.Derivation{{From: "sources", Fields: []string{"text"}}}, "text", "is not a boolean field"},
		{"a derivation that changes nothing", []platform.Derivation{{From: "sources"}}, "withheld", "empties no field"},
	} {
		e := platform.Entity{Type: "stock.note", Title: "Note", Model: note{}, Derived: x.derived, Withheld: x.field}
		_, err := platform.Describe("stock", e, func(reflect.Type) string { return "" })
		if err == nil || !strings.Contains(err.Error(), x.want) {
			t.Errorf("%s: %v, want %q", x.why, err, x.want)
		}
	}
	ok := platform.Entity{Type: "stock.note", Title: "Note", Model: note{},
		Derived:  []platform.Derivation{{From: "sources", Fields: []string{"text"}}, {From: "*", Fields: []string{"name"}}},
		Withheld: "withheld"}
	info, err := platform.Describe("stock", ok, func(reflect.Type) string { return "" })
	if err != nil || len(info.Derived) != 2 || len(info.Withheld) == 0 {
		t.Fatalf("a sound declaration was refused: %v, %+v", err, info.Derived)
	}
}
