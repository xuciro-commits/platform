package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func setFixture(t *testing.T) *Tenant {
	t.Helper()
	tn := stockTenant(t)
	c := platform.NewCaller(runtime{tn}, platform.Member{ID: "ana", Tenant: "t-1"}, "stock", false, false)
	at := timestamppb.New(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	for i := range 620 {
		id := fmt.Sprintf("I%03d", i)
		change := &pb.ChangeRecord{ChangeId: "set-" + id, RecordedTime: at, Revision: 1, Submission: &pb.Submission{PrincipalId: "ana", Target: &pb.EntityRef{Type: "stock.item", Id: id}, Schema: &pb.SchemaRef{Name: "stock.item.create"}}}
		if err := tn.records.put(c, change, Item{Record: platform.Record{ID: id}, Name: []string{"Bolt", "Wrench", "Nut"}[i%3], Qty: i, Line: []string{"L1", "L2"}[i%2], Owner: []string{"ana", "bo"}[i%2]}); err != nil {
			t.Fatal(err)
		}
	}
	return tn
}

func setTerm(field, op string, value any) *platform.RecordSetPredicate {
	return &platform.RecordSetPredicate{Domain: platform.Raw([]any{[]any{field, op, value}})}
}
func setExpression(op string, a, b *platform.RecordSetPredicate) *platform.QuerySet {
	return &platform.QuerySet{Op: op, Inputs: []*platform.RecordSetPredicate{a, b}}
}

func TestCompleteRecordSetsUseOneAuthorizedScopeBeforePaging(t *testing.T) {
	tn := setFixture(t)
	lead, _ := tn.Member("lead")
	ana, _ := tn.Member("ana")
	now := time.Now()
	a, b := setTerm("qty", "<", 520), setTerm("qty", ">=", 500)
	for _, tc := range []struct {
		op     string
		total  int
		offset int
		ids    []string
	}{
		{"union", 620, 519, []string{"I519", "I520"}},
		{"intersect", 20, 18, []string{"I518", "I519"}},
		{"subtract", 500, 498, []string{"I498", "I499"}},
	} {
		page, err := tn.Records(lead, "stock.item", platform.Query{Set: setExpression(tc.op, a, b), Sort: []string{"id"}, Limit: 2, Offset: tc.offset}, now)
		var ids []string
		for _, row := range page.Records {
			ids = append(ids, row.(Item).ID)
		}
		if err != nil || page.Total != tc.total || !reflect.DeepEqual(ids, tc.ids) {
			t.Fatalf("%s: %+v %v %v", tc.op, page, ids, err)
		}
	}
	page, err := tn.Records(ana, "stock.item", platform.Query{Set: setExpression("union", a, b), Limit: 1}, now)
	if err != nil || page.Total != 310 || len(page.Records) != 1 {
		t.Fatal("set widened owner scope", page.Total, err)
	}
	// Validation covers the right branch even if the left branch is universal.
	if _, err := tn.Records(ana, "stock.item", platform.Query{Set: setExpression("union", &platform.RecordSetPredicate{}, setTerm("secret", "=", "private"))}, now); err == nil {
		t.Fatal("hidden branch field accepted")
	}
	page, err = tn.Records(lead, "stock.item", platform.Query{Set: setExpression("union", &platform.RecordSetPredicate{Search: "Bolt"}, &platform.RecordSetPredicate{Search: "Wrench"}), Limit: 1}, now)
	if err != nil || page.Total != 414 {
		t.Fatal("source search was lost", page.Total, err)
	}
	// A nested difference and outer condition share the original exact comparator.
	page, err = tn.Records(lead, "stock.item", platform.Query{Domain: platform.Raw([]any{[]any{"qty", "=", platform.DecimalValue{Kind: "decimal", Value: "519"}}}), Set: setExpression("subtract", &platform.RecordSetPredicate{Set: setExpression("union", a, b)}, setTerm("qty", ">=", 520))}, now)
	if err != nil || page.Total != 1 || page.Records[0].(Item).ID != "I519" {
		t.Fatal("nested predicate/outer decimal condition failed", page.Total, err)
	}
	// Ordinary reads retain their existing scope, sort, count and window.
	plain, err := tn.Records(lead, "stock.item", platform.Query{Domain: a.Domain, Sort: []string{"id"}, Offset: 519, Limit: 1}, now)
	if err != nil || plain.Total != 520 || plain.Records[0].(Item).ID != "I519" {
		t.Fatal(plain.Total, err)
	}
}

func TestRecordSetHTTPRejectsMalformedBranchesWithoutFallingBack(t *testing.T) {
	tn := setFixture(t)
	handler := NewHost(Tokens(map[string]string{"lead": "lead", "ana": "ana"}), tn).Handler()
	request := func(who, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/records/stock.item/query", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+who)
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	valid := platform.Query{Set: setExpression("union", setTerm("qty", "<", 520), setTerm("qty", ">=", 500)), Limit: 2, Offset: 519, Sort: []string{"id"}}
	out := request("lead", string(platform.Raw(valid)))
	var page struct {
		Total   int
		Records []Item
	}
	json.Unmarshal(out.Body.Bytes(), &page)
	if out.Code != 200 || page.Total != 620 || len(page.Records) != 2 || page.Records[0].ID != "I519" {
		t.Fatal(out.Code, out.Body.String())
	}
	for _, body := range []string{`null`, `{} {}`, `{"set":{"op":"union","inputs":[null,{}]}}`, `{"set":{"op":"union","inputs":[{},{}]},"offset":-1}`, `{"set":{"op":"union","inputs":[{"limit":1},{}]}}`, `{"set":{"op":"bogus","inputs":[{},{}]}}`, `{"set":{"op":"union","inputs":[{}]}}`, `{"set":{"op":"union","inputs":[{}, {"domain":[["secret","=","hidden"]]}]}}`, strings.Repeat(" ", platform.QuerySetMaxBytes) + `{}`} {
		if out := request("ana", body); out.Code == 200 {
			t.Fatal("malformed set accepted", body[:min(len(body), 100)])
		}
	}
}
