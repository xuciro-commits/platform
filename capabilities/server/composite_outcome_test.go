package platformserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/studio/build"
)

func TestCompositeHTTPDistinguishesSavedRefusalFromUnknownOutcome(t *testing.T) {
	tn := builderTenant(t, "composite-outcome")
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "retained", map[string]any{"name": "retained", "title": "Retained", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}, at)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "retained", map[string]any{}, at)
	handler := NewHost(Tokens(map[string]string{"builder": "builder"}), tn).Handler()
	edits := []CompositeEdit{{Schema: "build.object.archive", Target: &pb.EntityRef{Type: build.ObjectType, Id: "retained"}, Payload: json.RawMessage(`{}`)}}
	post := func(edits []CompositeEdit) map[string]any {
		t.Helper()
		body, err := json.Marshal(map[string]any{"key": "original", "edits": edits})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/v1/composites", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer builder")
		r.Header.Set("Platform-Tenant", tn.ID)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var answer map[string]any
		if w.Code != http.StatusConflict || json.Unmarshal(w.Body.Bytes(), &answer) != nil {
			t.Fatalf("composite: %d %s", w.Code, w.Body.String())
		}
		return answer
	}

	tn.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append answer unavailable") }
	if answer := post(edits); answer["outcome"] != nil || answer["error"] != "append answer unavailable" {
		t.Fatalf("unsaved outcome reported as terminal: %+v", answer)
	}
	tn.AcceptResult = nil
	first := post(edits)
	if first["outcome"] != "refused" || !strings.Contains(first["error"].(string), "A published object cannot be archived") {
		t.Fatalf("saved refusal lost its terminal outcome or owner reason: %+v", first)
	}
	if again := post(edits); again["outcome"] != "refused" || again["error"] != first["error"] {
		t.Fatalf("same-key retry lost saved refusal: %+v", again)
	}
	edits[0].Target.Id = "changed"
	if answer := post(edits); answer["outcome"] != nil {
		t.Fatalf("a different intent inherited a saved outcome: %+v", answer)
	}
}
