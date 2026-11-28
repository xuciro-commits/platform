package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"platformserver/kernel/protocol"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	build "platformserver/services/resources/authoring"
	filesowner "platformserver/services/resources/files"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
	"platformserver/services/work"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
)

func newQuotaTenant(tb testing.TB, id string) *invariantTenant {
	tb.Helper()
	seats := []protocol.Seat{
		{Subjects: []string{"admin-1"}, Member: platform.Member{ID: "admin-1", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, ai.ID: ai.Admin}}},
		{Subjects: []string{"admin-2"}, Member: platform.Member{ID: "admin-2", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}},
	}
	apps := []platform.Service{
		work.New(id, actions.NewAuthority),
		ai.New(id, actions.NewAuthority),
		filesowner.New(id, actions.NewAuthority),
		monitoring.New(id, actions.NewAuthority),
	}
	live := newInvariantTenantWithApps(tb, id, apps, seats...)
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingLogRequests, "0")
	return live
}

func TestInvariantK63QuotaDataSoftAskAndHardAdmission(t *testing.T) {
	live := newQuotaTenant(t, "quota-first")
	now := invariantClock

	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("ticket", "Ticket"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})

	host := NewHost(Tokens(map[string]string{
		"host-op": "host-op",
		"admin-1": "admin-1",
		"admin-2": "admin-2",
		"builder": "builder",
	}), live.Tenant)
	host.HostAdmins = map[string]bool{"host-op": true}
	currentNow := now
	host.Now = func() time.Time { return currentNow }
	handler := host.Handler()

	plansReq := httptest.NewRequest(http.MethodGet, "/v1/host/plans", nil)
	plansReq.Header.Set("Authorization", "Bearer host-op")
	plansRes := httptest.NewRecorder()
	handler.ServeHTTP(plansRes, plansReq)
	if plansRes.Code != http.StatusOK {
		t.Fatalf("GET /v1/host/plans HTTP %d: %s", plansRes.Code, plansRes.Body.String())
	}
	var plans []protocol.QuotaPlan
	if err := json.Unmarshal(plansRes.Body.Bytes(), &plans); err != nil || len(plans) < 3 {
		t.Fatalf("expected builtin quota plans data, got %+v (err=%v)", plans, err)
	}

	assignBody, _ := json.Marshal(protocol.QuotaRequest{
		Plan: "starter",
		Quota: &protocol.TenantQuota{
			Records:  protocol.QuotaThresholds{Soft: 2, Hard: 3},
			AITokens: protocol.QuotaThresholds{Soft: 1000, Hard: 2000},
		},
		Reason: "assign starter with test record cap",
	})
	assignReq := httptest.NewRequest(http.MethodPost, "/v1/host/tenants/"+live.ID+"/quota", bytes.NewReader(assignBody))
	assignReq.Header.Set("Authorization", "Bearer host-op")
	assignReq.Header.Set("Content-Type", "application/json")
	assignRes := httptest.NewRecorder()
	handler.ServeHTTP(assignRes, assignReq)
	if assignRes.Code != http.StatusOK {
		t.Fatalf("POST /v1/host/tenants/%s/quota HTTP %d: %s", live.ID, assignRes.Code, assignRes.Body.String())
	}
	if !slices.ContainsFunc(live.Audit(), func(e protocol.AuditEntry) bool { return e.Action == "host.quota.set" }) {
		t.Fatalf("expected host.quota.set in tenant audit, got %+v", live.Audit())
	}

	submitHTTP := func(token, lang, authority, schema, typ, id string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		sub := live.submission(schema, typ, id, payload)
		sub.Authority = authority
		sub.PrincipalId = token
		raw, err := protojson.Marshal(sub)
		if err != nil {
			t.Fatal(err)
		}
		member, _ := live.Member(token)
		var ver *platform.SchemaVersion
		for _, act := range live.Catalog(member) {
			if act.Schema == schema {
				ver = act.SchemaVersion
				break
			}
		}
		if ver != nil {
			sub.Schema.Version = ver.Family
			raw, _ = protojson.Marshal(sub)
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(TenantHeader, live.ID)
		if ver != nil && ver.Version != "" {
			req.Header.Set(SchemaVersionHeader, ver.Version)
		}
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}

	openQuestions := func() []protocol.AgentQuestion {
		t.Helper()
		admin, _ := live.Member("admin-1")
		page, refusal := live.Records(context.Background(), admin, protocol.WorkQuestionType, platform.Query{}, currentNow)
		if refusal != nil {
			t.Fatalf("read questions: %v", refusal)
		}
		var out []protocol.AgentQuestion
		for _, raw := range page.Records {
			q := raw.(protocol.AgentQuestion)
			if q.State == "open" {
				out = append(out, q)
			}
		}
		return out
	}

	if res := submitHTTP("builder", "en", build.ID, "invariantproject.ticket.create", "invariantproject.ticket", "T1", map[string]any{"note": "one"}); res.Code != http.StatusOK {
		t.Fatalf("create T1 HTTP %d: %s", res.Code, res.Body.String())
	}
	if qs := openQuestions(); len(qs) != 0 {
		t.Fatalf("below soft limit should not ask: %+v", qs)
	}

	if res := submitHTTP("builder", "en", build.ID, "invariantproject.ticket.create", "invariantproject.ticket", "T2", map[string]any{"note": "two"}); res.Code != http.StatusOK {
		t.Fatalf("create T2 HTTP %d: %s", res.Code, res.Body.String())
	}
	qs := openQuestions()
	if len(qs) != 1 || qs[0].ID != "quota-records-2026-10-31" {
		t.Fatalf("crossing soft limit must create 1 records question, got %+v", qs)
	}
	if !slices.Contains(qs[0].Candidates, "admin-1") || !slices.Contains(qs[0].Candidates, "admin-2") {
		t.Fatalf("soft-limit question must target all workspace admins, got %+v", qs[0].Candidates)
	}

	if res := submitHTTP("builder", "en", build.ID, "invariantproject.ticket.edit", "invariantproject.ticket", "T2", map[string]any{"note": "two-edited"}); res.Code != http.StatusOK {
		t.Fatalf("edit T2 below hard limit HTTP %d: %s", res.Code, res.Body.String())
	}
	if qs = openQuestions(); len(qs) != 1 {
		t.Fatalf("same-day soft limit question must not repeat, got %d: %+v", len(qs), qs)
	}

	admin1, _ := live.Member("admin-1")
	if refusal := live.acceptUsage(admin1, platform.AIUsage{
		Call: "ai-soft-1", At: currentNow.Add(time.Minute), Member: admin1.ID, Model: "openai/gpt-4o",
		Input: 800, Output: 400, TokensReported: true, Cost: 0.01, CostReported: true, Outcome: "ok",
	}); refusal != nil {
		t.Fatalf("accept AI usage 1: %v", refusal)
	}
	if refusal := live.acceptUsage(admin1, platform.AIUsage{
		Call: "ai-soft-2", At: currentNow.Add(2 * time.Minute), Member: admin1.ID, Model: "openai/gpt-4o",
		Input: 100, Output: 100, TokensReported: true, Cost: 0.002, CostReported: true, Outcome: "ok",
	}); refusal != nil {
		t.Fatalf("accept AI usage 2: %v", refusal)
	}
	if qs = openQuestions(); len(qs) != 2 {
		t.Fatalf("expected 1 records + 1 ai question without duplicates, got %+v", qs)
	}

	if refusal := live.acceptUsage(admin1, platform.AIUsage{
		Call: "ai-hard-3", At: currentNow.Add(3 * time.Minute), Member: admin1.ID, Model: "openai/gpt-4o",
		Input: 500, Output: 200, TokensReported: true, Cost: 0.01, CostReported: true, Outcome: "ok",
	}); refusal != nil {
		t.Fatalf("accept AI usage 3 reaching hard token limit: %v", refusal)
	}
	aiFail := live.reserveCall(admin1, platform.AIModel{Model: "gpt-4o"}, platform.ChatRequest{Call: "ai-blocked"}, 100, 100, currentNow.Add(4*time.Minute))
	if aiFail == nil || !strings.Contains(aiFail.Detail, monitoring.ReasonAIQuotaExceeded) {
		t.Fatalf("expected AI hard quota refusal on reserveCall, got %+v", aiFail)
	}

	if res := submitHTTP("builder", "en", build.ID, "invariantproject.ticket.create", "invariantproject.ticket", "T3", map[string]any{"note": "three"}); res.Code != http.StatusOK {
		t.Fatalf("create T3 reaching hard limit HTTP %d: %s", res.Code, res.Body.String())
	}

	refusedEN := submitHTTP("builder", "en", build.ID, "invariantproject.ticket.create", "invariantproject.ticket", "T4", map[string]any{"note": "four"})
	if refusedEN.Code != http.StatusForbidden || !strings.Contains(refusedEN.Body.String(), monitoring.ReasonRecordQuotaExceeded) {
		t.Fatalf("expected hard limit refusal in English, got HTTP %d: %s", refusedEN.Code, refusedEN.Body.String())
	}
	refusedZH := submitHTTP("builder", "zh-CN", build.ID, "invariantproject.ticket.edit", "invariantproject.ticket", "T1", map[string]any{"note": "blocked-edit"})
	if refusedZH.Code != http.StatusForbidden || !strings.Contains(refusedZH.Body.String(), "工作区记录配额已满") {
		t.Fatalf("expected translated zh-CN hard limit refusal, got HTTP %d: %s", refusedZH.Code, refusedZH.Body.String())
	}

	readReq := httptest.NewRequest(http.MethodGet, "/v1/records/invariantproject.ticket", nil)
	readReq.Header.Set("Authorization", "Bearer builder")
	readReq.Header.Set(TenantHeader, live.ID)
	readRes := httptest.NewRecorder()
	handler.ServeHTTP(readRes, readReq)
	if readRes.Code != http.StatusOK || !strings.Contains(readRes.Body.String(), `"total":3`) {
		t.Fatalf("reads must remain unaffected at hard limit, got HTTP %d: %s", readRes.Code, readRes.Body.String())
	}

	meReq := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer admin-1")
	meReq.Header.Set(TenantHeader, live.ID)
	meRes := httptest.NewRecorder()
	handler.ServeHTTP(meRes, meReq)
	if meRes.Code != http.StatusOK {
		t.Fatalf("sign-in /v1/me must remain unaffected at hard limit, got HTTP %d", meRes.Code)
	}

	backupBytes, err := persistence.EncodeTenantBackup(live.ID, acceptedIdentity, live.entries, nil)
	if err != nil || len(backupBytes) == 0 {
		t.Fatalf("export must remain unaffected at hard limit: %v", err)
	}

	answerRes := submitHTTP("admin-2", "en", protocol.WorkApp, protocol.WorkQuestionType+".answer", protocol.WorkQuestionType, "quota-records-2026-10-31", map[string]any{"choice": "acknowledge"})
	if answerRes.Code != http.StatusOK {
		t.Fatalf("answering inbox question must remain allowed at hard limit, got HTTP %d: %s", answerRes.Code, answerRes.Body.String())
	}

	archiveRes := submitHTTP("builder", "en", build.ID, "invariantproject.ticket.archive", "invariantproject.ticket", "T3", map[string]any{})
	if archiveRes.Code != http.StatusOK {
		t.Fatalf("archiving/deleting must remain allowed at hard limit, got HTTP %d: %s", archiveRes.Code, archiveRes.Body.String())
	}

	afterArchive := submitHTTP("builder", "en", build.ID, "invariantproject.ticket.create", "invariantproject.ticket", "T5", map[string]any{"note": "after-delete"})
	if afterArchive.Code != http.StatusOK {
		t.Fatalf("new write after freeing quota must succeed, got HTTP %d: %s", afterArchive.Code, afterArchive.Body.String())
	}
}
