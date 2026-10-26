package platformserver

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestConfirmationReadRevalidatesCredentialAndExcludesConcurrentPoll(t *testing.T) {
	at := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	tn := confirmationTenant(t, "confirmation-credential")
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	credential := "Bearer original-read-only"
	tn.Secrets = func(name string) ([]byte, bool) { return []byte(credential), name == "status-reader" }
	reads, posts := 0, 0
	rotate := false
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		if req.Method == http.MethodPost {
			posts++
			if req.URL.Host == "wms.example" {
				return nil, io.ErrUnexpectedEOF
			}
			return confirmationResponse(200, `{}`)
		}
		if req.URL.Path != "/rows" {
			return confirmationResponse(200, `{}`)
		}
		reads++
		if req.Header.Get("Authorization") != credential {
			t.Fatal("status read used an unexpected credential")
		}
		if rotate {
			rotate = false
			tn.ReconcileConfirmations(at.Add(time.Second))
			credential = "Bearer replacement-read-only"
		}
		return confirmationResponse(200, `[{"id":"R1","state":"confirmed"}]`)
	}
	spec := businessConfirmationSpec("hold", "hold")
	spec.DeadlineSeconds = 3600
	prepareBusinessConfirmation(t, tn, at, spec)
	decide(t, tn, "builder", construction.ID, "build.connection.edit", construction.ConnectionType, "wms", map[string]any{"secret": "status-reader"}, at)
	decide(t, tn, "builder", construction.ID, "build.connection.check", construction.ConnectionType, "wms", map[string]any{}, at)
	tn.CheckConnections(at)
	decide(t, tn, "operator", construction.ID, "build.receipt.create", "build.receipt", "R1", map[string]any{"status": "new"}, at)
	decide(t, tn, "operator", construction.ID, "build.receipt.start", "build.receipt", "R1", map[string]any{}, at)
	tn.Dispatch(at)
	before := len(entries)
	rotate = true
	tn.ReconcileConfirmations(at.Add(time.Second))
	run := currentConfirmation(t, tn)
	if reads != 1 || len(entries) != before || len(run.Parties[1].Differences) != 0 || run.State == "confirmed" {
		t.Fatalf("concurrent or credential-stale read became evidence: reads=%d writes=%d run=%+v", reads, len(entries)-before, run)
	}
	tn.ReconcileConfirmations(at.Add(2 * time.Second))
	run = currentConfirmation(t, tn)
	if reads != 2 || posts != 2 || run.State != "confirmed" || len(run.Parties[1].Differences) != 1 {
		t.Fatalf("fresh read did not complete without resending the unknown action: reads=%d posts=%d run=%+v", reads, posts, run)
	}
	for _, entry := range entries {
		if bytes.Contains(entry.Body, []byte("Bearer original-read-only")) || bytes.Contains(entry.Body, []byte("Bearer replacement-read-only")) {
			t.Fatal("status read retained its credential")
		}
	}
	if bytes.Contains(platform.Raw(run), []byte(credential)) {
		t.Fatal("business evidence retained its credential")
	}
}
