package host

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func newObservationTenant(tb testing.TB, id string) *invariantTenant {
	tb.Helper()
	live := newInvariantTenant(tb, id,
		platform.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{}}},
		platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}},
		platform.Seat{Subjects: []string{"auditor"}, Member: platform.Member{ID: "auditor", Roles: map[string]string{controlowner.PlatformApp: platform.Auditor}}},
	)
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingLogRequests, "0")
	return live
}

func observationHost(tenants ...*Tenant) (*Host, http.Handler) {
	tokens := map[string]string{}
	for _, tenant := range tenants {
		tokens["builder-"+tenant.ID] = "builder"
		tokens["other-"+tenant.ID] = "other"
		tokens["admin-"+tenant.ID] = "admin"
		tokens["auditor-"+tenant.ID] = "auditor"
	}
	host := NewHost(Tokens(tokens), tenants...)
	host.Now = func() time.Time { return invariantClock }
	return host, host.Handler()
}

func observationToken(tenant, role string) string { return role + "-" + tenant }

func observationRequest(tb testing.TB, handler http.Handler, tenant, token, path string, query url.Values) *httptest.ResponseRecorder {
	tb.Helper()
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(TenantHeader, tenant)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeObservationPage(tb testing.TB, response *httptest.ResponseRecorder) platform.ObservationPage {
	tb.Helper()
	if response.Code != http.StatusOK {
		tb.Fatalf("logs: HTTP %d: %s", response.Code, response.Body)
	}
	var page platform.ObservationPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		tb.Fatal(err)
	}
	return page
}

func addObservation(tb testing.TB, live *invariantTenant, event platform.Observation) {
	tb.Helper()
	event.Tenant = live.ID
	if event.ID == "" {
		event.ID = fmt.Sprintf("observation-%d", invariantKeys.Add(1))
	}
	if err := live.Log().SaveObservations([]platform.Observation{event}); err != nil {
		tb.Fatal(err)
	}
}

func storedObservations(tb testing.TB, live *invariantTenant, query platform.ObservationQuery) platform.ObservationPage {
	tb.Helper()
	live.FlushObservations()
	query.Tenant = live.ID
	page, err := live.Log().QueryObservations(query)
	if err != nil {
		tb.Fatal(err)
	}
	return page
}

func TestInvariantObservationTenantIsolation(t *testing.T) {
	first := newObservationTenant(t, "observation-first")
	second := newObservationTenant(t, "observation-second")
	at := invariantClock.Add(-10 * time.Minute)
	addObservation(t, first, platform.Observation{ID: "first-only", At: at, Level: "info", Source: "app", Member: "builder", Message: "first tenant event"})
	addObservation(t, second, platform.Observation{ID: "second-only", At: at, Level: "info", Source: "app", Member: "builder", Message: "second tenant event"})
	_, handler := observationHost(first.Tenant, second.Tenant)
	firstPage := decodeObservationPage(t, observationRequest(t, handler, first.ID, observationToken(first.ID, "builder"), "/v1/monitoring/logs", nil))
	if len(firstPage.Entries) != 1 || firstPage.Entries[0].ID != "first-only" || firstPage.Entries[0].Tenant != first.ID {
		t.Fatalf("the first tenant read crossed its boundary: %+v", firstPage.Entries)
	}
	secondPage := decodeObservationPage(t, observationRequest(t, handler, second.ID, observationToken(second.ID, "builder"), "/v1/monitoring/logs", nil))
	if len(secondPage.Entries) != 1 || secondPage.Entries[0].ID != "second-only" || secondPage.Entries[0].Tenant != second.ID {
		t.Fatalf("the second tenant read crossed its boundary: %+v", secondPage.Entries)
	}
}

func TestInvariantObservationMemberScope(t *testing.T) {
	live := newObservationTenant(t, "observation-members")
	at := invariantClock.Add(-5 * time.Minute)
	addObservation(t, live, platform.Observation{ID: "builder-event", At: at, Level: "info", Source: "app", Member: "builder", Message: "builder event"})
	addObservation(t, live, platform.Observation{ID: "other-event", At: at.Add(time.Second), Level: "info", Source: "app", Member: "other", Message: "other event"})
	_, handler := observationHost(live.Tenant)
	builderPage := decodeObservationPage(t, observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/monitoring/logs", url.Values{"member": {"other"}}))
	if len(builderPage.Entries) != 1 || builderPage.Entries[0].Member != "builder" {
		t.Fatalf("an ordinary member read another member's logs: %+v", builderPage.Entries)
	}
	if !builderPage.Coverage.Filtered {
		t.Fatal("a permission-filtered log page did not declare its filter")
	}
	adminPage := decodeObservationPage(t, observationRequest(t, handler, live.ID, observationToken(live.ID, "admin"), "/v1/monitoring/logs", nil))
	if len(adminPage.Entries) != 2 {
		t.Fatalf("an administrator did not read the tenant range: %+v", adminPage.Entries)
	}
	auditorPage := decodeObservationPage(t, observationRequest(t, handler, live.ID, observationToken(live.ID, "auditor"), "/v1/monitoring/logs", nil))
	if len(auditorPage.Entries) != 2 {
		t.Fatalf("an auditor did not read the tenant range: %+v", auditorPage.Entries)
	}
}

func TestInvariantObservationReadKeepsAcceptedState(t *testing.T) {
	live := newObservationTenant(t, "observation-state")
	addObservation(t, live, platform.Observation{ID: "state-event", At: invariantClock.Add(-time.Minute), Level: "info", Source: "app", Member: "builder", Message: "state event"})
	entries := append([]Entry{}, live.entries...)
	records := live.recordImage(t)
	_, handler := observationHost(live.Tenant)
	decodeObservationPage(t, observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/monitoring/logs", nil))
	if !reflect.DeepEqual(entries, live.entries) {
		t.Fatal("reading operational logs changed accepted journal entries")
	}
	if changed := differingTypes(records, live.recordImage(t)); len(changed) != 0 {
		t.Fatalf("reading operational logs changed accepted records: %v", changed)
	}
}

func TestInvariantObservationRetentionAndCoverage(t *testing.T) {
	live := newObservationTenant(t, "observation-retention")
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingLogRetention, "1")
	keptAt := invariantClock.Add(-2 * time.Hour)
	addObservation(t, live, platform.Observation{ID: "beyond-retention", At: invariantClock.Add(-48 * time.Hour), Level: "info", Source: "app", Message: "expired"})
	addObservation(t, live, platform.Observation{ID: "retained", At: keptAt, Level: "info", Source: "app", Message: "retained"})
	live.PurgeObservations(invariantClock)
	page := storedObservations(t, live, platform.ObservationQuery{From: invariantClock.Add(-96 * time.Hour), To: invariantClock.Add(time.Hour), Limit: 100})
	if len(page.Entries) != 1 || page.Entries[0].ID != "retained" {
		t.Fatalf("retention did not remove only the expired event: %+v", page.Entries)
	}
	if !page.Coverage.RetainedFrom.Equal(keptAt) {
		t.Fatalf("retained-from is %s, want %s", page.Coverage.RetainedFrom, keptAt)
	}
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingLogRetention, "0")
	live.recordObservation(platform.Observation{ID: "suppressed", At: invariantClock, Level: "info", Source: "app", Message: "suppressed"})
	live.PurgeObservations(invariantClock.Add(2 * time.Hour))
	page = storedObservations(t, live, platform.ObservationQuery{From: invariantClock.Add(-96 * time.Hour), To: invariantClock.Add(3 * time.Hour), Limit: 100})
	if len(page.Entries) != 1 || page.Entries[0].ID != "retained" {
		t.Fatalf("zero retention did not keep existing events and suppress new ones: %+v", page.Entries)
	}
}

func TestInvariantObservationReadAudit(t *testing.T) {
	live := newObservationTenant(t, "observation-audit")
	addObservation(t, live, platform.Observation{ID: "audit-event", At: invariantClock.Add(-time.Minute), Level: "info", Source: "app", Message: "audit event"})
	before := len(live.Audit())
	_, handler := observationHost(live.Tenant)
	response := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/monitoring/logs", url.Values{"level": {"warn"}, "q": {"private-search"}})
	decodeObservationPage(t, response)
	audit := live.Audit()
	if len(audit) != before+1 {
		t.Fatalf("log read recorded %d audit entries, want 1", len(audit)-before)
	}
	entry := audit[len(audit)-1]
	if entry.Action != "platform.log.read" || entry.Member != "builder" || entry.App != controlowner.PlatformApp {
		t.Fatalf("log read audit identity: %+v", entry)
	}
	if !strings.Contains(entry.Target, "level=warn") || strings.Contains(entry.Target, "private-search") {
		t.Fatalf("log read audit target is not a filter summary: %q", entry.Target)
	}
}

func TestInvariantAuditDoesNotDependOnMemoryRing(t *testing.T) {
	live := newObservationTenant(t, "audit-persistent")
	for index := 0; index < 1005; index++ {
		live.audit.Remember(platform.AuditEntry{
			ID:     fmt.Sprintf("audit-%04d", index),
			At:     invariantClock.Add(time.Duration(index) * time.Second),
			Member: "builder", App: build.ID, Action: "build.object.create", Target: fmt.Sprintf("build.object/OBJ-%04d", index),
		})
	}
	if got := len(live.Audit()); got != 1000 {
		t.Fatalf("hot audit ring kept %d entries, want its bounded 1000", got)
	}
	page, err := live.Log().QueryAuditEntries(platform.AuditQuery{Tenant: live.ID, From: invariantClock, To: invariantClock.Add(2 * time.Hour), Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Entries) != 200 || page.Entries[0].ID != "audit-1004" {
		t.Fatalf("durable audit page: %+v", page)
	}
	first, err := live.Log().QueryAuditEntries(platform.AuditQuery{Tenant: live.ID, From: invariantClock, To: invariantClock.Add(2 * time.Hour), Limit: 200, Cursor: ""})
	if err != nil {
		t.Fatal(err)
	}
	next, err := live.Log().QueryAuditEntries(platform.AuditQuery{Tenant: live.ID, From: invariantClock, To: invariantClock.Add(2 * time.Hour), Limit: 200, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 200 || next.Entries[0].ID != "audit-0804" {
		t.Fatalf("durable audit cursor: %+v", next.Entries[:1])
	}
}

func TestInvariantAuditReadThroughHost(t *testing.T) {
	live := newObservationTenant(t, "audit-http")
	live.audit.Remember(platform.AuditEntry{ID: "accepted-a", At: invariantClock.Add(-time.Minute), Member: "builder", App: build.ID, Action: "build.object.create", Target: "build.object/OBJ1"})
	_, handler := observationHost(live.Tenant)
	response := observationRequest(t, handler, live.ID, observationToken(live.ID, "admin"), "/v1/audit", url.Values{"limit": {"10"}})
	if response.Code != http.StatusOK {
		t.Fatalf("audit: HTTP %d: %s", response.Code, response.Body)
	}
	var page platform.AuditPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].ID != "accepted-a" {
		t.Fatalf("audit entries: %+v", page.Entries)
	}
	if !strings.Contains(page.Entries[0].Target, "build.object") || page.Coverage.RetainedFrom.IsZero() {
		t.Fatalf("audit evidence: %+v", page)
	}
	invalid := observationRequest(t, handler, live.ID, observationToken(live.ID, "admin"), "/v1/audit", url.Values{"limit": {"999"}})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid audit limit: HTTP %d: %s", invalid.Code, invalid.Body)
	}
}

func TestInvariantObservationRequestCorrelation(t *testing.T) {
	live := newObservationTenant(t, "observation-requests")
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingLogRequests, "100")
	_, handler := observationHost(live.Tenant)
	request := httptest.NewRequest(http.MethodGet, "/v1/sessions?secret=not-in-logs", nil)
	request.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
	request.Header.Set(TenantHeader, live.ID)
	request.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sessions: HTTP %d: %s", response.Code, response.Body)
	}
	requestID := response.Header().Get("X-Request-Id")
	if len(requestID) != 32 {
		t.Fatalf("request ID length is %d: %q", len(requestID), requestID)
	}
	if _, err := hex.DecodeString(requestID); err != nil {
		t.Fatalf("request ID is not hexadecimal: %q", requestID)
	}
	page := storedObservations(t, live, platform.ObservationQuery{From: invariantClock.Add(-time.Minute), To: invariantClock.Add(time.Minute), RequestID: requestID, Source: "request", Limit: 10})
	if len(page.Entries) != 1 {
		t.Fatalf("request observation count is %d", len(page.Entries))
	}
	observation := page.Entries[0]
	if observation.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || len(observation.SpanID) != 16 || observation.SpanID == "00f067aa0ba902b7" || observation.Route != "GET /v1/sessions" || observation.Attributes["status"] != "200" {
		t.Fatalf("request correlation: %+v", observation)
	}
	raw, _ := json.Marshal(observation)
	if strings.Contains(string(raw), "not-in-logs") {
		t.Fatalf("request observation retained its query string: %s", raw)
	}
	noParent := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	noParent.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
	noParent.Header.Set(TenantHeader, live.ID)
	noParentResponse := httptest.NewRecorder()
	handler.ServeHTTP(noParentResponse, noParent)
	generated := storedObservations(t, live, platform.ObservationQuery{From: invariantClock.Add(-time.Minute), To: invariantClock.Add(time.Minute), RequestID: noParentResponse.Header().Get("X-Request-Id"), Source: "request", Limit: 10}).Entries
	if len(generated) != 1 || len(generated[0].TraceID) != 32 || len(generated[0].SpanID) != 16 {
		t.Fatalf("generated trace correlation: %+v", generated)
	}
}

func TestInvariantObservationSubmissionEvidence(t *testing.T) {
	live := newObservationTenant(t, "observation-submissions")
	_, handler := observationHost(live.Tenant)
	submission := live.submission("build.object.create", build.ObjectType, "OBSERVED", invariantObject("observed", "Sensitive title"))
	raw, err := protojson.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
	request.Header.Set(TenantHeader, live.ID)
	request.Header.Set("Traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("submission: HTTP %d: %s", response.Code, response.Body)
	}
	accepted := storedObservations(t, live, platform.ObservationQuery{From: invariantClock.Add(-time.Minute), To: invariantClock.Add(time.Minute), RequestID: response.Header().Get("X-Request-Id"), Source: "submit", Limit: 10}).Entries
	if len(accepted) != 1 {
		t.Fatalf("accepted submission observation count is %d", len(accepted))
	}
	if accepted[0].App != build.ID || accepted[0].Outcome != "ok" || accepted[0].ChangeID == "" || accepted[0].Attributes["schema"] != "build.object.create" || accepted[0].TraceID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("accepted submission evidence: %+v", accepted[0])
	}
	rawAccepted, _ := json.Marshal(accepted[0])
	if strings.Contains(string(rawAccepted), "Sensitive title") {
		t.Fatalf("submission observation retained its payload: %s", rawAccepted)
	}
	bad := []byte(`{"schema":{"name":"private-payload"`)
	badRequest := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(bad))
	badRequest.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
	badRequest.Header.Set(TenantHeader, live.ID)
	badResponse := httptest.NewRecorder()
	handler.ServeHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid submission: HTTP %d: %s", badResponse.Code, badResponse.Body)
	}
	refused := storedObservations(t, live, platform.ObservationQuery{From: invariantClock.Add(-time.Minute), To: invariantClock.Add(time.Minute), RequestID: badResponse.Header().Get("X-Request-Id"), Source: "submit", Limit: 10}).Entries
	if len(refused) != 1 || refused[0].Outcome != "refused" || !strings.Contains(refused[0].Attributes["errorCode"], "INVALID_ARGUMENT") {
		t.Fatalf("refused submission evidence: %+v", refused)
	}
	rawRefused, _ := json.Marshal(refused[0])
	if strings.Contains(string(rawRefused), "private-payload") {
		t.Fatalf("refused submission observation retained its payload: %s", rawRefused)
	}
}

func TestInvariantObservationHealthReadyAndMetrics(t *testing.T) {
	live := newObservationTenant(t, "observation-health")
	host := NewHost(Tokens(map[string]string{"builder": "builder", "host-admin": "host-admin"}), live.Tenant)
	host.Now = func() time.Time { return invariantClock }
	host.HostAdmins = map[string]bool{"host-admin": true}
	handler := host.Handler()
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"status":"ok"`) {
		t.Fatalf("healthz: HTTP %d: %s", health.Code, health.Body)
	}
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("readyz: HTTP %d: %s", ready.Code, ready.Body)
	}
	metrics := httptest.NewRecorder()
	local := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	local.RemoteAddr = "127.0.0.1:1234"
	handler.ServeHTTP(metrics, local)
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), `platform_requests_total{route="GET /healthz",status_class="2xx"}`) || !strings.Contains(metrics.Body.String(), "\nplatform_requests ") {
		t.Fatalf("metrics: HTTP %d: %s", metrics.Code, metrics.Body)
	}
	denied := httptest.NewRecorder()
	external := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	external.RemoteAddr = "203.0.113.9:1234"
	external.Header.Set("Authorization", "Bearer builder")
	handler.ServeHTTP(denied, external)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("ordinary member read metrics: HTTP %d: %s", denied.Code, denied.Body)
	}
	allowed := httptest.NewRecorder()
	admin := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	admin.RemoteAddr = "203.0.113.9:1234"
	admin.Header.Set("Authorization", "Bearer host-admin")
	handler.ServeHTTP(allowed, admin)
	if allowed.Code != http.StatusOK {
		t.Fatalf("host administrator read metrics: HTTP %d: %s", allowed.Code, allowed.Body)
	}
}
