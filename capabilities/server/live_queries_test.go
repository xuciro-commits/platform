package platformserver

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/enterprise"
	"platformserver/apps/work"
	"platformserver/platform"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestApprovalRequestsJoinSharedLiveReads(t *testing.T) {
	seat := Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{PlatformApp: Admin, work.ID: "member"}}}
	tn, err := NewTenant("live-requests", NewConsole("live-requests", seat), work.New("live-requests"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHost(Tokens(map[string]string{"builder": "builder"}), tn).Handler())
	defer server.Close()
	paths := []string{"/v1/me", "/v1/requests", "/v1/inbox"}
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
