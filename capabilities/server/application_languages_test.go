package platformserver

import (
	"encoding/json"
	"net/http/httptest"
	controlowner "platformserver/kernel/control"
	"slices"
	"strings"
	"testing"

	"platformserver/studio/build"

	"platformserver/platform"
)

func TestApplicationLanguagesFreezeAndRecover(t *testing.T) {
	compose := func() *Tenant { return builderTenant(t, "application-languages") }
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "specimen", "title": "Specimen entry", "fields": []build.Field{{Name: "note", Title: "Entry remarks", Type: "text"}, {Name: "title", Title: "Entry heading", Type: "text"}}}, txnAt)
	modelPage := modelTestPage("build.specimen", []platform.PageDataBinding{{ID: "list", Kind: "records", Model: &platform.ModelBinding{Type: "build.specimen"}}})
	modelPage.Name, modelPage.Title = "specimenmodel", "Specimen model"
	decide(t, tn, "builder", "build", build.PageType+".create", build.PageType, "MODEL", modelDraftPayload(modelPage), txnAt)
	decide(t, tn, "builder", "build", build.SchemaRelease, build.PageType, "MODEL", map[string]any{}, txnAt)
	languages := platform.Languages{"zh-CN": {"Sample desk": "样本台", "Specimen entry": "样本项目", "Entry remarks": "项目备注", "title": "Settings"}, "fr": {"Sample desk": "Bureau exemple"}}
	decide(t, tn, "builder", "build", build.AppType+".create", build.AppType, "APP", map[string]any{"name": "sampledesk", "title": "Sample desk", "pages": []string{"specimen", "specimenmodel"}, "languages": languages}, txnAt)
	preview, err := tn.PreviewRelease(memberOf(t, tn, "builder"), platform.AssetApp, "APP")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	if _, err := tn.SaveReleaseCandidate(memberOf(t, tn, "builder"), platform.AssetApp, "APP", preview.CandidateID, "save-language", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", "build", build.AppType+".edit", build.AppType, "APP", map[string]any{"languages": platform.Languages{"zh-CN": {"Sample desk": "未发布词典"}}}, txnAt)
	if got := tn.i18n.SayInApp("build", "zh-CN", "Sample desk"); got != "Sample desk" {
		t.Fatalf("draft changed live dictionary: %s", got)
	}
	if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), preview.CandidateID, "activate-language", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "user", "build", "build.specimen.create", "build.specimen", "ROW", map[string]any{"note": "Sample desk", "title": "Sample desk"}, txnAt)

	check := func(tn *Tenant) {
		t.Helper()
		if !slices.Contains(tn.i18n.Languages(), "fr") || tn.i18n.SayInApp("build", "zh-CN", "Sample desk") != "样本台" {
			t.Fatal("frozen application dictionary was not installed")
		}
		stored, _ := platform.Get[build.Application](tn.automation("build", false), "APP")
		if stored.Languages["zh-CN"]["Sample desk"] != "未发布词典" {
			t.Fatal("activation overwrote later translation draft")
		}
		host := NewHost(Tokens(map[string]string{"token": "user"}), tn)
		call := func(method, path, body, language string) string {
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Accept-Language", language)
			recorder := httptest.NewRecorder()
			host.Handler().ServeHTTP(recorder, req)
			if recorder.Code != 200 {
				t.Fatalf("%s: %d %s", path, recorder.Code, recorder.Body.String())
			}
			return recorder.Body.String()
		}
		if got := call("GET", "/v1/applications", "", "zh-CN"); !strings.Contains(got, `"title":"样本台"`) || !strings.Contains(got, `"title":"样本项目"`) {
			t.Fatalf("application discovery is untranslated: %s", got)
		}
		if got := call("POST", "/v1/page-view", `{"app":"sampledesk","owner":"build","page":"specimen"}`, "zh-CN"); !strings.Contains(got, `"title":"项目备注"`) || !strings.Contains(got, `"name":"note"`) {
			t.Fatalf("page declarations are untranslated or renamed: %s", got)
		}
		var modelResult PageModelResult
		if err := json.Unmarshal([]byte(call("POST", "/v1/page-model/read", `{"page":{"app":"build","kind":"page","name":"specimenmodel"},"section":"list","limit":20}`, "zh-CN")), &modelResult); err != nil {
			t.Fatal(err)
		}
		if field := slices.IndexFunc(modelResult.Fields, func(field platform.FieldInfo) bool { return field.Name == "note" }); field < 0 || modelResult.Fields[field].Title != "项目备注" {
			t.Fatalf("model fields overwrote translated entity declarations: %+v", modelResult.Fields)
		}
		if len(modelResult.Records) != 1 || modelResult.Records[0]["title"] != "Sample desk" || modelResult.Records[0]["note"] != "Sample desk" {
			t.Fatalf("model translation changed business values: %+v", modelResult.Records)
		}
		if got := call("GET", "/v1/applications", "", "en"); !strings.Contains(got, `"title":"Sample desk"`) {
			t.Fatalf("English source changed: %s", got)
		}
		for _, d := range tn.Definitions(memberOf(t, tn, "user")) {
			if d.Ref.Kind != platform.AssetApp || d.Ref.Name != "sampledesk" {
				continue
			}
			before := string(platform.Raw(d))
			translated := tn.i18n.Translate(d, "zh-CN").(map[string]any)["application"].(map[string]any)
			dict := translated["languages"].(map[string]any)["zh-CN"].(map[string]any)
			if dict["title"] != "Settings" || dict["Sample desk"] != "样本台" || string(platform.Raw(d)) != before {
				t.Fatal("translation changed dictionary entries or original declaration")
			}
		}
		record, _ := tn.RecordOf(memberOf(t, tn, "user"), "build.specimen", "ROW", txnAt)
		if !strings.Contains(string(platform.Raw(record)), `"note":"Sample desk"`) {
			t.Fatal("translation changed business text")
		}
	}
	check(tn)
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	check(recovered)
	saved, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	check(restored)
	CheckReplay(t, tn, entries, compose)
}

func TestApplicationLanguagesConflictsAndTenantOverride(t *testing.T) {
	tn := composeTenant(t, "application-words", []platform.Seat{seatOf("builder", "build:builder", "platform:admin")}, build.New("application-words"))
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "entry", "title": "Entry", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, txnAt)
	publish := func(id, name, text string) {
		t.Helper()
		decide(t, tn, "builder", "build", build.AppType+".create", build.AppType, id, map[string]any{"name": name, "title": name, "pages": []string{"entry"}, "languages": platform.Languages{"zh-CN": {"Ambiguous entry": text, "Settings": text, "Special {thing}": text + "{thing}"}}}, txnAt)
		decide(t, tn, "builder", "build", build.SchemaHandOver, build.AppType, id, map[string]any{}, txnAt)
	}
	publish("FIRST", "first", "第一")
	if got := tn.i18n.SayInApp("build", "zh-CN", "Special Ambiguous entry"); got != "第一第一" {
		t.Fatalf("application pattern did not reuse scoped dictionary: %s", got)
	}
	publish("SECOND", "second", "第二")
	if got := tn.i18n.SayInApp("build", "zh-CN", "Ambiguous entry"); got != "Ambiguous entry" {
		t.Fatalf("conflicting words picked an application: %s", got)
	}
	if got := tn.i18n.SayInApp("build", "zh-CN", "Settings"); got != "设置" {
		t.Fatalf("conflict did not fall back to platform dictionary: %s", got)
	}
	decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaTranslationSet, controlowner.TranslationType, "zh-CN", map[string]any{"text": "Ambiguous entry", "translation": "租户用词"}, txnAt)
	if got := tn.i18n.SayInApp("build", "zh-CN", "Ambiguous entry"); got != "租户用词" {
		t.Fatalf("tenant override lost to application dictionary: %s", got)
	}
	decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaTranslationSet, controlowner.TranslationType, "zh-CN", map[string]any{"text": "Settings", "translation": "租户设置"}, txnAt)
	if got := tn.i18n.SayInApp("build", "zh-CN", "Settings"); got != "租户设置" {
		t.Fatalf("tenant override lost to native dictionary: %s", got)
	}
	// Reversing the source declarations cannot choose a different conflict winner.
	definitions := slices.Clone(tn.definitions)
	slices.Reverse(definitions)
	tn.setDefinitions(definitions)
	decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaTranslationSet, controlowner.TranslationType, "zh-CN", map[string]any{"text": "Ambiguous entry", "translation": ""}, txnAt)
	if got := tn.i18n.SayInApp("build", "zh-CN", "Ambiguous entry"); got != "Ambiguous entry" {
		t.Fatalf("dictionary ordering changed conflict resolution: %s", got)
	}
}

func TestPagePresentationWordsAreResolvedWithoutChangingOriginalContent(t *testing.T) {
	tn := composeTenant(t, "presentation-words", nil)
	app := platform.Definition{Ref: platform.AssetRef{App: "build", Kind: platform.AssetApp, Name: "words"}, Application: &platform.Application{Name: "words", Languages: platform.Languages{"zh-CN": {"Instructions": "操作说明", "Open items": "未完成项", "Safe": "安全"}}}}
	tn.i18n.InstallApplications([]platform.Definition{app})
	original := platform.Page{Format: 3, Name: "display", Title: "Instructions", WidgetSpec: platform.PageWidgetSpec{Format: "platform.page.web.v1", Body: json.RawMessage(`{"sections":[{"id":"heading","widget":"heading","title":"Instructions","text":"Open items"}]}`)}}
	before := platform.Raw(original)
	version, _ := platform.PageContentVersion(original)
	def := platform.Definition{Ref: platform.AssetRef{App: "build", Kind: platform.AssetPage, Name: "display"}, Page: &original}
	translated := tn.i18n.Translate(def, "zh-CN").(map[string]any)["page"].(map[string]any)
	words := translated["presentationWords"].(map[string]string)
	if words["Instructions"] != "操作说明" || words["Open items"] != "未完成项" || translated["title"] != "操作说明" {
		t.Fatal("page did not receive its original resolved owner dictionary", translated)
	}
	if string(before) != string(platform.Raw(original)) {
		t.Fatal("discovery rewrote the published page")
	}
	original.PresentationWords = words
	after, _ := platform.PageContentVersion(original)
	if version != after {
		t.Fatal("presentation dictionary changed original content identity")
	}
	opaque := map[string]any{"app": "build", "title": "Instructions", "widgetSpec": map[string]any{"title": "Instructions", "text": "Open items"}}
	view := tn.i18n.Translate(opaque, "zh-CN").(map[string]any)
	raw := view["widgetSpec"].(map[string]any)
	if view["title"] != "操作说明" || raw["title"] != "Instructions" || raw["text"] != "Open items" {
		t.Fatal("translator interpreted opaque presentation bytes", view)
	}
}
