package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

func TestAcceptedNestedAttemptOwnsPreparedComputeAndPrivateState(t *testing.T) {
	for _, outcome := range []string{"accepted", "refused", "panic", "probe"} {
		t.Run(outcome, func(t *testing.T) {
			const id = "nested-compute-savepoint"
			source := newFrameDataComputeSource(id, 256<<10)
			tn := composeTenant(t, id, []Seat{seatOf("admin", PlatformApp+":"+Admin, "stock:clerk", "frames:operator")},
				work.New(id), flow.New(id), newStock(id), source)
			tn.Files = &memoryFiles{}
			if err := tn.files().Put(context.Background(), artifactKey(id, source.moduleDigest), source.module, "application/wasm"); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
			member := memberOf(t, tn, "admin")
			draft := tn.newStagedDecision()
			caller := platform.NewCaller(draft, member, PlatformApp, false, false)
			stockCaller := platform.NewCaller(draft, member, "stock", false, false)
			if _, err := draft.Decide(stockCaller, tn.app("stock"), submission(tn, member, "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 2}), at); err != nil {
				t.Fatal(err)
			}
			receipt, err := draft.Decide(caller, tn.app(PlatformApp), submission(tn, member, PlatformApp, SchemaAdd, MemberType, "parent", map[string]any{"subject": "user:parent@example.test"}), at)
			if err != nil {
				t.Fatal(err)
			}
			caller.Notify(platform.Notification{Title: "Parent", Key: "parent"}, at, platform.Recipient{Member: member.ID})
			q := platform.OperationRequest{App: "frames", Name: "aggregate", Key: "prepared", Inputs: platform.Raw(map[string]string{"payload": "retained parent input"})}
			op, version, ok := operationDefinition(source, q.Name, 0)
			if !ok {
				t.Fatal("Compute definition is missing")
			}
			definition, _ := canonicalDigest([]any{q.App, op, version})
			inputHash, _ := canonicalDigest(q.Inputs)
			ref, sealErr := tn.staged.sealInput(id+":frames:operation:"+q.Key+":"+operationEndpoint, member.ID, definition, inputHash, q.Inputs, op.Limits.DataInputBytes)
			if sealErr != nil {
				t.Fatal(sealErr)
			}
			t.Cleanup(func() { tn.staged.discardInput(ref) })
			requestHash, _ := canonicalDigest(q)
			draft.operationInputs = map[string][]*preparedOperationInput{requestHash: {{ref: ref, requestHash: requestHash}}}
			draft.operationAnswers = map[string]platform.OperationResult{"prior": {ID: "prior", Output: []byte(`[1]`), Ref: &platform.AssetRef{App: "frames", Kind: platform.AssetCompute, Name: "aggregate"}}}
			parentRefusal := platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Parent issue")
			draft.InputRefused(parentRefusal, []platform.FieldIssue{{Code: "required", Path: []string{"parent"}}})
			liveBefore := snapshot(tn)
			child := func(c platform.Caller) *kernel.Error {
				stockChild := platform.RouteCaller(c, "stock")
				if _, err := platform.Decide(stockChild, tn.app("stock"), submission(tn, member, "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 99}), at); err != nil {
					t.Fatal(err)
				}
				if _, err := platform.Decide(c, tn.app(PlatformApp), submission(tn, member, PlatformApp, SchemaAdd, MemberType, "child", map[string]any{"subject": "user:child@example.test"}), at); err != nil {
					t.Fatal(err)
				}
				c.Notify(platform.Notification{Title: "Child", Key: "child"}, at, platform.Recipient{Member: member.ID})
				answer, err := c.OperationResult("prior")
				if err != nil {
					t.Fatal(err)
				}
				answer.Output[1], answer.Ref.Name = '2', "child"
				childRefusal := c.RefuseFields([]platform.FieldIssue{{Code: "required", Path: []string{"child"}}})
				if _, err := c.RequestOperation(receipt, q); err != nil {
					t.Fatal(err)
				}
				switch outcome {
				case "refused":
					return childRefusal
				case "panic":
					panic("child rule panic")
				}
				return nil
			}
			if outcome == "probe" {
				if err := platform.ProbeDecision(caller, child); err != nil {
					t.Fatal(err)
				}
			} else {
				func() {
					defer func() {
						if got := recover(); outcome == "panic" && got != "child rule panic" || outcome != "panic" && got != nil {
							t.Fatalf("child panic: %v", got)
						}
					}()
					_, err := draft.Attempt(func() (*pb.ChangeRecord, *kernel.Error) { return nil, child(caller) })
					if (err != nil) != (outcome == "refused") {
						t.Fatalf("child refusal: %v", err)
					}
				}()
			}
			accepted := outcome == "accepted"
			wantQty, wantEvents, wantNotices, wantPending, wantIssues := 2, 2, 1, 1, 1
			if accepted {
				wantQty, wantEvents, wantNotices, wantPending, wantIssues = 99, 4, 2, 0, 2
			}
			item, _ := platform.Get[Item](stockCaller, "I1")
			_, hasChild := draft.states[PlatformApp].(*Console).Member("user:child@example.test")
			_, hasParent := draft.states[PlatformApp].(*Console).Member("user:parent@example.test")
			if item.Qty != wantQty || len(draft.events) != wantEvents || len(draft.notices) != wantNotices || !hasParent || hasChild != accepted || len(draft.inputIssues) != wantIssues || pendingOperationInputs(draft.operationInputs) != wantPending || snapshot(tn) != liveBefore {
				t.Fatalf("child escaped or lost its savepoint: qty=%d events=%d notices=%d child=%t parent=%t issues=%d inputs=%d", item.Qty, len(draft.events), len(draft.notices), hasChild, hasParent, len(draft.inputIssues), pendingOperationInputs(draft.operationInputs))
			}
			if !accepted {
				answer, _ := caller.OperationResult("prior")
				if string(answer.Output) != `[1]` || answer.Ref.Name != "aggregate" || len(draft.intents) != 0 || draft.failure != nil {
					t.Fatal("discarded child changed a parent answer, intent or failure")
				}
				if _, err := caller.RequestOperation(receipt, q); err != nil {
					t.Fatalf("parent cannot consume its prepared input after %s: %v", outcome, err)
				}
			}
			var binding operationBinding
			if len(draft.intents) != 1 || pendingOperationInputs(draft.operationInputs) != 0 || json.Unmarshal([]byte(draft.intents[0].Body), &binding) != nil || binding.SealedInput == nil || *binding.SealedInput != ref {
				t.Fatal("successful parent/child did not consume exactly its prepared artifact once")
			}
		})
	}
}

type nestedDocument struct {
	platform.Record
	Child  string `json:"child"`
	Number string `json:"number"`
}

type nestedIssuer struct {
	ledger  *platform.Ledger
	host    host.Host
	cycle   bool
	reject  bool
	calls   int
	publish bool
}

func newNestedIssuer(tenant string) *nestedIssuer {
	catalog := platform.NewCatalog(platform.Action{Schema: "nested.document.issue", Target: "nested.document",
		New: true, Title: "Issue", Description: "Issue a document and its child record.", Payload: []platform.Field{},
		Capability: "documents", Roles: []string{"clerk"}})
	return &nestedIssuer{ledger: platform.NewLedger(tenant, "nested", catalog, "nested.document")}
}
func (n *nestedIssuer) Attach(h host.Host) { n.host = h }
func (n *nestedIssuer) Manifest() platform.Manifest {
	return platform.Manifest{ID: "nested", Title: "Nested", Version: "1", Actions: n.ledger.Catalog,
		Sequences: []platform.Sequence{{Name: "document", Pattern: "D/{n}"}},
		Entities: []platform.Entity{{Type: "nested.document", Title: "Document",
			Description: "A document with a child record.", Model: nestedDocument{}}}}
}
func (n *nestedIssuer) Declarations() []*pb.AuthorityDeclaration { return n.ledger.Declarations() }
func (n *nestedIssuer) AcceptedLedger() *platform.Ledger         { return n.ledger }
func (n *nestedIssuer) Snapshot() (json.RawMessage, error)       { return n.ledger.Snapshot() }
func (n *nestedIssuer) Restore(raw json.RawMessage) error        { return n.ledger.Restore(raw) }
func (*nestedIssuer) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, unknown()
}
func (*nestedIssuer) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, unknown()
}
func (n *nestedIssuer) Submit(c platform.Caller, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	n.calls++
	return n.ledger.Receive(c, sub, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if n.cycle {
			_, err := n.host.Submit(c, sub, at)
			return nil, err
		}
		child := &pb.Submission{TenantId: sub.GetTenantId(), PrincipalId: c.ID, Authority: "stock",
			IdempotencyKey: sub.GetIdempotencyKey() + ":create",
			Target:         &pb.EntityRef{Type: "stock.item", Id: sub.GetTarget().GetId() + "-child"},
			Schema:         &pb.SchemaRef{Name: "stock.item.create", Version: 1},
			Payload:        []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}
		if _, err := n.host.Submit(c, child, at); err != nil {
			return nil, err
		}
		// Read then edit a record created inside the same parent transaction.
		child.IdempotencyKey = sub.GetIdempotencyKey() + ":edit"
		child.Schema = &pb.SchemaRef{Name: "stock.item.edit", Version: 1}
		child.Payload = []byte(`{"qty":7}`)
		if _, err := n.host.Submit(c, child, at); err != nil {
			return nil, err
		}
		item, ok := platform.Get[Item](n.host.As(c, "stock"), child.GetTarget().GetId())
		if !ok || item.Qty != 7 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Nested record is not visible in the decision")
		}
		if n.publish {
			object := &pb.Submission{TenantId: sub.GetTenantId(), PrincipalId: c.ID, Authority: build.ID,
				IdempotencyKey: sub.GetIdempotencyKey() + ":object", Target: &pb.EntityRef{Type: build.ObjectType, Id: "O1"},
				Schema:  &pb.SchemaRef{Name: build.ObjectType + ".create", Version: 1},
				Payload: []byte(`{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`)}
			if _, err := n.host.Submit(c, object, at); err != nil {
				return nil, err
			}
			object.IdempotencyKey, object.Schema.Name, object.Payload = sub.GetIdempotencyKey()+":publish", build.SchemaPublish, []byte(`{}`)
			if _, err := n.host.Submit(c, object, at); err != nil {
				return nil, err
			}
		}
		if n.reject {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		return func(r *pb.ChangeRecord) {
			number, _ := c.Next(r, "document", at)
			c.Put(r, nestedDocument{Record: platform.Record{ID: sub.GetTarget().GetId()}, Child: item.ID, Number: number})
		}, nil
	})
}

func nestedTenant(t *testing.T) *Tenant {
	return nestedTenantID(t, "nested-tenant")
}

func nestedTenantID(t *testing.T, id string, extra ...platform.App) *Tenant {
	t.Helper()
	seat := Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana",
		Roles: map[string]string{"stock": "clerk", "nested": "clerk"}}}
	for _, app := range extra {
		if app.Manifest().ID == build.ID {
			seat.Member.Roles[build.ID] = build.Builder
		}
	}
	apps := append([]platform.App{NewConsole(id, seat), newStock(id), newNestedIssuer(id)}, extra...)
	tn, err := NewTenant(id, apps...)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func TestAcceptedNestedPublicationSharesParentCommitAndInstallation(t *testing.T) {
	compose := func() *Tenant {
		return nestedTenantID(t, "nested-publish", build.New("nested-publish"))
	}
	live := compose()
	live.app("nested").(*nestedIssuer).publish = true
	member, _ := live.Member("ana")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	var entries []Entry
	fail := true
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if _, exists := live.owner["action:build.visit.create"]; exists ||
			live.records.types["build.visit"] != nil || len(live.app(build.ID).(platform.App).AcceptedLedger().RecordsFor(live.ID)) != 0 {
			t.Fatal("nested publication installed before the parent's commit")
		}
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	sub := nestedSubmission(live, "D1")
	before := snapshot(live)
	if _, err := live.Submit(member, sub, at); err == nil || snapshot(live) != before {
		t.Fatal("failed parent append left a child publication or ledger")
	}
	fail = false
	if _, err := live.Submit(member, sub, at); err != nil {
		t.Fatalf("nested publication: %v fault=%+v", err, live.fault.Load())
	}
	result, _, err := decodeAcceptedBatch(entries[0].Body)
	if err != nil || len(result.Decisions) != 5 || len(result.Rows) != 3 ||
		!slices.ContainsFunc(result.Decisions, func(d acceptedDecision) bool { return d.Publication != nil }) ||
		live.owner["action:build.visit.create"] == nil || live.records.types["build.visit"] == nil {
		t.Fatalf("nested publication was not installed from its one saved result: %v", err)
	}
	CheckReplay(t, live, entries, compose)
}

func nestedSubmission(tn *Tenant, key string) *pb.Submission {
	return &pb.Submission{TenantId: tn.ID, PrincipalId: "ana", Authority: "nested", IdempotencyKey: key,
		Target: &pb.EntityRef{Type: "nested.document", Id: key},
		Schema: &pb.SchemaRef{Name: "nested.document.issue", Version: 1}, Payload: []byte(`{}`)}
}

func TestAcceptedNestedDecisionsShareOneCommitAndRecovery(t *testing.T) {
	live := nestedTenant(t)
	member, _ := live.Member("ana")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	var entries []Entry
	fail := true
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		// Neither the parent nor either nested decision may be public yet.
		if len(live.app("stock").(*stock).ledger.RecordsFor(live.ID)) != 0 ||
			len(live.app("nested").(*nestedIssuer).ledger.RecordsFor(live.ID)) != 0 ||
			live.records.types["stock.item"].rows["D1-child"] != nil ||
			live.sequences.last["nested/document/0"] != 0 {
			t.Fatal("a nested decision escaped before the one journal append")
		}
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	sub := nestedSubmission(live, "D1")
	if _, err := live.Submit(member, sub, at); err == nil || len(entries) != 0 {
		t.Fatalf("failed outer append was accepted: %v", err)
	}
	fail = false
	receipt, refused := live.Submit(member, sub, at)
	if refused != nil || receipt == nil || len(entries) != 1 {
		t.Fatalf("nested decisions did not commit together: %+v, %v, fault=%+v, entries=%+v", receipt, refused, live.fault.Load(), entries)
	}
	result, _, err := decodeAcceptedBatch(entries[0].Body)
	if err != nil || len(result.Decisions) != 3 || len(result.Rows) != 2 {
		t.Fatalf("one batch omitted nested receipts or rows: %+v, %v", result, err)
	}
	if live.sequences.last["nested/document/0"] != 1 || result.SequenceBases["nested/document/0"] != 0 {
		t.Fatal("the number was not committed with its predecessor")
	}
	item := live.records.types["stock.item"].rows["D1-child"]
	if item == nil || item.value.Interface().(Item).Qty != 7 || len(item.history) != 2 {
		t.Fatal("the committed nested create/edit result is incomplete")
	}
	calls := live.app("nested").(*nestedIssuer).calls
	if retried, err := live.Submit(member, sub, at.Add(time.Hour)); err != nil ||
		retried.GetChangeId() != receipt.GetChangeId() || len(entries) != 1 ||
		live.app("nested").(*nestedIssuer).calls != calls {
		t.Fatal("top-level retry ran the nested business decisions again")
	}
	CheckReplay(t, live, entries, func() *Tenant { return nestedTenant(t) })
	if applied, err := live.applyAcceptedBatch(live.app("nested").(*nestedIssuer).ledger, entries[0].Body); err != nil || applied {
		t.Fatalf("reapplying the whole result was not a no-op: new=%t, %v", applied, err)
	}
}

func TestAcceptedNestedRefusalDiscardsEveryChild(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		live := nestedTenant(t)
		parent := live.app("nested").(*nestedIssuer)
		parent.reject, parent.cycle = true, cycle
		member, _ := live.Member("ana")
		var entries []Entry
		live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
			entries = append(entries, e)
			return e.Body, nil
		}
		if _, err := live.Submit(member, nestedSubmission(live, "refused"), time.Now()); err == nil ||
			len(entries) != 1 {
			t.Fatalf("refused/cyclic parent had no durable refusal: cycle=%t, %v", cycle, err)
		}
		if _, _, err := decodeRefusedResult(entries[0].Body); err != nil {
			t.Fatal(err)
		}
		if len(live.app("stock").(*stock).ledger.RecordsFor(live.ID)) != 0 ||
			len(parent.ledger.RecordsFor(live.ID)) != 0 ||
			len(live.records.types["stock.item"].rows) != 0 || len(live.events) != 0 {
			t.Fatal("a refused parent exposed a child receipt, record or event")
		}
	}
}

func TestAcceptedNestedBatchValidatesAllLedgersBeforeApplication(t *testing.T) {
	source := nestedTenant(t)
	member, _ := source.Member("ana")
	var entry Entry
	source.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entry = e; return e.Body, nil }
	if _, err := source.Submit(member, nestedSubmission(source, "D1"), time.Now()); err != nil {
		t.Fatalf("%v, fault=%+v, entry=%s", err, source.fault.Load(), entry.Body)
	}
	result, _, err := decodeAcceptedBatch(entry.Body)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the final parent image consistent with a bad parent receipt so the
	// row checks pass, and ensure its later ledger check cannot partially apply
	// the earlier stock ledger. An impossible epoch is checked by the kernel.
	last := len(result.Decisions) - 1
	bad := new(pb.ChangeRecord)
	if err := protojson.Unmarshal(result.Decisions[last].Receipt, bad); err != nil {
		t.Fatal(err)
	}
	bad.Revision = 9
	result.Decisions[last].Receipt, _ = protojson.Marshal(bad)
	result.Receipt = result.Decisions[last].Receipt
	for i := range result.Rows {
		if result.Rows[i].App == "nested" {
			var record nestedDocument
			if err := json.Unmarshal(result.Rows[i].Value, &record); err != nil {
				t.Fatal(err)
			}
			record.Revision = 9
			result.Rows[i].Value, _ = json.Marshal(record)
		}
	}
	result.Digest, _ = digestAcceptedBatch(result)
	raw, _ := json.Marshal(result)
	recovered := nestedTenant(t)
	if _, err := recovered.applyAcceptedBatch(recovered.app("nested").(*nestedIssuer).ledger, raw); err == nil {
		t.Fatal("invalid final parent receipt was accepted")
	}
	if len(recovered.app("stock").(*stock).ledger.RecordsFor(recovered.ID)) != 0 ||
		len(recovered.app("nested").(*nestedIssuer).ledger.RecordsFor(recovered.ID)) != 0 ||
		len(recovered.records.types["stock.item"].rows) != 0 || recovered.sequences.last["nested/document/0"] != 0 {
		t.Fatal("a late invalid receipt exposed an earlier child's state")
	}
}

func TestJournalAcceptedNestedBatchCrashBeforeApplication(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	id := fmt.Sprintf("nested-%d", time.Now().UnixNano())
	defer func() { _, _ = j.Pool().Exec(ctx, `delete from journal where tenant=$1`, id) }()
	compose := func() *Tenant { return nestedTenantID(t, id) }
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live := compose()
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if _, err := j.AppendAccepted(ctx, id, e, key, hash); err != nil {
			return nil, err
		}
		panic("injected crash after the one parent/child append")
	}
	m, _ := live.Member("ana")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	s := nestedSubmission(live, "D1")
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the append/application crash was not injected")
			}
		}()
		live.Submit(m, s, at)
	}()
	if len(live.app("stock").(*stock).ledger.RecordsFor(id)) != 0 ||
		len(live.app("nested").(*nestedIssuer).ledger.RecordsFor(id)) != 0 ||
		len(live.records.types["stock.item"].rows) != 0 || live.sequences.last["nested/document/0"] != 0 {
		t.Fatal("unapplied nested result leaked a record, receipt or number")
	}
	reopened, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("nested decisions did not share one durable row: %d, %v", len(entries), err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.app("nested").(*nestedIssuer).calls != 0 {
		t.Fatal("recovery reran the parent business decision")
	}
	recovered.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, e, key, hash)
	}
	if receipt, err := recovered.Submit(m, s, at.Add(time.Hour)); err != nil ||
		receipt.GetRevision() != 1 || reopened.Position(id) != 1 {
		t.Fatalf("nested retry changed the journal: %+v, %v", receipt, err)
	}
	CheckReplay(t, recovered, entries, compose)
}
