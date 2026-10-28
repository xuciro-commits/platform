package platformserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"platformserver/apps/build"
	enterprise "platformserver/engine/organization"
	"platformserver/platform"
)

func TestPackageInitializationRetainsAcceptedPrefixAcrossLostReply(t *testing.T) {
	const id = "package-initialization"
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "enterprise:admin"), seatOf("limited", "build:builder")}, build.New(id), enterprise.New(id, platform.OrgSeed{}))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	publishObject(t, tn, "builder", "ROW", map[string]any{"name": "sample", "title": "Sample", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}, at)
	steps := []map[string]any{}
	for _, step := range []struct {
		app, schema, typ, id string
		payload              any
	}{
		{"enterprise", "enterprise.element.add", "enterprise.element", "unit", map[string]any{"stereotype": enterprise.Organization, "name": "Unit"}},
		{"build", "build.sample.create", "build.sample", "row", map[string]any{"note": "Original value"}},
	} {
		steps = append(steps, map[string]any{"name": step.id, "app": step.app, "schema": step.schema, "target": map[string]string{"type": step.typ, "id": step.id}, "payload": string(platform.Raw(step.payload))})
	}
	plan := filepath.Join(t.TempDir(), "package.json")
	if err := os.WriteFile(plan, platform.Raw(map[string]any{"id": "example.initialization", "version": "1.0.0", "blueprint": map[string]any{"setup": map[string]any{"roles": []any{}, "steps": steps}}}), 0600); err != nil {
		t.Fatal(err)
	}
	host := NewHost(Tokens(map[string]string{"builder-token": "builder", "limited-token": "limited"}), tn)
	host.Now = func() time.Time { return at }
	var lost atomic.Bool
	handler := host.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/submissions" {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if bytes.Contains(raw, []byte(`"build.sample.create"`)) && lost.CompareAndSwap(false, true) {
				answer := httptest.NewRecorder()
				handler.ServeHTTP(answer, r)
				if answer.Code != http.StatusOK || !bytes.Contains(answer.Body.Bytes(), []byte(`"record"`)) {
					t.Errorf("accepted reply: %s", answer.Body)
				}
				connection, _, _ := w.(http.Hijacker).Hijack()
				_ = connection.Close()
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	initialize := func(token string) ([]byte, error) {
		command := exec.Command("python3", "../../deploy/local/initialize.py", "--package", plan, "--host", server.URL, "--tenant", id, "--token-env", "PLATFORM_INITIALIZE_TEST_TOKEN", "--run-key", "initial-v1")
		command.Env = append(os.Environ(), "PLATFORM_INITIALIZE_TEST_TOKEN="+token)
		return command.CombinedOutput()
	}
	if output, err := initialize("limited-token"); err == nil || !strings.Contains(string(output), "the action is unavailable to this member") || len(tn.app(enterprise.ID).(*enterprise.Enterprise).Model().Elements) != 0 {
		t.Fatalf("initialization bypassed original owner: %v %s", err, output)
	}
	if output, err := initialize("builder-token"); err == nil || !lost.Load() {
		t.Fatalf("lost reply did not interrupt setup: %v %s", err, output)
	}
	before, count := snapshot(tn), len(entries)
	for range 2 {
		if output, err := initialize("builder-token"); err != nil || len(entries) != count || snapshot(tn) != before {
			t.Fatalf("retry repeated accepted writes: %v %s", err, output)
		}
	}
	row := declaredRecord(t, tn, "builder", "build.sample", "row", at)
	if row["revision"] != float64(1) || row["note"] != "Original value" {
		t.Fatalf("repeated row: %+v", row)
	}
	CheckReplay(t, tn, entries, compose)
}
