package platformserver

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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
