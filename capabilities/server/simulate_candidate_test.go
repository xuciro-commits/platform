package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestCandidateSimulationUsesOnlyFixedData(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	seat := func(id, role string) Seat {
		return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
	}
	tn, err := NewTenant("fixed", NewConsole("fixed", seat("builder", build.Builder), seat("operator", build.User)), build.New("fixed"))
	if err != nil {
		t.Fatal(err)
	}
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	tn.Secrets = func(string) ([]byte, bool) { t.Fatal("test read production secrets"); return nil, false }
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("test sent a production effect")
		return nil, nil
	}
	tn.AIClient = func(*http.Request) (*http.Response, error) {
		t.Fatal("test called a production model")
		return nil, nil
	}
	builder, _ := tn.Member("builder")
	operator, _ := tn.Member("operator")
	keys := 0
	submit := func(m platform.Member, schema, typ, id string, payload any) {
		t.Helper()
		keys++
		raw, _ := json.Marshal(payload)
		if _, ok := payload.(build.Object); ok {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			for _, field := range []string{"id", "revision", "created", "changed", "archived", "state", "installed", "published"} {
				delete(fields, field)
			}
			raw, _ = json.Marshal(fields)
		}
		if _, kerr := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID,
			IdempotencyKey: fmt.Sprint(keys), Schema: &pb.SchemaRef{Name: schema, Version: 1},
			Target: &pb.EntityRef{Type: typ, Id: id}, Payload: raw}, at); kerr != nil {
			t.Fatalf("%s: %v", schema, kerr)
		}
	}
	object := build.Object{Name: "visit", Title: "Visit", Fields: []build.Field{{Name: "guest", Title: "Guest", Type: "text"}},
		States: []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}},
		Actions: []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done",
			Conditions: []build.Condition{{Field: "guest", Operator: "not empty", Message: "Needs a guest"}}}}}
	submit(builder, build.ObjectType+".create", build.ObjectType, "O1", object)
	submit(builder, build.SchemaPublish, build.ObjectType, "O1", map[string]any{})
	// The fixed plan deliberately reuses a real record ID: it must see an
	// empty test store, not read, overwrite or inherit that production row.
	submit(operator, "build.visit.create", "build.visit", "V1", map[string]string{"guest": "PRODUCTION"})
	submit(builder, build.PageType+".create", build.PageType, "P1", map[string]any{
		"name": "visitwork", "title": "Visit work", "object": "build.visit", "list": []string{"guest"}, "detail": []string{"guest"},
		"actions": []string{"build.visit.close"},
	})
	submit(builder, build.SchemaRelease, build.PageType, "P1", map[string]any{})
	submit(builder, build.AppType+".create", build.AppType, "A1", map[string]any{
		"name": "visits", "title": "Visits", "pages": []string{"visitwork"},
	})
	submit(builder, build.SchemaHandOver, build.AppType, "A1", map[string]any{})
	submit(builder, build.ObjectType+".edit", build.ObjectType, "O1", map[string]any{"actions": []build.Action{
		{Name: "close", Title: "Close", From: []string{"open"}, To: "done", Conditions: []build.Condition{
			{Field: "guest", Operator: "=", Value: "Ada", Message: "Only Ada in this candidate"}}},
	}})
	request := CandidateSimulationRequest{ObjectID: "O1", As: operator.ID, At: at, Steps: []SimulationStep{
		{Type: "build.visit", ID: "V1", Action: "build.visit.create", Payload: json.RawMessage(`{"guest":"Ada"}`)},
		{Type: "build.visit", ID: "V1", Action: "build.visit.close", Payload: json.RawMessage(`{}`)},
		{Type: "build.visit", ID: "V2", Action: "build.visit.create", Payload: json.RawMessage(`{"guest":"Grace"}`)},
		{Type: "build.visit", ID: "V2", Action: "build.visit.close", Payload: json.RawMessage(`{}`)},
	}}
	before, count := snapshot(tn), len(journal)
	result, kerr := tn.SimulateCandidate(builder, request)
	if kerr != nil || !result.Recovered || len(result.Steps) != 4 {
		t.Fatalf("isolated run: %+v, %v", result, kerr)
	}
	review, err := tn.PreviewRelease(builder, platform.AssetObject, "O1")
	if err != nil || review.CandidateID != result.CandidateID {
		t.Fatalf("tested a different release: %+v, %v", review, err)
	}
	if !result.Steps[1].Accepted || result.Steps[3].Accepted || !strings.Contains(result.Steps[3].Refusal, "Only Ada") {
		t.Fatalf("did not use draft rules: %+v", result.Steps)
	}
	if !strings.Contains(string(result.Steps[1].Changes[0].Record), `"state":"done"`) {
		t.Fatalf("success did not change the test record: %+v", result.Steps[1])
	}
	again, kerr := tn.SimulateCandidate(builder, request)
	if kerr != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("fixed inputs differ on repeat: %+v, %v", again, kerr)
	}
	if snapshot(tn) != before || len(journal) != count {
		t.Fatal("candidate test altered production state or journal")
	}
	// Related creation closes both object definitions, yet never copies either
	// object's production records. Required inputs still refuse atomically.
	submit(builder, build.ObjectType+".create", build.ObjectType, "O2", build.Object{Name: "followup", Title: "Follow-up", Fields: []build.Field{
		{Name: "visit", Title: "Visit", Type: "reference", Ref: "build.visit", Inverse: "followups"},
		{Name: "note", Title: "Note", Type: "text", Required: true},
	}})
	submit(builder, build.SchemaPublish, build.ObjectType, "O2", map[string]any{})
	submit(builder, build.ObjectType+".edit", build.ObjectType, "O1", map[string]any{"actions": []build.Action{
		{Name: "close", Title: "Close", From: []string{"open"}, To: "done", Inputs: []build.Input{{Name: "note", Title: "Note", Type: "text"}},
			Creates: []build.Create{{Object: "build.followup", Via: "visit", Sets: []build.Set{{Field: "note", From: "note"}}}}},
	}})
	relatedRequest := CandidateSimulationRequest{ObjectID: "O1", As: operator.ID, At: at, Steps: []SimulationStep{
		{Type: "build.visit", ID: "TEST", Action: "build.visit.create", Payload: json.RawMessage(`{"guest":"Ada"}`)},
		{Type: "build.visit", ID: "TEST", Action: "build.visit.close", Payload: json.RawMessage(`{}`)},
		{Type: "build.visit", ID: "TEST", Action: "build.visit.close", Payload: json.RawMessage(`{"note":"Call back"}`)},
	}}
	before, count = snapshot(tn), len(journal)
	related, kerr := tn.SimulateCandidate(builder, relatedRequest)
	if kerr != nil || !related.Recovered || related.Steps[1].Accepted || !related.Steps[2].Accepted || len(related.Steps[2].Changes) != 2 {
		t.Fatalf("related action test: %+v, %v", related, kerr)
	}
	if snapshot(tn) != before || len(journal) != count {
		t.Fatal("related candidate test altered production")
	}
	// Testing as a member keeps that member's field and action permissions;
	// the builder role requesting the run does not grant them extra access.
	submit(builder, build.ObjectType+".create", build.ObjectType, "O3", build.Object{Name: "privatevisit", Title: "Private visit",
		Fields: []build.Field{{Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}},
		Access: []build.Access{{Role: build.User, Read: "own", Create: true}},
		States: []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}},
		Actions: []build.Action{
			{Name: "tag", Title: "Tag", From: []string{"open"}, Sets: []build.Set{{Field: "secret", From: "=hidden sample"}}},
			{Name: "close", Title: "Close", From: []string{"open"}, To: "done", Roles: []string{build.Builder}},
		},
	})
	private, kerr := tn.SimulateCandidate(builder, CandidateSimulationRequest{ObjectID: "O3", As: operator.ID, At: at, Steps: []SimulationStep{
		{Type: "build.privatevisit", ID: "TEST", Action: "build.privatevisit.create"},
		{Type: "build.privatevisit", ID: "TEST", Action: "build.privatevisit.tag"},
		{Type: "build.privatevisit", ID: "TEST", Action: "build.privatevisit.close"},
	}})
	if kerr != nil || !private.Steps[1].Accepted || private.Steps[2].Accepted || len(private.Steps[1].Changes) != 1 {
		t.Fatalf("member permissions: %+v, %v", private, kerr)
	}
	if strings.Contains(string(private.Steps[1].Changes[0].Record), "hidden sample") {
		t.Fatal("candidate output exposed a field the selected member cannot read")
	}
	// Approval must not silently become an immediate action in a test tenant
	// without a work app, even though the live definition itself is valid.
	submit(builder, build.ObjectType+".edit", build.ObjectType, "O1", map[string]any{"states": []build.State{
		{Name: "open", Title: "Open"}, {Name: "waiting", Title: "Waiting"}, {Name: "done", Title: "Done"},
	}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done", Approval: &build.ActionApproval{
		Pending: "waiting", Levels: []build.ApproverLevel{{Title: "Review", Role: build.Builder}},
	}}}})
	if _, kerr := tn.SimulateCandidate(builder, request); kerr == nil || !strings.Contains(kerr.Message, "approval") {
		t.Fatalf("approval test did not fail closed: %v", kerr)
	}
	for _, bad := range []platform.Member{operator, {ID: "builder", Tenant: "foreign", Roles: map[string]string{build.ID: build.Builder}}} {
		if _, kerr := tn.SimulateCandidate(bad, request); kerr == nil {
			t.Fatal("unauthorised member tested a candidate")
		}
	}
	request.As = "missing"
	if _, kerr := tn.SimulateCandidate(builder, request); kerr == nil {
		t.Fatal("unknown member tested a candidate")
	}
	request.As = operator.ID
	request.Steps = make([]SimulationStep, 21)
	if _, kerr := tn.SimulateCandidate(builder, request); kerr == nil {
		t.Fatal("unbounded plan accepted")
	}
	// HTTP never accepts trailing JSON or lets a non-builder into the test.
	h := NewHost(Tokens(map[string]string{"b": "builder", "u": "operator"}), tn)
	for _, tc := range []struct {
		token, body string
		status      int
	}{
		{"u", `{}`, http.StatusForbidden},
		{"b", `{} {}`, http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/simulate/candidate", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("HTTP: %d %s", rec.Code, rec.Body.String())
		}
	}
}
