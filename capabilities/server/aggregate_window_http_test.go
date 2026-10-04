package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestWindowHTTPOriginalMemberFieldsPredicatesAndNoModeFallback(t *testing.T) {
	tn, err := NewTenant("windows", NewConsole("windows", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("windows"))
	if err != nil {
		t.Fatal(err)
	}
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	key := 0
	submit := func(m platform.Member, typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, issue := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if issue != nil {
			t.Fatal(typ, id, verb, issue.Message)
		}
	}
	fields := []build.Field{{Name: "name", Title: "Name", Type: "text", Search: true}, {Name: "at", Title: "Event time", Type: "datetime"}, {Name: "value", Title: "Signal", Type: "decimal"}, {Name: "secret", Title: "Private signal", Type: "decimal", Read: []string{build.Builder}}, {Name: "privateat", Title: "Private time", Type: "datetime", Read: []string{build.Builder}}}
	submit(builder, build.ObjectType, "object", "create", map[string]any{"name": "observations", "title": "Observations", "fields": fields, "access": []build.Access{{Role: build.User, Read: "own", Create: true}}})
	submit(builder, build.ObjectType, "object", "publish", map[string]any{})
	for _, r := range []struct {
		member          platform.Member
		id, name, stamp string
		value           float64
	}{{reader, "A", "keep", "2026-10-03T01:00:00+01:00", 0.1}, {builder, "B", "keep", "2026-10-03T00:00:01Z", 0.2}, {reader, "C", "drop", "2026-10-03T00:00:02Z", 0.3}, {reader, "D", "keep", "", 900}} {
		payload := map[string]any{"name": r.name, "value": r.value}
		if r.stamp != "" {
			payload["at"] = r.stamp
		}
		submit(r.member, "build.observations", r.id, "create", payload)
	}
	handler := NewHost(Tokens(map[string]string{"reader": "reader", "builder": "builder"}), tn).Handler()
	post := func(token string, q any) (int, Aggregate) {
		t.Helper()
		request := httptest.NewRequest("POST", "/v1/aggregates/build.observations/query", strings.NewReader(string(platform.Raw(q))))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var result Aggregate
		if response.Code == 200 && json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatal(response.Body.String())
		}
		return response.Code, result
	}
	base := AggregateQuery{Window: &AggregateWindowQuery{TimeField: "at", Field: "value", Rows: 2, Threshold: "0.1"}}
	for _, expected := range []struct {
		member       string
		total, timed int
		first, last  string
		mean         float64
	}{{"reader", 3, 2, "C", "A", 0.2}, {"builder", 4, 3, "C", "B", 0.25}} {
		status, result := post(expected.member, base)
		if status != 200 || result.Window == nil {
			t.Fatal(status, result)
		}
		w := result.Window
		if w.Total != expected.total || w.Timed != expected.timed || w.Count != 2 || w.First.ID != expected.first || w.Last.ID != expected.last || *w.Mean != expected.mean {
			t.Fatal(expected, w)
		}
	}
	predicates := base
	predicates.Domain = platform.Raw([]any{[]any{"name", "=", "keep"}})
	predicates.Search = "keep"
	predicates.Set = setExpression("union", setTerm("value", "<=", 1), setTerm("value", ">=", 900))
	status, result := post("reader", predicates)
	if status != 200 || result.Window.Total != 2 || result.Window.Count != 1 || result.Window.MissingTime != 1 || result.Window.First.ID != "A" || result.Window.Above != 0 {
		t.Fatal(status, result)
	}
	for _, body := range []any{map[string]any{"window": map[string]any{"timeField": "at", "field": "secret", "rows": 1000, "threshold": "0"}}, map[string]any{"window": map[string]any{"timeField": "privateat", "field": "value", "rows": 1000, "threshold": "0"}}, map[string]any{"window": base.Window, "domain": []any{[]any{"secret", "=", 1}}}, map[string]any{"window": base.Window, "set": setExpression("union", &platform.RecordSetPredicate{}, setTerm("secret", "=", 1))}, map[string]any{"window": base.Window, "limit": 1}, map[string]any{"window": base.Window, "groups": []string{"name"}}, map[string]any{"window": map[string]any{"timeField": "at", "field": "value", "rows": 100001, "threshold": "0"}}, map[string]any{"window": map[string]any{"timeField": "at", "field": "value", "rows": 1000, "threshold": "1e999999"}}} {
		status, result := post("reader", body)
		if status != 400 || result.Window != nil {
			t.Fatal("invalid read was not refused", status, result)
		}
	}
	request := httptest.NewRequest("GET", "/v1/aggregates/build.observations?measure=count", nil)
	request.Header.Set("Authorization", "Bearer reader")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var complete Aggregate
	_ = json.Unmarshal(response.Body.Bytes(), &complete)
	if complete.Window != nil || len(complete.Rows) != 1 || complete.Rows[0]["count"] != float64(3) {
		t.Fatal("ordinary complete aggregate changed", complete)
	}
}
