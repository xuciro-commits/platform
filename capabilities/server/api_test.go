package platformserver

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/org"
	"platformserver/apps/relations"
	"platformserver/apps/work"
	"platformserver/platform"
)

// The host API contract (ADR-0023 D7): every route is described, the document
// is OpenAPI 3.1 whose references all resolve, a member's document names the
// entity types and payloads they see, and the web edge's types are exactly
// what the Go types generate.
func TestAPIContract(t *testing.T) {
	tn, err := NewTenant("t-1", NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana",
		Roles: map[string]string{PlatformApp: Admin, work.ID: "member", KnowledgeApp: KnowledgeEditor}}}),
		org.New("t-1", platform.OrgSeed{}), relations.New("t-1"), work.New("t-1"), flow.New("t-1"), ai.New("t-1"), NewAgents("t-1"), NewKnowledge("t-1"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHost(Tokens(map[string]string{"ana-token": "ana"}), tn)
	handler := h.Handler()
	req := httptest.NewRequest("GET", "/v1/openapi.json", nil)
	req.Header.Set("Authorization", "Bearer ana-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || doc["openapi"] != "3.1.0" {
		t.Fatalf("not OpenAPI 3.1: %v %.200s", err, rec.Body.String())
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	var check func(path string, v any)
	check = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				if _, ok := schemas[strings.TrimPrefix(ref, "#/components/schemas/")]; !ok || !strings.HasPrefix(ref, "#/components/schemas/") {
					t.Errorf("%s: %s resolves to nothing", path, ref)
				}
			}
			for k, child := range x {
				check(path+"/"+k, child)
			}
		case []any:
			for _, child := range x {
				check(path, child)
			}
		}
	}
	check("", doc)
	paths := doc["paths"].(map[string]any)
	ids := map[string]bool{}
	for _, r := range h.routes {
		method, path, _ := strings.Cut(r.Pattern, " ")
		op, ok := paths[path].(map[string]any)[strings.ToLower(method)].(map[string]any)
		if !ok || op["summary"] == "" {
			t.Errorf("%s is not described", r.Pattern)
			continue
		}
		if id := op["operationId"].(string); ids[id] {
			t.Errorf("operationId %s twice", id)
		} else {
			ids[id] = true
		}
	}
	for _, want := range []string{"MeView", "EntityInfo", "InboxTask", "entity:knowledge.term", "payload:knowledge.term.create", "payload:platform.member.language"} {
		if _, ok := schemas[want]; !ok {
			t.Errorf("the contract lacks %s", want)
		}
	}
	if term := schemas["entity:knowledge.term"].(map[string]any); term["properties"].(map[string]any)["refersTo"].(map[string]any)["description"] == nil {
		t.Errorf("an entity's schema carries its meaning: %v", term)
	}
	// Every named read the contract documents is a read some app serves.
	reads := map[string]bool{}
	for _, a := range tn.apps {
		for _, r := range a.Manifest().Reads {
			reads[r] = true
		}
	}
	for _, r := range namedReads {
		if name := strings.TrimPrefix(r.Pattern, "GET /v1/"); !reads[name] {
			t.Errorf("%s documents a read no app serves", r.Pattern)
		}
	}
	// The web edge's types are generated, never edited: stale types fail here
	// (go run ./cmd/api-types writes them again).
	const generated = "../../web/packages/kernel/src/gen/host.ts"
	want, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	static := NewHost(nil)
	static.Handler()
	if got := TypeScript(static.OpenAPI(nil, nil), KernelModules("../../web/packages/kernel/src/gen")); got != string(want) {
		t.Errorf("%s is stale: run go run ./cmd/api-types", generated)
	}
}
