package platformserver

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/enterprise"
	"platformserver/apps/work"
	"platformserver/platform"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
)

func TestEnterpriseLegacyViewRecovery(t *testing.T) {
	compose := func() *Tenant {
		return composeTenant(t, "legacy-views", []Seat{seatOf("admin", "enterprise:admin")}, enterprise.New("legacy-views", platform.OrgSeed{}))
	}
	tn := compose()
	m := memberOf(t, tn, "admin")
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	grids := map[string]string{"Pr-Sr": "organization", "Pr-Cn": "organization", "Rs-Sr": "organization", "St-Tx": "function", "St-Sr": "function", "Sv-Tx": "output", "Op-Pr": "control", "Pj-Rm": "control"}
	var entries []Entry
	for grid := range grids {
		sub := submission(tn, m, enterprise.ID, enterprise.SchemaViewSave, enterprise.ViewType, grid,
			map[string]any{"name": grid, "grid": grid, "elements": []string{}, "layout": map[string][2]float64{"historical": {123, 456}}, "asOf": "2026-10-07"})
		raw, err := protojson.Marshal(sub)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, Entry{App: enterprise.ID, Kind: "submission", Principal: platform.Raw(m), Body: raw, At: at})
	}
	before := string(platform.Raw(entries))
	if err := tn.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if string(platform.Raw(entries)) != before {
		t.Fatal("legacy recovery rewrote journal bytes")
	}
	value, err := tn.Read(m, enterprise.ReadModel)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range value.(enterprise.Model).Views {
		if view.Viewpoint != grids[view.ID] || view.Layout["historical"] != [2]float64{123, 456} || view.AsOf != "2026-10-07" {
			t.Fatalf("legacy view identity or drawing changed: %+v", view)
		}
	}
	if why := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaViewSave, enterprise.ViewType, "live-grid", map[string]any{"name": "old write", "grid": "Pr-Sr"}, at); why == "ok" {
		t.Fatal("the retired grid field returned to the live contract")
	}
	// The snapshot decoder has the same bounded mapping as historical input.
	var saved enterprise.View
	if err := json.Unmarshal([]byte(`{"id":"original","name":"Plan","grid":"Pj-Rm","elements":[],"layout":{"milestone":[12,34]}}`), &saved); err != nil || saved.Viewpoint != "control" || saved.ID != "original" || saved.Layout["milestone"] != [2]float64{12, 34} {
		t.Fatalf("historical snapshot did not retain its drawing: %+v %v", saved, err)
	}
	if err := json.Unmarshal([]byte(`{"id":"modern","viewpoint":"data","grid":"Pr-Sr","elements":[]}`), &saved); err != nil || saved.Viewpoint != "data" {
		t.Fatal("legacy metadata replaced a current viewpoint")
	}
}

func TestApprovalRequestsJoinSharedLiveReads(t *testing.T) {
	seat := Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{PlatformApp: Admin, work.ID: "member", build.ID: build.Builder}}}
	tn, err := NewTenant("live-requests", NewConsole("live-requests", seat), work.New("live-requests"), build.New("live-requests"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHost(Tokens(map[string]string{"builder": "builder"}), tn).Handler())
	defer server.Close()
	paths := []string{"/v1/me", "/v1/requests", "/v1/inbox", "/v1/releases/active", "/v1/releases/candidates"}
	watch, _ := json.Marshal(paths)
	req, _ := http.NewRequest("GET", server.URL+"/v1/changes?"+url.Values{"watch": {string(watch)}}.Encode(), nil)
	req.Header.Set("Authorization", "Bearer builder")
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("shared approval reads: HTTP %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame LiveQueryFrame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		if len(frame.Results) != len(paths) {
			t.Fatalf("missing snapshots: %+v", frame)
		}
		for _, result := range frame.Results {
			if result.Status != http.StatusOK {
				t.Fatalf("%s: HTTP %d", result.Path, result.Status)
			}
		}
		break
	}
}

func TestEnterpriseHTTPAndLiveReads(t *testing.T) {
	seat := Seat{Subjects: []string{"user:admin@example.test"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin, enterprise.ID: enterprise.Admin}}}
	tn, err := NewTenant("enterprise-http", NewConsole("enterprise-http", seat), enterprise.New("enterprise-http", platform.OrgSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHost(Tokens(map[string]string{"token": "user:admin@example.test"}), tn).Handler())
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	paths := []string{"/v1/enterprise", "/v1/enterprise-metamodel", "/v1/enterprise-published", "/v1/enterprise-patterns"}
	get := func(path string) *http.Response {
		t.Helper()
		request, _ := http.NewRequest("GET", server.URL+path, nil)
		request.Header.Set("Authorization", "Bearer token")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			t.Fatalf("%s: HTTP %d", path, response.StatusCode)
		}
		return response
	}
	for _, path := range paths {
		get(path).Body.Close()
	}
	watch, _ := json.Marshal(paths)
	response := get("/v1/changes?" + url.Values{"watch": {string(watch)}}.Encode())
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	frame := func() LiveQueryFrame {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var out LiveQueryFrame
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &out); err != nil {
					t.Fatal(err)
				}
				for _, result := range out.Results {
					if result.Status != 200 {
						t.Fatalf("live %s: HTTP %d", result.Path, result.Status)
					}
				}
				return out
			}
		}
	}
	if initial := frame(); len(initial.Results) != len(paths) {
		t.Fatalf("missing initial enterprise snapshots: %+v", initial)
	}
	admin, _ := tn.Member("admin")
	if _, err := tn.Submit(admin, &pb.Submission{TenantId: tn.ID, PrincipalId: admin.ID, Authority: enterprise.ID, IdempotencyKey: "add-unit",
		Target: &pb.EntityRef{Type: enterprise.ElementType, Id: "company"}, Schema: &pb.SchemaRef{Name: enterprise.SchemaElementAdd, Version: 1},
		Payload: []byte(`{"stereotype":"ActualOrganization","name":"Company"}`)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	updated := frame()
	found := false
	for _, result := range updated.Results {
		if result.Path == "/v1/enterprise" && strings.Contains(string(result.Body), `"id":"company"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("model change did not reach the original enterprise subscription")
	}
}

func TestLiveQueriesFilterDependenciesAndInvalidateDeniedReads(t *testing.T) {
	tenant := &Tenant{records: newRecordStore()}
	mux := http.NewServeMux()
	var reads atomic.Int32
	var denied atomic.Bool
	mux.HandleFunc("GET /v1/records/sample.note", func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if denied.Load() {
			http.Error(w, "denied", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"value": int(reads.Load())})
	})
	mux.HandleFunc("GET /v1/changes", func(w http.ResponseWriter, r *http.Request) { followLiveQueries(w, r, tenant, mux) })
	server := httptest.NewServer(mux)
	defer server.Close()
	watch, _ := json.Marshal([]string{"/v1/records/sample.note"})
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(server.URL + "/v1/changes?" + url.Values{"watch": {string(watch)}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	frame := func() LiveQueryFrame {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var f LiveQueryFrame
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
					t.Fatal(err)
				}
				return f
			}
		}
	}
	if f := frame(); len(f.Results) != 1 || f.Results[0].Status != 200 {
		t.Fatalf("initial=%+v", f)
	}
	tenant.records.mu.Lock()
	tenant.records.liveTouched("other.note")
	tenant.records.mu.Unlock()
	tenant.changedOwner("other")
	time.Sleep(200 * time.Millisecond)
	if reads.Load() != 1 {
		t.Fatal("unrelated type triggered a business read")
	}
	tenant.operationsChanged()
	time.Sleep(200 * time.Millisecond)
	if reads.Load() != 1 {
		t.Fatal("queue progress triggered a business read")
	}
	tenant.records.mu.Lock()
	tenant.records.liveTouched("sample.note")
	tenant.records.mu.Unlock()
	tenant.changedOwner("sample")
	if f := frame(); f.Results[0].Status != 200 || reads.Load() != 2 {
		t.Fatalf("relevant=%+v, reads=%d", f, reads.Load())
	}
	denied.Store(true)
	tenant.changedOwner("platform")
	if f := frame(); f.Results[0].Status != 403 || strings.Contains(string(f.Results[0].Body), "value") {
		t.Fatalf("denial kept old values: %+v", f)
	}
}

func TestLiveRegistrationRejectsWritesAndStreams(t *testing.T) {
	tenant := &Tenant{records: newRecordStore()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/records/sample.note/query", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"records":[]}`)) })
	mux.HandleFunc("POST /v1/releases/candidates/query", func(w http.ResponseWriter, r *http.Request) {
		t.Error("a write route was executed")
		w.Write([]byte(`{}`))
	})
	for _, query := range []string{"/v1/releases/candidates/query\n{}", "/v1/submissions\n{}", "/v1/changes", "https://other.invalid/v1/me", "/v1/records/../me", "/v1/records/sample.note/query\nnot-json"} {
		raw, _ := json.Marshal([]string{query})
		request := httptest.NewRequest("GET", "/v1/changes?"+url.Values{"watch": {string(raw)}}.Encode(), nil)
		rec := httptest.NewRecorder()
		followLiveQueries(rec, request, tenant, mux)
		if rec.Code != 400 {
			t.Fatalf("%q accepted: %d", query, rec.Code)
		}
	}
}

func TestLiveQueriesUseOriginalMemberAuthorizationAfterRevocation(t *testing.T) {
	tn, member, _ := upgradeTenant(t, "live-auth")
	server := httptest.NewServer(NewHost(Tokens(map[string]string{"token": "user:dana@example.test"}), tn).Handler())
	defer server.Close()
	watch, _ := json.Marshal([]string{"/v1/records/build.object"})
	request, _ := http.NewRequest("GET", server.URL+"/v1/changes?"+url.Values{"watch": {string(watch)}}.Encode(), nil)
	request.Header.Set("Authorization", "Bearer token")
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	read := func() LiveQueryResult {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var frame LiveQueryFrame
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
					t.Fatal(err)
				}
				return frame.Results[0]
			}
		}
	}
	if result := read(); result.Status != 200 {
		t.Fatalf("authorized initial read: %+v", result)
	}
	payload, _ := json.Marshal(map[string]string{"app": "build"})
	if _, err := tn.Submit(member("dana"), &pb.Submission{TenantId: tn.ID, PrincipalId: "dana", Authority: PlatformApp, IdempotencyKey: "live-revoke", Target: &pb.EntityRef{Type: MemberType, Id: "dana"}, Schema: &pb.SchemaRef{Name: "platform.member.revoke", Version: 1}, Payload: payload}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if result := read(); result.Status == 200 && strings.Contains(string(result.Body), `"records":[{`) {
		t.Fatalf("revoked member kept protected records: %+v", result)
	} else if result.Status == 200 {
		t.Fatalf("revocation did not invalidate the protected query: %+v", result)
	}
}

func TestEnterpriseAppliedPatternReplays(t *testing.T) {
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"user:admin@example.test"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin, enterprise.ID: enterprise.Admin}}}
		tn, err := NewTenant("example-replay", NewConsole("example-replay", seat), enterprise.New("example-replay", platform.OrgSeed{Units: []platform.Unit{{ID: "existing", Name: "Existing enterprise"}}}))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose()
	var entries []Entry
	live.Record = func(entry Entry) { entries = append(entries, entry) }
	admin, _ := live.Member("admin")
	if _, err := live.Submit(admin, &pb.Submission{TenantId: live.ID, PrincipalId: admin.ID, Authority: enterprise.ID, IdempotencyKey: "example",
		Target: &pb.EntityRef{Type: enterprise.ModelType, Id: "model"}, Schema: &pb.SchemaRef{Name: enterprise.SchemaPatternApply, Version: 1},
		Payload: []byte(`{"pattern":"hotel","name":"Example hotel","under":"existing","params":{"floors":2}}`)}, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one durable input, got %d", len(entries))
	}
	CheckReplay(t, live, entries, compose)
}

// View identity and relationship replacement must preserve unrelated state,
// survive refusals and replay identically through the real submission pipeline.
func TestEnterpriseViewIdentityAndAtomicRelationships(t *testing.T) {
	compose := func() *Tenant {
		return composeTenant(t, "model-decisions", []Seat{seatOf("admin", "enterprise:admin", "build:builder"), seatOf("viewer", "enterprise:admin")},
			enterprise.New("model-decisions", platform.OrgSeed{Structures: []platform.Structure{{ID: "management", Name: "Management", Kind: "management"}}}), build.New("model-decisions"))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b", "c"} {
		decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, id, map[string]any{"name": id, "stereotype": enterprise.Organization}, at)
	}
	model := func(who string) enterprise.Model {
		t.Helper()
		value, err := tn.Read(memberOf(t, tn, who), enterprise.ReadModel)
		if err != nil {
			t.Fatal(err)
		}
		return value.(enterprise.Model)
	}
	for _, id := range []string{"first", "second"} {
		decide(t, tn, "admin", enterprise.ID, enterprise.SchemaViewSave, enterprise.ViewType, id,
			map[string]any{"name": id, "viewpoint": "organization", "kind": "management", "asOf": "2026-10-08", "elements": []string{"a", "b"}}, at)
	}
	second := model("admin").Views[1]
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaViewSave, enterprise.ViewType, "first",
		map[string]any{"name": "Renamed", "viewpoint": "data", "elements": []string{}, "asOf": "2026-10-07"}, at)
	m := model("admin")
	if m.Views[0].ID != "first" || m.Views[0].Kind != "management" || len(m.Views[0].Elements) != 0 || !reflect.DeepEqual(m.Views[1], second) {
		t.Fatalf("view state crossed identities: %+v", m.Views)
	}
	if got := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaViewSave, enterprise.ViewType, "first", map[string]any{"name": "bad", "viewpoint": "made-up"}, at); got == "ok" {
		t.Fatal("unknown viewpoint accepted")
	}
	if got := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, "bad-project",
		map[string]any{"name": "Bad project", "stereotype": enterprise.Project, "properties": map[string]any{"startDate": "2026-10-10T00:00:00Z", "endDate": "2026-10-08T00:00:00Z"}}, at); got == "ok" {
		t.Fatal("a backwards project interval entered the model")
	}
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaRelationshipAdd, enterprise.RelationshipType, "old", map[string]any{"stereotype": enterprise.Placement, "source": "a", "target": "b", "kind": "management"}, at)
	before := model("admin")
	if got := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaRelationshipChange, enterprise.RelationshipType, "old",
		map[string]any{"replacement": "bad", "stereotype": enterprise.Placement, "source": "a", "target": "missing", "kind": "management"}, at); got == "ok" {
		t.Fatal("invalid replacement accepted")
	}
	if !reflect.DeepEqual(before.Relationships, model("admin").Relationships) {
		t.Fatal("refused replacement ended the old relationship")
	}
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaRelationshipChange, enterprise.RelationshipType, "old",
		map[string]any{"replacement": "new", "stereotype": enterprise.Placement, "source": "a", "target": "c", "kind": "management"}, at)
	m = model("admin")
	if len(m.Relationships) != 2 || m.Relationships[0].Until != "2026-10-08" || m.Relationships[1].Target != "c" {
		t.Fatalf("same-day change: %+v", m.Relationships)
	}
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaViewDelete, enterprise.ViewType, "first", map[string]any{"id": "first"}, at)
	if len(model("admin").Views) != 1 || len(model("admin").Elements) != 3 {
		t.Fatal("deleting a drawing changed the model")
	}
	decide(t, tn, "admin", build.ID, "build.object.create", build.ObjectType, "OBJ", map[string]any{"name": "privateobject", "title": "Private object"}, at)
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaViewSave, enterprise.ViewType, "second",
		map[string]any{"name": "Pinned", "viewpoint": "organization", "elements": []string{"a"}, "pins": []map[string]any{{"ref": "record:build.object/OBJ", "anchor": "a", "label": "Cached private text", "at": []int{10, 20}}}}, at)
	if len(model("viewer").Views[0].Pins) != 0 {
		t.Fatal("private record pin leaked into shared model")
	}
	if pins := model("admin").Views[0].Pins; len(pins) != 1 || pins[0].Label == "Cached private text" {
		t.Fatal("pin label bypassed the current scoped record")
	}
	if got := refuse(t, tn, "viewer", enterprise.ID, enterprise.SchemaViewSave, enterprise.ViewType, "second", map[string]any{"name": "overwrite", "viewpoint": "organization", "elements": []string{"a"}}, at); got == "ok" {
		t.Fatal("a partially visible view silently lost another member's pins")
	}
	CheckReplay(t, tn, entries, compose)
}

func TestGovernanceHTTPAndLiveReads(t *testing.T) {
	tn, _, _ := upgradeTenant(t, "governance-live")
	host := NewHost(Tokens(map[string]string{"dana": "user:dana@example.test", "aud": "user:aud@example.test", "mo": "user:mo@example.test"}), tn)
	server := httptest.NewServer(host.Handler())
	defer server.Close()
	paths := []string{"/v1/access", "/v1/permissions", "/v1/projects", "/v1/authz/explain?member=mo&permission=build.object.edit", "/v1/tokens", "/v1/sessions"}
	watch, _ := json.Marshal(paths)
	client := &http.Client{Timeout: 3 * time.Second}
	for _, token := range []string{"dana", "aud", "mo"} {
		t.Run(token, func(t *testing.T) {
			req, _ := http.NewRequest("GET", server.URL+"/v1/changes?"+url.Values{"watch": {string(watch)}}.Encode(), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("governance subscription rejected: HTTP %d", response.StatusCode)
			}
			reader := bufio.NewReader(response.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				var frame LiveQueryFrame
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
					t.Fatal(err)
				}
				if len(frame.Results) != len(paths) {
					t.Fatal("missing governance reads")
				}
				want := 200
				if token == "mo" {
					want = 403
				}
				for _, result := range frame.Results {
					status := want
					if result.Path == "/v1/tokens" || result.Path == "/v1/sessions" {
						status = 200
					}
					if result.Status != status {
						t.Fatalf("%s: status %d, want %d", result.Path, result.Status, status)
					}
				}
				break
			}
		})
	}
}
