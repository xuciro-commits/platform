package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type connectorOrder struct {
	platform.Record
	Name string `json:"name"`
}

type connectorOrderApp struct {
	ledger *platform.Ledger
	calls  int
}

func newConnectorOrderApp(tenant string) *connectorOrderApp {
	catalog := platform.NewCatalog(platform.Action{Schema: "connector.order.commit", Target: "connector.order",
		Title: "Commit order", Description: "Commit an order with its source cursor",
		Payload: []platform.Field{{Name: "from", Type: "string"}, {Name: "to", Type: "string"}},
		Roles:   []string{"clerk"}})
	return &connectorOrderApp{ledger: platform.NewLedger(tenant, "connector-order", catalog, "connector.order")}
}
func (a *connectorOrderApp) Manifest() platform.Manifest {
	return platform.Manifest{ID: "connector-order", Title: "Orders", Version: "1", Actions: a.ledger.Catalog,
		Entities: []platform.Entity{{Type: "connector.order", Title: "Order", Model: connectorOrder{}}}}
}
func (a *connectorOrderApp) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *connectorOrderApp) AcceptedLedger() *platform.Ledger         { return a.ledger }
func (*connectorOrderApp) AcceptedActionSchemas() []string {
	return []string{"connector.order.commit"}
}
func (a *connectorOrderApp) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }
func (a *connectorOrderApp) Restore(raw json.RawMessage) error  { return a.ledger.Restore(raw) }
func (*connectorOrderApp) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, unknown()
}
func (*connectorOrderApp) Read(platform.Caller, string) (any, *kernel.Error) { return nil, unknown() }
func (a *connectorOrderApp) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.calls++
	var cursor struct{ From, To string }
	if json.Unmarshal(sub.GetPayload(), &cursor) != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return a.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(r *pb.ChangeRecord) {
			if err := c.Deliver("connector.order", cursor.From, cursor.To, now); err != nil {
				return
			}
			c.Put(r, connectorOrder{Record: platform.Record{ID: sub.GetTarget().GetId()}, Name: cursor.To})
		}, nil
	})
}

func connectorOrderTenant(t *testing.T, id string) *Tenant {
	t.Helper()
	seat := Seat{Subjects: []string{"gateway"}, Member: platform.Member{
		ID: "gateway", Roles: map[string]string{"connector-order": "clerk"}}}
	tn, err := NewTenant(id, NewConsole(id, seat), newConnectorOrderApp(id))
	if err != nil {
		t.Fatal(err)
	}
	if err := tn.Connect(&pb.ConnectorDescriptor{ConnectorId: "gateway",
		Direction:   pb.ConnectorDirection_CONNECTOR_DIRECTION_POLL,
		DataClasses: []string{"connector.order"}, Heartbeat: durationpb.New(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return tn
}

func connectorOrderSubmission(id string) *pb.Submission {
	return &pb.Submission{TenantId: id, PrincipalId: "gateway", Authority: "connector-order",
		IdempotencyKey: "cursor-1", Target: &pb.EntityRef{Type: "connector.order", Id: "O1"},
		Schema: &pb.SchemaRef{Name: "connector.order.commit", Version: 1}, Payload: []byte(`{"from":"","to":"cursor-1"}`)}
}

func TestAcceptedConnectorCursorAndDecisionShareOneResult(t *testing.T) {
	compose := func() *Tenant { return connectorOrderTenant(t, "accepted-cursor") }
	tn := compose()
	member, _ := tn.Member("gateway")
	now := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	sub := connectorOrderSubmission(tn.ID)
	var entries []Entry
	fail := true
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		status, _ := tn.connectors.Status(tn.ID, member.ID, now)
		if status.GetCursor() != "" || tn.records.types["connector.order"].rows["O1"] != nil {
			t.Fatal("connector cursor or order escaped before the durable result")
		}
		if fail {
			return nil, errors.New("append failure")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if _, err := tn.Submit(member, sub, now); err == nil {
		t.Fatal("append failure accepted a cursor")
	}
	fail = false
	receipt, refusal := tn.Submit(member, sub, now)
	if refusal != nil || receipt == nil || len(entries) != 1 {
		t.Fatalf("connector order was not accepted: %v", refusal)
	}
	status, _ := tn.connectors.Status(tn.ID, member.ID, now)
	if status.GetCursor() != "cursor-1" || tn.records.types["connector.order"].rows["O1"] == nil {
		t.Fatal("committed cursor and order were not installed together")
	}
	again, refusal := tn.Submit(member, sub, now.Add(time.Minute))
	if refusal != nil || again.GetChangeId() != receipt.GetChangeId() || len(entries) != 1 {
		t.Fatalf("a duplicate reran the cursor transition: %v", refusal)
	}
	batch, _, err := decodeAcceptedBatch(entries[0].Body)
	if err != nil || len(batch.Deliveries) != 1 || batch.Deliveries[0].To != "cursor-1" {
		t.Fatalf("result omitted the cursor transition: %+v %v", batch, err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	got, _ := recovered.connectors.Status(recovered.ID, member.ID, now)
	if got.GetCursor() != "cursor-1" {
		t.Fatal("recovery did not apply the saved cursor")
	}
	CheckReplay(t, tn, entries, compose)

	damaged := entries[0]
	batch.Deliveries[0].Before = "not-the-prior-mark"
	batch.Digest, _ = digestAcceptedBatch(batch)
	damaged.Body, _ = json.Marshal(batch)
	isolated := compose()
	if err := isolated.recoverEntries([]Entry{damaged}); err == nil || !isolated.quarantined() {
		t.Fatal("corrupt connector predecessor did not quarantine its tenant")
	}
	unchanged, _ := isolated.connectors.Status(isolated.ID, member.ID, now)
	if unchanged.GetCursor() != "" || isolated.records.types["connector.order"].rows["O1"] != nil {
		t.Fatal("corrupt cursor transition changed a record or connector")
	}
}

func TestAcceptedConnectorSavepointDiscardsRejectedDelivery(t *testing.T) {
	tn := stockTenant(t)
	now := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	if err := tn.Connect(&pb.ConnectorDescriptor{ConnectorId: "ana",
		Direction:   pb.ConnectorDirection_CONNECTOR_DIRECTION_POLL,
		DataClasses: []string{"stock.item"}, Heartbeat: durationpb.New(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	member, _ := tn.Member("ana")
	draft := tn.newStagedDecision()
	c := platform.NewCaller(draft, member, "stock", false, false)
	_, refusal := draft.Attempt(func() (*pb.ChangeRecord, *kernel.Error) {
		if err := c.Deliver("stock.item", "", "page-one", now); err != nil {
			return nil, err
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	})
	if refusal == nil || len(draft.deliveries) != 0 {
		t.Fatal("refused attempt kept its private connector transition")
	}
	if err := draft.Deliver(c, "stock.item", "", "page-two", now); err != nil {
		t.Fatalf("refused attempt reserved the cursor: %v", err)
	}
	if mark, _ := tn.connectors.Status(tn.ID, member.ID, now); mark.GetCursor() != "" {
		t.Fatal("savepoint changed the live connector")
	}
}

func TestJournalAcceptedConnectorCrashAfterCommit(t *testing.T) {
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
	id := fmt.Sprintf("accepted-cursor-%d", time.Now().UnixNano())
	defer j.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	if entries, err := j.Entries(ctx, id, 0); err != nil || len(entries) != 0 {
		t.Fatalf("new journal is not empty: %v %v", entries, err)
	}
	live := connectorOrderTenant(t, id)
	member, _ := live.Member("gateway")
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	sub := connectorOrderSubmission(id)
	live.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		if _, err := j.AppendAccepted(ctx, id, entry, key, hash); err != nil {
			return nil, err
		}
		return nil, errors.New("simulated crash after commit")
	}
	if _, refusal := live.Submit(member, sub, now); refusal == nil {
		t.Fatal("interrupted reply was accepted")
	}
	if mark, _ := live.connectors.Status(id, member.ID, now); mark.GetCursor() != "" ||
		live.records.types["connector.order"].rows["O1"] != nil {
		t.Fatal("crash applied cursor or record without a recovered result")
	}
	next, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	entries, err := next.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("PostgreSQL lost the committed result: %v %v", entries, err)
	}
	recovered := connectorOrderTenant(t, id)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.app("connector-order").(*connectorOrderApp).calls != 0 {
		t.Fatal("recovery reran the application decision")
	}
	if mark, _ := recovered.connectors.Status(id, member.ID, now); mark.GetCursor() != "cursor-1" ||
		recovered.records.types["connector.order"].rows["O1"] == nil {
		t.Fatal("recovery did not apply both cursor and record")
	}
	recovered.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		return next.AppendAccepted(ctx, id, entry, key, hash)
	}
	if receipt, refusal := recovered.Submit(member, sub, now.Add(time.Minute)); refusal != nil ||
		receipt == nil || next.Position(id) != 1 || recovered.app("connector-order").(*connectorOrderApp).calls != 0 {
		t.Fatalf("retry reran the committed decision: %v", refusal)
	}
	CheckReplay(t, recovered, entries, func() *Tenant { return connectorOrderTenant(t, id) })
}
