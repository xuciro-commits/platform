package platformserver

import (
	"net/http/httptest"
	"strings"
	"testing"

	"platformserver/platform"
)

// Languages (ADR-0023): declarations are served in the request's language,
// records never; every platform app's text has a Chinese translation.
func TestLanguages(t *testing.T) {
	tn, err := NewTenant("t-1", NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana",
		Roles: map[string]string{PlatformApp: Admin, WorkApp: "member", OrgApp: "admin"}}}),
		NewOrganization("t-1", platform.OrgSeed{}), NewRelations("t-1"), NewWork("t-1"), NewFlows("t-1"), NewAI("t-1"), NewAgents("t-1"), NewKnowledge("t-1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range []string{PlatformApp, OrgApp, RelationsApp, WorkApp, FlowApp, AIApp, AgentApp, KnowledgeApp} {
		if missing := tn.Untranslated(app, "zh-CN"); len(missing) > 0 {
			t.Errorf("%s lacks Chinese for %q", app, missing)
		}
	}
	h := NewHost(Tokens(map[string]string{"ana-token": "ana"}), tn)
	call := func(path, language string) string {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer ana-token")
		if language != "" {
			req.Header.Set("Accept-Language", language)
		}
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		return rec.Body.String()
	}
	for _, c := range []struct{ path, language, contains string }{
		{"/v1/me", "", `{"id":"platform","title":"Settings","role":"admin"}`},
		{"/v1/me", "zh-CN,zh;q=0.9,en;q=0.8", `{"id":"platform","role":"admin","title":"设置"}`},
		{"/v1/me", "zh-Hans", `"language":"zh-CN"`},
		{"/v1/me", "zh", `"languages":["zh-CN"]`},
		{"/v1/me", "zh-TW", `"title":"Settings"`},  // no traditional dictionary: English
		{"/v1/me", "en-GB,zh;q=0.5", `"title":"Settings"`}, // the first language wins
		{"/v1/me", "fr,zh;q=0.5", `"title":"设置"`},        // the first the tenant speaks
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
	if got := Translate(map[string]any{"record": map[string]any{"name": "Settings"}, "title": "Settings"}, tn.Dictionary("zh-CN")).(map[string]any); got["title"] != "设置" || got["record"].(map[string]any)["name"] != "Settings" {
		t.Errorf("translated %v", got)
	}
}
