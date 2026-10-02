package platformserver

import (
	"fmt"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http/httptest"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestAggregateSetsCountCompleteAuthorizedMembership(t *testing.T) {
	tn := setFixture(t)
	lead, _ := tn.Member("lead")
	ana, _ := tn.Member("ana")
	now := time.Now()
	set := setExpression("union", setTerm("qty", "<", 520), setTerm("qty", ">=", 500))
	for _, tc := range []struct {
		member platform.Member
		count  int
		sum    float64
	}{{lead, 620, 191890}, {ana, 310, 95790}} {
		out, err := tn.Aggregate(tc.member, "stock.item", AggregateQuery{Set: set, Measures: []string{"count", "sum:qty"}}, now)
		if err != nil || len(out.Rows) != 1 || out.Rows[0]["count"] != tc.count || out.Rows[0]["sum:qty"] != tc.sum {
			t.Fatal(out, err)
		}
	}
	out, err := tn.Aggregate(lead, "stock.item", AggregateQuery{Set: set, Groups: []string{"line"}}, now)
	if err != nil || len(out.Rows) != 2 || out.Rows[0]["line"] != "L1" || out.Rows[0]["count"] != 310 || out.Rows[1]["count"] != 310 {
		t.Fatal(out, err)
	}
	for _, q := range []AggregateQuery{{Set: setExpression("union", &platform.RecordSetPredicate{}, setTerm("secret", "=", "x"))}, {Set: set, Groups: []string{"secret"}}, {Set: set, Groups: []string{"line", "line", "line", "line", "line"}}, {Set: set, Measures: make([]string, 9)}} {
		if _, err := tn.Aggregate(ana, "stock.item", q, now); err == nil {
			t.Fatal("invalid or hidden aggregation accepted")
		}
	}
	handler := NewHost(Tokens(map[string]string{"lead": "lead"}), tn).Handler()
	for _, body := range []string{string(platform.Raw(AggregateQuery{Set: set, Groups: []string{"line"}})), `{"set":{"op":"union","inputs":[{},{}]},"limit":1}`} {
		r := httptest.NewRequest("POST", "/v1/aggregates/stock.item/query", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer lead")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		if strings.Contains(body, "limit") {
			if response.Code == 200 {
				t.Fatal("aggregate silently accepted a record window")
			}
		} else if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}

func TestAggregateSetGroupBudgetDoesNotReturnPartialGroups(t *testing.T) {
	tn := setFixture(t)
	lead, _ := tn.Member("lead")
	caller := platform.NewCaller(runtime{tn}, platform.Member{ID: "ana", Tenant: "t-1"}, "stock", false, false)
	at := timestamppb.New(time.Now())
	for i := 620; i <= AggregateSetMaxRows; i++ {
		id := fmt.Sprintf("I%04d", i)
		change := &pb.ChangeRecord{ChangeId: "aggregate-" + id, RecordedTime: at, Revision: 1, Submission: &pb.Submission{PrincipalId: "ana", Target: &pb.EntityRef{Type: "stock.item", Id: id}, Schema: &pb.SchemaRef{Name: "stock.item.create"}}}
		if err := tn.records.put(caller, change, Item{Record: platform.Record{ID: id}, Name: id, Qty: i, Line: "L1", Owner: "ana"}); err != nil {
			t.Fatal(err)
		}
	}
	q := AggregateQuery{Set: setExpression("union", &platform.RecordSetPredicate{}, &platform.RecordSetPredicate{}), Groups: []string{"qty"}, Domain: platform.Raw([]any{[]any{"qty", "<", AggregateSetMaxRows}})}
	out, err := tn.Aggregate(lead, "stock.item", q, time.Now())
	if err != nil || len(out.Rows) != AggregateSetMaxRows {
		t.Fatal("legal full group result rejected", len(out.Rows), err)
	}
	q.Domain = nil
	out, err = tn.Aggregate(lead, "stock.item", q, time.Now())
	if err == nil || len(out.Rows) != 0 {
		t.Fatal("oversized groups returned a partial aggregate", len(out.Rows), err)
	}
}
