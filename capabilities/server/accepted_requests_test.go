package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type modelResultCart struct{ resultCart }

func (a *modelResultCart) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if s.GetSchema().GetName() == "cart.line.answer" {
		return a.resultCart.cart.Submit(c, s, at)
	}
	return a.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(r *pb.ChangeRecord) {
			c.Put(r, Cart{Record: platform.Record{ID: s.GetTarget().GetId()}, Item: "pen", Status: "asked"})
			for _, target := range []string{"first", "second"} {
				c.Request(r, platform.Request{Model: "local/chosen", Target: target,
					Payload: platform.Prompt{System: "Only classify.", User: target, MaxTokens: 12}, Reply: "cart.line.answer"})
			}
		}, nil
	})
}

type resultCart struct{ *cart }

func (a *resultCart) AcceptedLedger() *platform.Ledger { return a.ledger }
func (*resultCart) AcceptedActionSchemas() []string {
	return []string{"cart.line.add", "cart.line.ask", "cart.line.greedy", "cart.line.answer"}
}

type resultDepot struct {
	*depot
	forbidDecisions bool
}

func (a *resultDepot) AcceptedLedger() *platform.Ledger { return a.ledger }
func (*resultDepot) AcceptedActionSchemas() []string    { return []string{"stock.hold.make"} }
func (a *resultDepot) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if a.forbidDecisions {
		panic("recovery must not run the provider")
	}
	return a.depot.Submit(c, s, at)
}

func requestsResultTenant(t *testing.T) *Tenant {
	return requestsResultTenantFor(t, "t")
}

func requestsResultTenantFor(t *testing.T, id string) *Tenant {
	t.Helper()
	source := newRequestsTenant(t, true)
	tn, err := NewTenant(id, NewConsole(id),
		&resultCart{&cart{ledger: platform.NewLedger(id, "cart", source.app("cart").(*cart).ledger.Catalog, "cart.line")}},
		&resultDepot{depot: &depot{ledger: platform.NewLedger(id, "stock", source.app("stock").(*depot).ledger.Catalog, "stock.hold")}})
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func TestAcceptedProtocolRequestAndReplyShareCommit(t *testing.T) {
	tn := requestsResultTenant(t)
	member := platform.Member{ID: "buyer", Tenant: tn.ID, Roles: map[string]string{"cart": "buyer", "stock": "keeper"}}
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	sub := &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: "cart", IdempotencyKey: "hold-one",
		Target: &pb.EntityRef{Type: "cart.line", Id: "L1"}, Schema: &pb.SchemaRef{Name: "cart.line.add", Version: 1},
		Payload: []byte(`{"item":"pen"}`)}
	fail := true
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		// All three decisions, including the reply, must still be invisible.
		if len(tn.records.types["cart.line"].rows) != 0 || len(tn.records.types["stock.hold"].rows) != 0 ||
			len(tn.app("cart").(*resultCart).ledger.Changes.Records(tn.ID)) != 0 ||
			len(tn.app("stock").(*resultDepot).ledger.Changes.Records(tn.ID)) != 0 {
			t.Fatal("protocol transaction changed live records before append")
		}
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	if _, refusal := tn.Submit(member, sub, at); refusal == nil || len(entries) != 0 {
		t.Fatalf("failed append was acknowledged: %v", refusal)
	}
	fail = false
	first, refusal := tn.Submit(member, sub, at)
	if refusal != nil || len(entries) != 1 {
		t.Fatalf("protocol transaction did not commit: %v, entries=%d", refusal, len(entries))
	}
	saved, _, err := decodeAcceptedBatch(entries[0].Body)
	if err != nil || len(saved.Decisions) != 3 || len(saved.Rows) != 2 {
		t.Fatalf("request and reply were not one result: %+v, %v", saved, err)
	}
	line, errRecord := tn.RecordOf(member, "cart.line", "L1", at)
	if errRecord != nil || line.Record.(Cart).Status != "held" {
		t.Fatalf("answer did not see its privately accepted provider record: %+v, %v", line, errRecord)
	}
	second, refusal := tn.Submit(member, sub, at.Add(time.Hour))
	if refusal != nil || !proto.Equal(first, second) || len(entries) != 1 {
		t.Fatalf("retry reran a protocol decision: %v", refusal)
	}
	CheckReplay(t, tn, entries, func() *Tenant {
		restored := requestsResultTenant(t)
		restored.app("stock").(*resultDepot).forbidDecisions = true
		return restored
	})
}

func TestAcceptedProtocolRefusalKeepsRequestAndReplyOnly(t *testing.T) {
	for _, test := range []struct {
		name, item string
		keeper     bool
	}{{"business", "none", true}, {"permission", "pen", false}} {
		t.Run(test.name, func(t *testing.T) {
			tn := requestsResultTenant(t)
			member := platform.Member{ID: "buyer", Tenant: tn.ID, Roles: map[string]string{"cart": "buyer"}}
			if test.keeper {
				member.Roles["stock"] = "keeper"
			}
			var entries []Entry
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
				entries = append(entries, e)
				return e.Body, nil
			}
			at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
			sub := &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: "cart", IdempotencyKey: "ask-refused",
				Target: &pb.EntityRef{Type: "cart.line", Id: "L1"}, Schema: &pb.SchemaRef{Name: "cart.line.ask", Version: 1},
				Payload: []byte(fmt.Sprintf(`{"item":%q}`, test.item))}
			if _, err := tn.Submit(member, sub, at); err != nil || len(entries) != 1 {
				t.Fatalf("the accepted request's refused answer did not commit: %v", err)
			}
			saved, _, err := decodeAcceptedBatch(entries[0].Body)
			if err != nil || len(saved.Decisions) != 2 || len(saved.Rows) != 1 ||
				len(tn.records.types["stock.hold"].rows) != 0 {
				t.Fatalf("refused provider left a decision or record: %+v, %v", saved, err)
			}
			line, refusal := tn.RecordOf(member, "cart.line", "L1", at)
			if refusal != nil || line.Record.(Cart).Status != "refused" {
				t.Fatalf("refused answer was lost: %+v, %v", line, refusal)
			}
			CheckReplay(t, tn, entries, func() *Tenant {
				restored := requestsResultTenant(t)
				restored.app("stock").(*resultDepot).forbidDecisions = true
				return restored
			})
		})
	}
}

func TestJournalAcceptedProtocolCrashBeforeApplication(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE is not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	id := fmt.Sprintf("protocol-crash-%d", time.Now().UnixNano())
	if _, err := j.pool.Exec(ctx, `DELETE FROM journal WHERE tenant=$1`, id); err != nil {
		t.Fatal(err)
	}
	defer j.pool.Exec(ctx, `DELETE FROM journal WHERE tenant=$1`, id)
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	tn := requestsResultTenantFor(t, id)
	member := platform.Member{ID: "buyer", Tenant: id, Roles: map[string]string{"cart": "buyer", "stock": "keeper"}}
	at := time.Date(2026, 9, 28, 19, 0, 0, 0, time.UTC)
	sub := &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: "cart", IdempotencyKey: "pg-request",
		Target: &pb.EntityRef{Type: "cart.line", Id: "L1"}, Schema: &pb.SchemaRef{Name: "cart.line.add", Version: 1},
		Payload: []byte(`{"item":"pen"}`)}
	tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		saved, err := j.AppendAccepted(ctx, id, e, key, hash)
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if len(saved) == 0 {
			t.Fatal("missing committed bytes")
		}
		panic("crash after append")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("crash was not injected")
			}
		}()
		if _, err := tn.Submit(member, sub, at); err != nil {
			t.Fatalf("submit before crash: %v", err)
		}
	}()
	if len(tn.records.types["cart.line"].rows) != 0 || len(tn.records.types["stock.hold"].rows) != 0 {
		t.Fatal("crashed process applied the transaction")
	}
	other, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	entries, err := other.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("committed protocol result was lost: %v", err)
	}
	restored := requestsResultTenantFor(t, id)
	restored.app("stock").(*resultDepot).forbidDecisions = true
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Submit(member, sub, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	line, refusal := restored.RecordOf(member, "cart.line", "L1", at)
	if refusal != nil || line.Record.(Cart).Status != "held" {
		t.Fatalf("request/reply did not recover atomically: %+v, %v", line, refusal)
	}
}

func TestAcceptedModelRequestsPersistWithoutCallingProvider(t *testing.T) {
	compose := func() *Tenant {
		source := newRequestsTenant(t, false)
		tn, err := NewTenant("t", NewConsole("t"), &modelResultCart{resultCart{source.app("cart").(*cart)}})
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	member := platform.Member{ID: "buyer", Tenant: tn.ID, Roles: map[string]string{"cart": "buyer"}}
	at := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	sub := &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: "cart", IdempotencyKey: "model-requests",
		Target: &pb.EntityRef{Type: "cart.line", Id: "L1"}, Schema: &pb.SchemaRef{Name: "cart.line.ask", Version: 1},
		Payload: []byte(`{"item":"pen"}`)}
	fail := true
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if len(tn.outbound) != 0 || len(tn.records.types["cart.line"].rows) != 0 {
			t.Fatal("model request escaped before append")
		}
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	if _, err := tn.Submit(member, sub, at); err == nil || len(tn.outbound) != 0 {
		t.Fatal("failed append left a model request")
	}
	fail = false
	receipt, refusal := tn.Submit(member, sub, at)
	if refusal != nil || len(entries) != 1 || len(tn.outbound) != 2 {
		t.Fatalf("model requests did not commit with the record: %v, outbound=%d", refusal, len(tn.outbound))
	}
	for i, intent := range tn.outbound {
		var ask modelAsk
		if json.Unmarshal([]byte(intent.Body), &ask) != nil || intent.Key != fmt.Sprintf("%s#%d", receipt.GetChangeId(), i) ||
			intent.Endpoint != modelEndpoint || !intent.At.Equal(at) || ask.Model != "local/chosen" ||
			ask.Record != "cart.line/L1" || ask.Reply != "cart.line.answer" || ask.Prompt.MaxTokens != 12 {
			t.Fatalf("model intent lost its committed input: %+v, %+v", intent, ask)
		}
	}
	CheckReplay(t, tn, entries, compose)
	if _, err := tn.Submit(member, sub, at.Add(time.Hour)); err != nil || len(entries) != 1 || len(tn.outbound) != 2 {
		t.Fatalf("retry duplicated model work: %v", err)
	}
}
