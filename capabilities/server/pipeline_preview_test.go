package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"platformserver/platform/authz"
)

func TestPipelineDraftPreview(t *testing.T) {
	at := time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC)
	tn := composeTenant(t, "pipeline-preview", []Seat{seatOf("builder", "build:builder"), seatOf("integrator", "build:integrator"), seatOf("operator", "build:user")}, build.New("pipeline-preview"))
	writes := 0
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { writes++; return entry.Body, nil }
	for _, id := range []string{"input", "lookup", "output"} {
		decide(t, tn, "builder", construction.ID, construction.DatasetType+".create", construction.DatasetType, id, map[string]any{"name": id, "title": id}, at)
	}
	rows := make([]map[string]any, 101)
	for i := range rows {
		rows[i] = map[string]any{"sku": "A", "qty": "3"}
	}
	rows[0]["sku"] = ""
	decide(t, tn, "builder", construction.ID, construction.SchemaDatasetLoad, construction.DatasetType, "input", map[string]any{"rows": rows}, at)
	decide(t, tn, "builder", construction.ID, construction.SchemaDatasetLoad, construction.DatasetType, "lookup", map[string]any{"rows": []map[string]any{{"sku": "A", "label": "Matched"}}}, at)
	decide(t, tn, "builder", construction.ID, construction.PipelineType+".create", construction.PipelineType, "PIPE", map[string]any{"name": "trial", "title": "Trial", "input": "input", "outputDataset": "output"}, at)
	pipeline, _ := platform.Get[construction.Pipeline](tn.automation(construction.ID, false), "PIPE")
	pipeline.Steps = []construction.Step{{Kind: "cast", Column: "qty", Type: "number"}, {Kind: "compute", To: "amount", Formula: "qty * 2"}, {Kind: "lookup", Dataset: "lookup", Column: "sku", Match: "sku", Columns: []string{"label"}}}
	pipeline.Expectations = []construction.Expectation{{Column: "sku", Rule: "notnull"}}
	request := PipelinePreviewRequest{Pipeline: pipeline}
	originalDraft := platform.Raw(request)
	snapshot := func() []byte {
		raw, _, err := tn.Snapshot(func() int64 { return 0 })
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before, written := snapshot(), writes
	out, problem := tn.PreviewPipeline(memberOf(t, tn, "integrator"), request, at)
	if problem != nil {
		t.Fatal(problem)
	}
	if out.InputRows != 101 || out.InputVersion != 1 || out.SampledRows != 100 || !out.Truncated || len(out.Rows) != 99 || len(out.Quarantine) != 1 || out.Rows[0]["label"] != "Matched" || fmt.Sprint(out.Rows[0]["amount"]) != "6" {
		t.Fatalf("draft transformations or sample counts differ: %s", platform.Raw(out))
	}
	if !bytes.Equal(originalDraft, platform.Raw(request)) {
		t.Fatal("trial changed the request draft")
	}
	if writes != written {
		t.Fatal("trial appended journal entries")
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("trial changed production state")
	}
	// The HTTP path accepts the entire unsaved editor value and uses the same
	// read permission; it never has to publish or save the pipeline first.
	host := NewHost(Tokens(map[string]string{"builder-token": "builder", "operator-token": "operator"}), tn)
	call := func(token string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/pipelines/preview", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		host.Handler().ServeHTTP(rec, req)
		return rec
	}
	if response := call("builder-token", originalDraft); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sampledRows":100`) {
		t.Fatalf("preview HTTP: %d %s", response.Code, response.Body.String())
	}
	if response := call("operator-token", originalDraft); response.Code != http.StatusForbidden {
		t.Fatalf("business member trial: %d %s", response.Code, response.Body.String())
	}
	request.Pipeline.Steps = []construction.Step{{Kind: "cast", Column: "qty", Type: "number"}, {Kind: "compute", To: "amount", Formula: "qty / 0"}}
	if _, problem := tn.PreviewPipeline(memberOf(t, tn, "builder"), request, at); problem == nil {
		t.Fatal("new draft formula used frozen divide-by-zero semantics")
	}
	if writes != written || !bytes.Equal(before, snapshot()) {
		t.Fatal("successful or refused HTTP trial changed production")
	}
}

func TestPipelineDraftPreviewBoundsAndAuthorization(t *testing.T) {
	at := time.Date(2026, 10, 17, 21, 0, 0, 0, time.UTC)
	tn := composeTenant(t, "pipeline-preview-bounds", []Seat{seatOf("builder", "build:builder")}, build.New("pipeline-preview-bounds"))
	for _, id := range []string{"input", "lookup", "output"} {
		decide(t, tn, "builder", construction.ID, construction.DatasetType+".create", construction.DatasetType, id, map[string]any{"name": id, "title": id}, at)
	}
	rows := make([]map[string]any, 100)
	for i := range rows {
		rows[i] = map[string]any{"sku": "A"}
	}
	for _, id := range []string{"input", "lookup"} {
		decide(t, tn, "builder", construction.ID, construction.SchemaDatasetLoad, construction.DatasetType, id, map[string]any{"rows": rows}, at)
	}
	member := memberOf(t, tn, "builder")
	request := PipelinePreviewRequest{Pipeline: construction.Pipeline{Name: "trial", Title: "Trial", Input: "input", OutputDataset: "output", Steps: []construction.Step{{Kind: "join", Dataset: "lookup", Column: "sku", Match: "sku"}}}}
	if _, problem := tn.PreviewPipeline(member, request, at); problem == nil || !strings.Contains(problem.Message, "1000 row limit") {
		t.Fatalf("expanding join was not bounded: %v", problem)
	}
	request.Pipeline.Steps = []construction.Step{}
	if out, problem := tn.PreviewPipeline(member, request, at); problem != nil || len(out.Rows) != 100 || out.Truncated {
		t.Fatalf("exact sample limit: rows=%d truncated=%t refusal=%v", len(out.Rows), out.Truncated, problem)
	}
	// A member who may discover datasets still cannot bypass a policy on the
	// dataset version's read by requesting transformed sample data.
	tn.app(construction.ID).Manifest().Actions.UsePolicies(func() []authz.Policy {
		return []authz.Policy{{ID: "deny-version", Effect: "deny", Permission: construction.DatasetVersionType + ".read"}}
	})
	if out, problem := tn.PreviewPipeline(member, request, at); problem == nil || len(out.Rows) != 0 || len(out.Quarantine) != 0 {
		t.Fatalf("denied input escaped through trial: %s %v", platform.Raw(out), problem)
	}
	tn.app(construction.ID).Manifest().Actions.UsePolicies(func() []authz.Policy {
		return []authz.Policy{{ID: "deny-trial", Effect: "deny", Permission: "build.pipeline.preview"}}
	})
	if _, problem := tn.PreviewPipeline(member, request, at); problem == nil || problem.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("trial ignored explicit deny: %v", problem)
	}
}
