package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/internal/records"
	"platformserver/platform"
)

func booleanRule(op string, args ...platform.Expr) *platform.Expr {
	return &platform.Expr{Op: op, Kind: "boolean", Args: args}
}

func commonConstraints() []platform.Constraint {
	return []platform.Constraint{
		{Name: "number", Kind: "unique", Fields: []string{"number"}, Scope: "tenant", Message: "Number must be unique.", Translations: map[string]string{"zh-CN": "编号必须唯一。"}},
		{Name: "positive", Kind: "expression", Fields: []string{"qty"}, Message: "Quantity must be positive.", Translations: map[string]string{"zh-CN": "数量必须为正。"}, Expr: booleanRule(">", platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 0))},
		{Name: "period", Kind: "range", Fields: []string{"end", "start"}, Message: "End must not precede start.", Translations: map[string]string{"zh-CN": "结束时间不得早于开始时间。"}, Expr: booleanRule(">=", platform.ExprReference("date", "end"), platform.ExprReference("date", "start"))},
		{Name: "revision", Kind: "immutable", Fields: []string{"version"}, Message: "Released version is immutable.", Translations: map[string]string{"zh-CN": "已生效版本不可更改。"}},
	}
}

type constraintEntry struct {
	platform.Record
	Number  string `json:"number"`
	Qty     int    `json:"qty"`
	Start   string `json:"start" type:"date"`
	End     string `json:"end" type:"date"`
	Version string `json:"version"`
	State   string `json:"state" field:"readonly" choices:"draft,released"`
}

// A neutral native owner proves that Put itself is the gate, including a
// custom handler that omits Caller.Check. No industry app is changed.
type constraintOwner struct {
	ledger *platform.Ledger
	entity platform.Entity
}

func newConstraintOwner(tenant string) *constraintOwner {
	entity := platform.Entity{Type: "invariants.entry", Title: "Entry", Model: constraintEntry{}, Constraints: commonConstraints(), Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"operator"}},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft"}, {Name: "released", Title: "Released"}}, Transitions: []platform.Transition{
			{Name: "release", Title: "Release", From: []string{"draft"}, To: []string{"released"}, Roles: []string{"operator"}},
			{Name: "return", Title: "Return", From: []string{"released"}, To: []string{"draft"}, Roles: []string{"operator"}},
		}}}
	actions := platform.EntityActions(entity)
	actions = append(actions, platform.Action{Schema: "invariants.entry.overwrite", Target: entity.Type, Title: "Overwrite", Description: "Replace the quantity through a custom decision", Capability: entity.Type, Roles: []string{"operator"}, Payload: []platform.Field{{Name: "qty", Type: "integer", Description: "Quantity"}}})
	return &constraintOwner{entity: entity, ledger: platform.NewLedger(tenant, "invariants", platform.NewCatalog(actions...), entity.Type)}
}
func (o *constraintOwner) Manifest() platform.Manifest {
	edit, _ := o.ledger.Catalog.Action("invariants.entry.edit")
	edit.Schema = "edit"
	return platform.Manifest{ID: "invariants", Version: "1", Actions: o.ledger.Catalog, Entities: []platform.Entity{o.entity}, Provides: []platform.Provision{{Protocol: platform.Protocol{Name: "invariant-entry", Version: 1, Actions: []platform.Action{edit}}, Actions: map[string]string{"edit": "invariants.entry.edit"}}}}
}
func (o *constraintOwner) AcceptedLedger() *platform.Ledger                  { return o.ledger }
func (o *constraintOwner) Declarations() []*pb.AuthorityDeclaration          { return o.ledger.Declarations() }
func (o *constraintOwner) Snapshot() (json.RawMessage, error)                { return o.ledger.Snapshot() }
func (o *constraintOwner) Restore(raw json.RawMessage) error                 { return o.ledger.Restore(raw) }
func (o *constraintOwner) Read(platform.Caller, string) (any, *kernel.Error) { return nil, unknown() }
func (o *constraintOwner) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, unknown()
}
func (o *constraintOwner) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if s.GetSchema().GetName() == "invariants.entry.overwrite" {
		return o.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
			entry, ok := platform.Get[constraintEntry](c, s.GetTarget().GetId())
			if !ok {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			var payload struct {
				Qty int `json:"qty"`
			}
			if json.Unmarshal(s.GetPayload(), &payload) != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			entry.Qty = payload.Qty
			return func(r *pb.ChangeRecord) { _ = c.Put(r, entry) }, nil
		})
	}
	r, err, _ := o.ledger.Generated(c, s, at, nil, o.entity)
	return r, err
}

func validConstraintEntry(number string) map[string]any {
	return map[string]any{"number": number, "qty": 2, "start": "2026-10-12", "end": "2026-10-13", "version": "v1"}
}

func constraintTenant(t *testing.T) *Tenant {
	bot := seatOf("bot", "build:builder", "invariants:operator")
	bot.Member.Agent = true
	return composeTenant(t, "constraints", []Seat{seatOf("builder", "build:builder", "invariants:operator"), seatOf("reader", "build:user"), bot}, build.New("constraints"), newConstraintOwner("constraints"))
}

func publishConstraintEntry(t *testing.T, tenant *Tenant) string {
	object := map[string]any{"name": "entry", "title": "Entry", "fields": []construction.Field{{Name: "number", Title: "Number", Type: "text"}, {Name: "qty", Title: "Quantity", Type: "integer"}, {Name: "start", Title: "Start", Type: "date"}, {Name: "end", Title: "End", Type: "date"}, {Name: "version", Title: "Version", Type: "text"}}, "constraints": commonConstraints(), "states": []construction.State{{Name: "draft", Title: "Draft"}, {Name: "released", Title: "Released"}}, "actions": []construction.Action{{Name: "release", Title: "Release", From: []string{"draft"}, To: "released"}, {Name: "return", Title: "Return", From: []string{"released"}, To: "draft"}}}
	return publishObject(t, tenant, "builder", "ENTRY", object, txnAt)
}

// Measure final checks on an already prepared private decision, including the
// parent version-pin scan. Preparation and writes are outside the timed loop;
// the existing decision trace counts successful expression checks, not visits.
func BenchmarkFinalConstraintScan(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		for _, affected := range []bool{false, true} {
			scenario := "unrelated-write"
			if affected {
				scenario = "one-constrained-write"
			}
			b.Run(fmt.Sprintf("records=%d/%s", count, scenario), func(b *testing.B) {
				entity := newConstraintOwner("benchmark").entity
				rules := commonConstraints()
				entity.Constraints = []platform.Constraint{rules[1], rules[2], {Name: "bounded", Kind: "expression", Fields: []string{"qty"}, Message: "Quantity is below the limit.", Expr: booleanRule("<", platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 1000))}}
				info, err := platform.Describe("invariants", entity, func(reflect.Type) string { return "" })
				if err != nil {
					b.Fatal(err)
				}
				type note struct {
					platform.Record
					Text string `json:"text"`
				}
				unrelated, err := platform.Describe("invariants", platform.Entity{Type: "invariants.note", Model: note{}}, func(reflect.Type) string { return "" })
				if err != nil {
					b.Fatal(err)
				}
				entries := make([]records.Entry[*row], count)
				for i := range entries {
					id := fmt.Sprintf("ROW-%06d", i)
					value := constraintEntry{Record: platform.Record{ID: id, Revision: 1}, Qty: 2, Start: "2026-10-12", End: "2026-10-13", State: "draft"}
					entries[i] = records.Entry[*row]{Key: id, Value: &row{value: reflect.ValueOf(&value).Elem()}}
				}
				index, err := records.From(entries)
				if err != nil {
					b.Fatal(err)
				}
				store := newRecordStore()
				store.types[info.Type] = &entityType{info: info, rows: index}
				store.types[unrelated.Type] = &entityType{info: unrelated}
				store.types[unrelated.Type].setRow("NOTE", &row{value: reflect.ValueOf(&note{Record: platform.Record{ID: "NOTE", Revision: 1}, Text: "before"}).Elem()})
				tenant := &Tenant{records: store}
				draft := &stagedDecision{tenant: tenant, records: store.forkRecords(), at: txnAt, constraintWrites: map[string]constraintWrite{}}
				caller := platform.NewCaller(draft, platform.Member{ID: "operator", Roles: map[string]string{"invariants": "operator"}}, "invariants", false, false)
				typ, id := unrelated.Type, "NOTE"
				after := &row{value: reflect.ValueOf(&note{Record: platform.Record{ID: id, Revision: 2}, Text: "after"}).Elem()}
				if affected {
					typ, id = info.Type, "ROW-000000"
					value := store.types[typ].rows.Get(id).value.Interface().(constraintEntry)
					value.Qty = 3
					value.Revision++
					after = &row{value: reflect.ValueOf(&value).Elem()}
				}
				draft.constraintWrites[typ+"/"+id] = constraintWrite{caller: caller, before: store.types[typ].rows.Get(id)}
				draft.records.types[typ].setRow(id, after)
				checks := 0
				tenant.simulationTrace = func(event platform.DecisionTrace) {
					if event.Kind == "constraint" {
						checks++
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := draft.checkFinalConstraints(); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				want := 0
				if affected {
					want = len(info.Constraints) * b.N
				}
				if checks != want {
					b.Fatalf("measured %d constraint evaluations, want %d", checks, want)
				}
				b.ReportMetric(float64(count), "constrained-records")
				b.ReportMetric(float64(len(info.Constraints)), "constraints/record")
				b.ReportMetric(float64(len(draft.constraintWrites)), "writes/op")
				b.ReportMetric(float64(checks)/float64(b.N), "evals/op")
			})
		}
	}
}

func TestFinalLocalConstraintsKeepOrderingAndIgnoreUntouchedHistory(t *testing.T) {
	tenant := constraintTenant(t)
	first := tenant.records.types["invariants.entry"]
	rules := commonConstraints()
	first.info.Constraints = []platform.Constraint{rules[1], rules[2], {Name: "bounded", Kind: "expression", Fields: []string{"qty"}, Message: "Quantity is below the limit.", Expr: booleanRule("<", platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 1000))}}
	second := &entityType{info: first.info}
	second.info.Type = "invariants.zentry"
	tenant.records.types[second.info.Type] = second
	for _, et := range []*entityType{second, first} {
		for _, id := range []string{"Z", "OLD", "A"} {
			value := constraintEntry{Record: platform.Record{ID: id, Revision: 1}, Qty: 0, Start: "2026-10-12", End: "2026-10-13", State: "draft"}
			et.setRow(id, &row{value: reflect.ValueOf(&value).Elem()})
		}
	}
	var trace []string
	tenant.simulationTrace = func(event platform.DecisionTrace) {
		if event.Kind == "constraint" {
			trace = append(trace, event.Type+"/"+event.ID+"/"+event.Name)
		}
	}
	draft := tenant.newStagedDecision()
	draft.at = txnAt
	caller := platform.NewCaller(draft, memberOf(t, tenant, "builder"), "invariants", false, false)
	draft.constraintWrites = map[string]constraintWrite{}
	for _, ref := range []string{second.info.Type + "/A", first.info.Type + "/Z", first.info.Type + "/A"} {
		typ, id, _ := strings.Cut(ref, "/")
		draft.constraintWrites[ref] = constraintWrite{caller: caller, before: tenant.records.types[typ].rows.Get(id)}
	}
	value := constraintEntry{Record: platform.Record{ID: "A", Revision: 2}, Qty: 2, Start: "2026-10-12", End: "2026-10-11", State: "draft"}
	draft.records.types[first.info.Type].setRow("A", &row{value: reflect.ValueOf(&value).Elem()})
	err := draft.checkFinalConstraints()
	issues := draft.inputIssues[err]
	if err == nil || len(issues) != 1 || issues[0].Record != first.info.Type+"/A" || issues[0].Constraint != "period" {
		t.Fatal("final constraints changed type/id/rule refusal order", err, issues)
	}
	if !reflect.DeepEqual(trace, []string{first.info.Type + "/A/positive", first.info.Type + "/A/period"}) {
		t.Fatal("final constraints skipped or reordered the original rules", trace)
	}
	trace = nil
	draft = tenant.newStagedDecision()
	draft.at = txnAt
	caller = platform.NewCaller(draft, memberOf(t, tenant, "builder"), "invariants", false, false)
	draft.constraintWrites = map[string]constraintWrite{first.info.Type + "/A": {caller: caller, before: first.rows.Get("A")}}
	value.End = "2026-10-13"
	draft.records.types[first.info.Type].setRow("A", &row{value: reflect.ValueOf(&value).Elem()})
	if err := draft.checkFinalConstraints(); err != nil {
		t.Fatal("untouched historical violations blocked a valid local write", err)
	}
	if !reflect.DeepEqual(trace, []string{first.info.Type + "/A/positive", first.info.Type + "/A/period", first.info.Type + "/A/bounded"}) {
		t.Fatal("one local write evaluated more than its three original rules", trace)
	}
}

func TestConstraintReadPlanKeepsExternalDependenciesNonlocal(t *testing.T) {
	path := platform.Expr{Op: "path", Kind: "integer", Args: []platform.Expr{platform.ExprReference("reference", "parent")}, Path: []string{"qty"}}
	info := platform.EntityInfo{App: "invariants", Type: "invariants.child", Fields: []platform.FieldInfo{{Name: "parent", Type: "reference", Ref: "invariants.entry"}}}
	draft := &stagedDecision{}
	for _, tc := range []struct {
		name    string
		rule    platform.Constraint
		paths   map[string][][]string
		unknown bool
	}{
		{name: "path inside args and in", rule: platform.Constraint{Expr: &platform.Expr{Op: "all", In: booleanRule(">", path, platform.ExprLiteralValue("integer", 0))}}, paths: map[string][][]string{"": {{"parent", "qty"}}}},
		{name: "aggregate field", rule: platform.Constraint{Expr: &platform.Expr{Op: "sum", Agg: &platform.ExprAggregate{Source: "items", Field: []string{"parent", "qty"}}}}, paths: map[string][][]string{"items": {{"parent", "qty"}}}},
		{name: "window inside args", rule: platform.Constraint{Expr: booleanRule(">", platform.Expr{Window: &platform.ExprWindowReference{Source: "readings"}}, platform.ExprLiteralValue("integer", 0))}},
		{name: "declared reads", rule: platform.Constraint{Reads: &platform.RuleReadSpec{}}},
		{name: "parent unique", rule: platform.Constraint{Kind: "unique", Scope: "parent"}, paths: map[string][][]string{"": {{"parent"}}}},
		{name: "missing declared set", rule: platform.Constraint{Reads: &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{"items": {Set: "missing"}}}}, unknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := draft.constraintReadPlan(info, tc.rule)
			if plan.local || plan.unknown != tc.unknown {
				t.Fatal("external dependency was treated as a local constraint", plan)
			}
			if tc.paths != nil && !reflect.DeepEqual(plan.paths, tc.paths) {
				t.Fatal("constraint dependency paths changed", plan.paths)
			}
		})
	}
}

// One declared invariant group, the real generated/transaction/import/agent/
// protocol entry paths, and an ignored Put error all yield the same answer.
func TestBusinessConstraintsAllWriteEntrances(t *testing.T) {
	for _, entry := range []struct {
		name, field string
		value       any
	}{
		{"number", "number", "ONE"}, {"positive", "qty", 0}, {"period", "end", "2026-10-11"}, {"revision", "version", "v2"},
	} {
		t.Run(entry.name, func(t *testing.T) {
			var expected *kernel.Error
			for _, path := range []string{"standard", "transaction", "csv", "agent", "protocol", "unchecked-put"} {
				if path == "unchecked-put" && entry.name != "positive" {
					continue
				}
				t.Run(path, func(t *testing.T) {
					tenant := constraintTenant(t)
					typ, app := publishConstraintEntry(t, tenant), construction.ID
					if path == "protocol" || path == "unchecked-put" {
						typ, app = "invariants.entry", "invariants"
					}
					decide(t, tenant, "builder", app, typ+".create", typ, "ONE", validConstraintEntry("ONE"), txnAt)
					decide(t, tenant, "builder", app, typ+".create", typ, "TWO", validConstraintEntry("TWO"), txnAt)
					if entry.name == "revision" {
						decide(t, tenant, "builder", app, typ+".release", typ, "TWO", nil, txnAt)
					}
					if path == "transaction" {
						object, err := tenant.RecordOf(memberOf(t, tenant, "builder"), construction.ObjectType, "ENTRY", txnAt)
						if err != nil {
							t.Fatal(err)
						}
						definition := object.Record.(construction.Object)
						definition.Actions = append(definition.Actions, construction.Action{Name: "apply", Title: "Apply", From: []string{"draft", "released"}, Inputs: []construction.Input{{Name: "value", Title: "Value", Type: map[string]string{"qty": "integer", "number": "text", "end": "date", "version": "text"}[entry.field]}}, Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "entrynumbers", Pattern: "{n}"}}, {Name: "write", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: entry.field, From: "value"}}}}}})
						decide(t, tenant, "builder", construction.ID, "build.object.edit", construction.ObjectType, "ENTRY", map[string]any{"actions": definition.Actions}, txnAt)
						decide(t, tenant, "builder", construction.ID, "build.object.publish", construction.ObjectType, "ENTRY", nil, txnAt)
					}
					before := txnState(tenant)
					member := memberOf(t, tenant, "builder")
					var refusal *kernel.Error
					var issues []platform.FieldIssue
					patch := map[string]any{entry.field: entry.value}
					switch path {
					case "csv":
						csv := []byte(fmt.Sprintf("id,%s\nTWO,%v\n", entry.field, entry.value))
						preview, err := tenant.Import(member, typ, csv, true, txnAt)
						if err != nil || len(preview) != 1 || preview[0].Error == nil || preview[0].Outcome != "CONSTRAINT_VIOLATION" {
							t.Fatalf("preview bypassed invariant: %+v %v", preview, err)
						}
						rows, err := tenant.Import(member, typ, csv, false, txnAt)
						if err != nil || len(rows) != 1 || rows[0].Error == nil || rows[0].Outcome != "CONSTRAINT_VIOLATION" {
							t.Fatalf("import bypassed invariant: %+v %v", rows, err)
						}
						issues = rows[0].Error.Issues
						refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: rows[0].Error.Message}
						if preview[0].Error.Message != rows[0].Error.Message {
							t.Fatal("preview and import reasons differ")
						}
					case "protocol":
						key := fmt.Sprintf("protocol-%s", entry.name)
						_, _, refusal = tenant.Invoke(member, "invariant-entry/1", "edit", "TWO", platform.Raw(patch), key, txnAt)
						issues = tenant.committed.refusals[app+"/"+key].Issues
					default:
						who, schema := "builder", typ+".edit"
						if path == "agent" {
							who = "bot"
						}
						if path == "transaction" {
							schema = typ + ".apply"
							patch = map[string]any{"value": entry.value}
						}
						if path == "unchecked-put" {
							schema = typ + ".overwrite"
						}
						member = memberOf(t, tenant, who)
						_, refusal, issues = tenant.submitDiagnosed(member, submission(tenant, member, app, schema, typ, "TWO", patch), txnAt)
					}
					if refusal == nil || len(issues) != 1 || issues[0].Code != "constraint_violation" || issues[0].Constraint != entry.name {
						t.Fatalf("wrong diagnostic: %v %+v", refusal, issues)
					}
					if expected == nil {
						expected = refusal
					} else if *expected != *refusal {
						t.Fatalf("entrance changed refusal: expected=%+v actual=%+v", expected, refusal)
					}
					if txnState(tenant) != before || len(tenant.sequences.clone()) != 0 {
						t.Fatal("rejected entrance left state or consumed a number")
					}
				})
			}
		})
	}
}

func TestBusinessConstraintsUniqueConcurrentAndHistory(t *testing.T) {
	tenant := constraintTenant(t)
	var entries []Entry
	tenant.Record = func(entry Entry) { entries = append(entries, entry) }
	typ := publishConstraintEntry(t, tenant)
	member := memberOf(t, tenant, "builder")
	var group sync.WaitGroup
	answers := make(chan *kernel.Error, 2)
	for _, id := range []string{"A", "B"} {
		request := submission(tenant, member, construction.ID, typ+".create", typ, id, validConstraintEntry("SAME"))
		group.Add(1)
		go func() { defer group.Done(); _, err := tenant.Submit(member, request, txnAt); answers <- err }()
	}
	group.Wait()
	close(answers)
	success, denied := 0, 0
	for err := range answers {
		if err == nil {
			success++
		} else if strings.Contains(err.Message, "Number must be unique") {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatalf("uniqueness raced: %d/%d", success, denied)
	}
	decide(t, tenant, "builder", construction.ID, typ+".create", typ, "HISTORY", validConstraintEntry("HISTORY"), txnAt)
	decide(t, tenant, "builder", construction.ID, typ+".release", typ, "HISTORY", nil, txnAt)
	decide(t, tenant, "builder", construction.ID, typ+".return", typ, "HISTORY", nil, txnAt)
	if result := refuse(t, tenant, "builder", construction.ID, typ+".edit", typ, "HISTORY", map[string]any{"version": "v2"}, txnAt); !strings.Contains(result, "Released version is immutable") {
		t.Fatalf("return unlocked history: %s", result)
	}
	// Tightening an invariant changes new writes, not already accepted bytes.
	constraints := commonConstraints()
	constraints[1].Expr = booleanRule(">", platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 10))
	decide(t, tenant, "builder", construction.ID, "build.object.edit", construction.ObjectType, "ENTRY", map[string]any{"constraints": constraints}, txnAt)
	decide(t, tenant, "builder", construction.ID, "build.object.publish", construction.ObjectType, "ENTRY", nil, txnAt)
	if _, err := tenant.RecordOf(member, typ, "HISTORY", txnAt); err != nil {
		t.Fatal("tightening hid historical row", err)
	}
	if result := refuse(t, tenant, "builder", construction.ID, typ+".edit", typ, "HISTORY", map[string]any{"number": "CHANGED"}, txnAt); !strings.Contains(result, "Quantity must be positive") {
		t.Fatal("new write escaped tighter rule", result)
	}
	// Include the definition prefix; replay may neither call the current rules
	// nor rewrite the low-quantity row that the new definition now rejects.
	all := append([]Entry(nil), entries...)
	base := constraintTenant(t)
	if err := base.Replay(all); err != nil {
		t.Fatal(err)
	}
	actual, err := base.RecordOf(memberOf(t, base, "builder"), typ, "HISTORY", txnAt)
	if err != nil || reflect.ValueOf(actual.Record).FieldByName("Qty").Elem().Int() != 2 {
		t.Fatalf("history changed during replay: %+v %v", actual, err)
	}
}

func TestBusinessConstraintsNoLiveRuntimeWriteBypass(t *testing.T) {
	tenant := constraintTenant(t)
	entry := constraintEntry{Record: platform.Record{ID: "BYPASS"}, Number: "BYPASS", Qty: 0, Start: "2026-10-12", End: "2026-10-13", Version: "v1"}
	c := platform.NewCaller(runtime{tenant}, memberOf(t, tenant, "builder"), "invariants", false, false)
	if err := c.PutAt(txnAt, "read-callback", entry); err == nil {
		t.Fatal("a live read or callback bypassed the private write gate")
	}
	if tenant.records.types["invariants.entry"].rows.Get("BYPASS") != nil {
		t.Fatal("direct live write left a record")
	}
}

func TestBusinessConstraintsTransactionTotalsAndReferenceCardinality(t *testing.T) {
	tenant := constraintTenant(t)
	var entries []Entry
	tenant.Record = func(entry Entry) { entries = append(entries, entry) }
	header := publishObject(t, tenant, "builder", "HEADER", map[string]any{"name": "header", "title": "Header", "fields": []construction.Field{{Name: "number", Title: "Number", Type: "text"}, {Name: "total", Title: "Total", Type: "integer"}}}, txnAt)
	line := publishObject(t, tenant, "builder", "LINE", map[string]any{"name": "line", "title": "Line", "fields": []construction.Field{{Name: "header", Title: "Header", Type: "reference", Ref: header, Required: true}, {Name: "qty", Title: "Quantity", Type: "integer"}, {Name: "debit", Title: "Debit", Type: "decimal"}, {Name: "credit", Title: "Credit", Type: "decimal"}}}, txnAt)
	decide(t, tenant, "builder", construction.ID, "build.query.create", construction.QueryType, "LINES", map[string]any{"name": "lines", "title": "Lines", "description": "All lines of a header", "object": line, "by": "header", "limit": 200}, txnAt)
	decide(t, tenant, "builder", construction.ID, "build.query.publish", construction.QueryType, "LINES", nil, txnAt)
	reads := &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{"lines": {App: construction.ID, Query: "lines", Version: 1, Object: line, Of: "$subject", Policy: "complete"}}}
	sum := platform.Expr{Op: "sum", Kind: "integer", Agg: &platform.ExprAggregate{Source: "lines", Field: []string{"qty"}}}
	count := platform.Expr{Op: "count", Kind: "integer", Agg: &platform.ExprAggregate{Source: "lines"}}
	debits := platform.Expr{Op: "sum", Kind: "decimal", Agg: &platform.ExprAggregate{Source: "lines", Field: []string{"debit"}}}
	credits := platform.Expr{Op: "sum", Kind: "decimal", Agg: &platform.ExprAggregate{Source: "lines", Field: []string{"credit"}}}
	difference := platform.Expr{Op: "abs", Kind: "decimal", Args: []platform.Expr{{Op: "-", Kind: "decimal", Args: []platform.Expr{debits, credits}}}}
	constraints := []platform.Constraint{
		{Name: "total", Kind: "expression", Fields: []string{"total"}, Level: "txn", Message: "Header total must equal all line quantities.", Expr: booleanRule("=", platform.ExprReference("integer", "total"), sum), Reads: reads},
		{Name: "balanced", Kind: "expression", Fields: []string{"total"}, Level: "txn", Message: "Debit and credit must balance within 0.005.", Expr: booleanRule("<=", difference, platform.ExprLiteralValue("decimal", "0.005")), Reads: reads},
		{Name: "children", Kind: "cardinality", Fields: []string{"archived"}, Level: "txn", Message: "A header with active lines cannot be archived.", Expr: booleanRule("or", platform.Expr{Op: "not", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("boolean", "archived")}}, *booleanRule("=", count, platform.ExprLiteralValue("integer", 0))), Reads: reads},
	}
	decide(t, tenant, "builder", construction.ID, "build.object.edit", construction.ObjectType, "HEADER", map[string]any{"constraints": constraints}, txnAt)
	decide(t, tenant, "builder", construction.ID, "build.object.publish", construction.ObjectType, "HEADER", nil, txnAt)
	txn := &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{
		{Name: "number", Kind: "sequence", Sequence: &platform.Sequence{Name: "documents", Pattern: "D-{n:3}"}},
		{Name: "request", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "consumed", From: "=true"}}},
		{Name: "head", Kind: "create", Target: platform.TxnRef{Type: header}, Changes: []platform.TxnSet{{Field: "number", From: "step.number"}, {Field: "total", From: "=5"}}},
		{Name: "first", Kind: "create", Target: platform.TxnRef{Type: line}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}, {Field: "qty", From: "=2"}, {Field: "debit", From: "=1.005"}, {Field: "credit", From: "=0"}}},
		{Name: "second", Kind: "create", Target: platform.TxnRef{Type: line}, Changes: []platform.TxnSet{{Field: "header", From: "step.head"}, {Field: "qty", From: "qty"}, {Field: "debit", From: "=0"}, {Field: "credit", From: "credit"}}},
	}}
	request := publishObject(t, tenant, "builder", "REQUEST", map[string]any{"name": "request", "title": "Request", "fields": []construction.Field{{Name: "consumed", Title: "Consumed", Type: "boolean"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "actions": []construction.Action{{Name: "apply", Title: "Apply", From: []string{"draft"}, Inputs: []construction.Input{{Name: "qty", Title: "Second quantity", Type: "integer"}, {Name: "credit", Title: "Credit", Type: "decimal"}}, Transaction: txn}}}, txnAt)
	decide(t, tenant, "builder", construction.ID, request+".create", request, "R1", map[string]any{}, txnAt)
	before := txnState(tenant)
	position := len(entries)
	member := memberOf(t, tenant, "builder")
	_, err, issues := tenant.submitDiagnosed(member, submission(tenant, member, construction.ID, request+".apply", request, "R1", map[string]any{"qty": 4, "credit": 1}), txnAt)
	if err == nil || len(issues) != 1 || issues[0].Constraint != "total" {
		t.Fatalf("unbalanced transaction accepted: %v %+v", err, issues)
	}
	if txnState(tenant) != before || len(tenant.sequences.clone()) != 0 {
		t.Fatal("transaction-level failure left rows, relationship or number")
	}
	if len(entries) != position+1 {
		t.Fatal("failure produced additional ledger entries")
	}
	saved, _, decodeErr := decodeRefusedResult(entries[len(entries)-1].Body)
	if decodeErr != nil || saved.Version != 3 {
		t.Fatal("constraint refusal not versioned", decodeErr)
	}
	_, err, issues = tenant.submitDiagnosed(member, submission(tenant, member, construction.ID, request+".apply", request, "R1", map[string]any{"qty": 3, "credit": 0.99}), txnAt)
	if err == nil || len(issues) != 1 || issues[0].Constraint != "balanced" || txnState(tenant) != before || len(tenant.sequences.clone()) != 0 {
		t.Fatal("unbalanced amounts escaped the atomic boundary", err, issues)
	}
	receipt := decide(t, tenant, "builder", construction.ID, request+".apply", request, "R1", map[string]any{"qty": 3, "credit": 1}, txnAt)
	batch, _, decodeErr := decodeAcceptedBatch(entries[len(entries)-1].Body)
	if decodeErr != nil || len(batch.Rows) != 4 || len(batch.Decisions) != 1 || len(batch.Sequences) != 1 {
		t.Fatalf("reference and rows did not share one result: %+v %v", batch, decodeErr)
	}
	headID := receipt.ChangeId + "-head"
	for _, image := range batch.Rows {
		if image.Type == line {
			var values map[string]any
			_ = json.Unmarshal(image.Value, &values)
			if values["header"] != headID {
				t.Fatal("relation points outside the accepted header")
			}
		}
	}
	before = txnState(tenant)
	_, err, issues = tenant.submitDiagnosed(member, submission(tenant, member, construction.ID, line+".edit", line, receipt.ChangeId+"-second", map[string]any{"qty": 4}), txnAt)
	if err == nil || len(issues) != 1 || issues[0].Constraint != "total" || txnState(tenant) != before {
		t.Fatal("child edit bypassed unchanged parent total", err, issues)
	}
	// Preserve the total while independently demonstrating archive cardinality.
	if result := refuse(t, tenant, "builder", construction.ID, header+".archive", header, headID, nil, txnAt); !strings.Contains(result, "active lines") {
		t.Fatal("archive escaped relation constraint", result)
	}
	restored := constraintTenant(t)
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if snapshot(restored) != snapshot(tenant) {
		t.Fatal("constraint transaction replay reran or changed results")
	}
}

func TestBusinessConstraintsRequiredPatternTemporalAndScopedUnique(t *testing.T) {
	tenant := constraintTenant(t)
	parent := publishObject(t, tenant, "builder", "PARENT", map[string]any{"name": "parent", "title": "Parent", "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}}}, txnAt)
	decide(t, tenant, "builder", construction.ID, parent+".create", parent, "P1", map[string]any{"name": "One"}, txnAt)
	decide(t, tenant, "builder", construction.ID, parent+".create", parent, "P2", map[string]any{"name": "Two"}, txnAt)
	constraints := []platform.Constraint{
		{Name: "value", Kind: "required", Fields: []string{"code"}, Message: "Code is required."},
		{Name: "perparent", Kind: "unique", Fields: []string{"code"}, Scope: "parent", Message: "Code is unique within its parent."},
		{Name: "shape", Kind: "pattern", Fields: []string{"code"}, Message: "Use two uppercase letters.", Expr: booleanRule("matches", platform.ExprReference("text", "code"), platform.ExprLiteralValue("text", "[A-Z]{2}"))},
		{Name: "valid", Kind: "temporal", Fields: []string{"date"}, Message: "Date must be in the validity period.", Expr: booleanRule("within-period", platform.ExprReference("date", "date"), platform.ExprLiteralValue("date", "2026-10-01"), platform.ExprLiteralValue("date", "2026-10-31"))},
	}
	typ := publishObject(t, tenant, "builder", "CHILD", map[string]any{"name": "child", "title": "Child", "fields": []construction.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: parent}, {Name: "code", Title: "Code", Type: "text"}, {Name: "date", Title: "Date", Type: "date"}}, "constraints": constraints}, txnAt)
	valid := map[string]any{"parent": "P1", "code": "AB", "date": "2026-10-12"}
	decide(t, tenant, "builder", construction.ID, typ+".create", typ, "C1", valid, txnAt)
	other := maps.Clone(valid)
	other["parent"] = "P2"
	decide(t, tenant, "builder", construction.ID, typ+".create", typ, "C2", other, txnAt)
	for _, bad := range []struct {
		field string
		value any
		name  string
	}{{"code", "", "value"}, {"code", "AB", "perparent"}, {"code", "axb", "shape"}, {"date", "2026-11-01", "valid"}} {
		payload := maps.Clone(valid)
		payload["code"] = "CD"
		payload[bad.field] = bad.value
		_, err, issues := tenant.submitDiagnosed(memberOf(t, tenant, "builder"), submission(tenant, memberOf(t, tenant, "builder"), construction.ID, typ+".create", typ, "BAD"+bad.name, payload), txnAt)
		if err == nil || len(issues) != 1 || issues[0].Constraint != bad.name {
			t.Fatalf("%s: %v %+v", bad.name, err, issues)
		}
	}
	// Empty CSV edits must clear the cell and reject the entire row.
	rows, err := tenant.Import(memberOf(t, tenant, "builder"), typ, []byte("id,code\nC1,\n"), false, txnAt)
	if err != nil || len(rows) != 1 || rows[0].Error == nil || rows[0].Error.Issues[0].Constraint != "value" {
		t.Fatalf("blank CSV silently retained code: %+v %v", rows, err)
	}
}

func TestBusinessConstraintsAffectedReferencesAndWithheldFacts(t *testing.T) {
	tenant := constraintTenant(t)
	holder := publishObject(t, tenant, "builder", "HOLDER", map[string]any{"name": "holder", "title": "Holder", "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}}}, txnAt)
	decide(t, tenant, "builder", construction.ID, holder+".create", holder, "H1", map[string]any{"name": "Group"}, txnAt)
	parent := publishObject(t, tenant, "builder", "RESOURCE", map[string]any{"name": "resource", "title": "Resource", "fields": []construction.Field{{Name: "capacity", Title: "Capacity", Type: "integer", Read: []string{"builder"}}}}, txnAt)
	decide(t, tenant, "builder", construction.ID, parent+".create", parent, "P1", map[string]any{"capacity": 10}, txnAt)
	path := platform.Expr{Op: "path", Kind: "integer", Args: []platform.Expr{platform.ExprReference("reference", "resource")}, Path: []string{"capacity"}}
	rule := platform.Constraint{Name: "capacity", Kind: "expression", Fields: []string{"qty"}, Message: "Capacity must cover quantity.", Expr: booleanRule("<=", platform.ExprReference("integer", "qty"), path)}
	child := publishObject(t, tenant, "builder", "RESERVATION", map[string]any{"name": "reservation", "title": "Reservation", "fields": []construction.Field{{Name: "resource", Title: "Resource", Type: "reference", Ref: parent}, {Name: "holder", Title: "Holder", Type: "reference", Ref: holder}, {Name: "qty", Title: "Quantity", Type: "integer"}}, "constraints": []platform.Constraint{rule}}, txnAt)
	decide(t, tenant, "builder", construction.ID, child+".create", child, "C1", map[string]any{"resource": "P1", "holder": "H1", "qty": 2}, txnAt)
	before := txnState(tenant)
	member := memberOf(t, tenant, "builder")
	_, err, issues := tenant.submitDiagnosed(member, submission(tenant, member, construction.ID, parent+".edit", parent, "P1", map[string]any{"capacity": 1}), txnAt)
	if err == nil || len(issues) != 1 || issues[0].Constraint != "capacity" || txnState(tenant) != before {
		t.Fatal("source edit broke an unchanged dependent record", err, issues)
	}
	decide(t, tenant, "builder", construction.ID, "build.query.create", construction.QueryType, "RESERVATIONS", map[string]any{"name": "reservations", "title": "Reservations", "description": "Reservations of one holder", "object": child, "by": "holder", "limit": 200}, txnAt)
	decide(t, tenant, "builder", construction.ID, "build.query.publish", construction.QueryType, "RESERVATIONS", nil, txnAt)
	itemPath := platform.Expr{Op: "path", Kind: "integer", Args: []platform.Expr{platform.ExprReference("reference", "item.resource")}, Path: []string{"capacity"}}
	predicate := booleanRule("<=", platform.ExprReference("integer", "item.qty"), itemPath)
	groupRule := platform.Constraint{Name: "itemsafety", Kind: "expression", Fields: []string{"name"}, Level: "txn", Message: "Every item must fit its resource.", Expr: &platform.Expr{Op: "all", Kind: "boolean", Agg: &platform.ExprAggregate{Source: "items"}, In: predicate}, Reads: &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{"items": {App: construction.ID, Query: "reservations", Version: 1, Object: child, Of: "$subject", Policy: "complete"}}}}
	decide(t, tenant, "builder", construction.ID, "build.object.edit", construction.ObjectType, "HOLDER", map[string]any{"constraints": []platform.Constraint{groupRule}}, txnAt)
	decide(t, tenant, "builder", construction.ID, "build.object.publish", construction.ObjectType, "HOLDER", nil, txnAt)
	_, err, issues = tenant.submitDiagnosed(member, submission(tenant, member, construction.ID, parent+".edit", parent, "P1", map[string]any{"capacity": 1}), txnAt)
	if err == nil || len(issues) != 1 || issues[0].Constraint != "itemsafety" {
		t.Fatal("a reference reached from a collection escaped affected closure", err, issues)
	}
	// The business actor can read the dependent record, but not the source
	// capacity. A withheld fact refuses, and the private source value is absent.
	user := memberOf(t, tenant, "reader")
	d := tenant.newStagedDecision()
	d.at = txnAt
	c := platform.NewCaller(d, user, construction.ID, false, false)
	info := d.records.types[child].info
	after := d.records.types[child].rows.Get("C1")
	if err := d.checkConstraint(c, info, rule, after, after); err == nil {
		t.Fatal("withheld rule input silently passed")
	} else {
		diagnostics := d.inputIssues[err]
		if len(diagnostics) != 1 || diagnostics[0].Reason != "withheld" || diagnostics[0].Values["capacity"] != nil {
			t.Fatalf("private constraint diagnostic leaked source: %+v", diagnostics)
		}
	}
}
