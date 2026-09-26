package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/apps/org"
	"platformserver/apps/relations"
	"platformserver/apps/work"
	"platformserver/platform"
)

// Languages (ADR-0023): declarations are served in the request's language,
// records never; every platform app's text has a Chinese translation.
func TestLanguages(t *testing.T) {
	tn, err := NewTenant("t-1", NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana",
		Roles: map[string]string{PlatformApp: Admin, work.ID: "member", org.ID: "admin"}}},
		Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{work.ID: "member"}}},
		Seat{Subjects: []string{"cy"}, Member: platform.Member{ID: "cy", Roles: map[string]string{work.ID: "member"}}}),
		org.New("t-1", platform.OrgSeed{}), relations.New("t-1"), work.New("t-1"), NewFlows("t-1"), NewAI("t-1"), NewAgents("t-1"), NewKnowledge("t-1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range []string{PlatformApp, org.ID, relations.ID, work.ID, FlowApp, AIApp, AgentApp, KnowledgeApp} {
		if missing := tn.Untranslated(app, "zh-CN"); len(missing) > 0 {
			t.Errorf("%s lacks Chinese for %q", app, missing)
		}
	}
	h := NewHost(Tokens(map[string]string{"ana-token": "ana", "bo-token": "bo", "cy-token": "cy"}), tn)
	callAs := func(token, path, language string) string {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if language != "" {
			req.Header.Set("Accept-Language", language)
		}
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		return rec.Body.String()
	}
	call := func(path, language string) string { return callAs("ana-token", path, language) }
	for _, c := range []struct{ path, language, contains string }{
		{"/v1/me", "", `{"id":"platform","title":"Settings","role":"admin"}`},
		{"/v1/me", "zh-CN,zh;q=0.9,en;q=0.8", `{"id":"platform","role":"admin","title":"设置"}`},
		{"/v1/me", "zh-Hans", `"language":"zh-CN"`},
		{"/v1/me", "zh", `"languages":["zh-CN"]`},
		{"/v1/me", "zh-TW", `"title":"Settings"`},          // no traditional dictionary: English
		{"/v1/me", "en-GB,zh;q=0.5", `"title":"Settings"`}, // the first language wins
		{"/v1/me", "fr,zh;q=0.5", `"title":"设置"`},          // the first the tenant speaks
		{"/v1/actions", "zh-CN", `"title":"授予角色"`},
		{"/v1/entities", "zh-CN", `"title":"任务"`},
		{"/v1/entities", "zh-CN", `"choiceTitles":["进行中",`}, // a task's states; the choices stay the values records hold
		{"/v1/settings", "zh-CN", `"title":"智能体使用的模型"`},
	} {
		if got := call(c.path, c.language); !strings.Contains(got, c.contains) {
			t.Errorf("%s in %q: want %s, got %.600s", c.path, c.language, c.contains, got)
		}
	}
	// Records are what people wrote: never translated, whatever they say.
	if got := tn.Translate(map[string]any{"record": map[string]any{"name": "Settings"}, "title": "Settings"}, "zh-CN").(map[string]any); got["title"] != "设置" || got["record"].(map[string]any)["name"] != "Settings" {
		t.Errorf("translated %v", got)
	}

	// What apps write for people reads in the reader's language: exactly, or
	// by a pattern whose values are translated in turn (ADR-0023 6b).
	for _, c := range []struct{ in, want string }{
		{"Approve: Grant role platform.member/bo", "审批：授予角色 platform.member/bo"},
		{"Grant role platform.member/bo: approved", "授予角色 platform.member/bo：已批准"},
		{"Grant role platform.member/bo: rejected", "授予角色 platform.member/bo：已驳回"},
		{"Overdue: Approve: Grant role platform.member/bo", "逾期：审批：授予角色 platform.member/bo"},
		{"Settings", "设置"},
		{"Something no dictionary knows", "Something no dictionary knows"},
		{"Move a task from open or done to canceled.", "把任务从进行中或已完成改为已取消。"}, // a generated sentence, by its words
		{"Create saved view", "新建已保存视图"},
	} {
		if got := tn.Say("zh-CN", c.in); got != c.want {
			t.Errorf("Say(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if !tn.says("zh-CN", "Archive a task: it leaves lists but stays referenced and in history.", 0) || tn.says("zh-CN", "Archive a gizmo: it leaves lists but stays referenced and in history.", 0) {
		t.Errorf("a pattern counts only when the language says its every value")
	}
	if got := tn.Say("", "Overdue: x"); got != "Overdue: x" {
		t.Errorf("English is as written: %q", got)
	}

	// A member's own language, then the browser's, then the tenant's default.
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	keys := 0
	decide := func(who, schema, typ, id string, payload any) string {
		keys++
		m, _ := tn.app(PlatformApp).(*Console).Member(who)
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(m, &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: PlatformApp, IdempotencyKey: fmt.Sprint("l", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	for _, c := range []struct{ who, id, language, want string }{
		{"bo", "bo", "zh-CN", "ok"},
		{"bo", "ana", "zh-CN", "ERROR_CODE_POLICY_DENIED"}, // only your own, unless an administrator
		{"bo", "bo", "fr", "ERROR_CODE_INVALID_ARGUMENT"},  // a language the tenant speaks
		{"ana", "cy", "en", "ok"},
	} {
		if got := decide(c.who, SchemaLanguage, MemberType, c.id, map[string]string{"language": c.language}); got != c.want {
			t.Errorf("%s sets %s's language %s: %s, want %s", c.who, c.id, c.language, got, c.want)
		}
	}
	decide("ana", SchemaLanguage, MemberType, "cy", map[string]string{"language": ""})
	if got := callAs("bo-token", "/v1/me", "en"); !strings.Contains(got, `"language":"zh-CN"`) || !strings.Contains(got, `"preferred":"zh-CN"`) {
		t.Errorf("bo's own language wins over the browser's: %s", got)
	}
	if got := decide("ana", SchemaSettingSet, SettingType, PlatformApp+"/"+SettingLanguage, map[string]string{"value": "zh-CN"}); got != "ok" {
		t.Fatalf("tenant default: %s", got)
	}
	for _, c := range []struct{ language, want string }{{"", `"language":"zh-CN"`}, {"en-US", `"language":""`}, {"fr", `"language":"zh-CN"`}} {
		if got := callAs("cy-token", "/v1/me", c.language); !strings.Contains(got, c.want) {
			t.Errorf("cy asks %q: want %s, got %.300s", c.language, c.want, got)
		}
	}
}
