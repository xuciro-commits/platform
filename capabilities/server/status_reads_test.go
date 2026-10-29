package platformserver

import (
	"encoding/json"
	"net/http"
	"platformkernel/kernel"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/build"
	"testing"
	"time"
)

// A source's publication and an explicit pull are different requests. Business
// status polling must use the former's frozen authority, even before any pull.
func TestPublishedSourceStatusReadsUsePublisherAuthority(t *testing.T) {
	for _, kind := range []string{"confirmation", "dispatch"} {
		t.Run(kind, func(t *testing.T) {
			at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
			var tn *Tenant
			var check func() *kernel.Error
			var sourceRaw, connectionRaw json.RawMessage
			var revision uint32
			var applicant string
			if kind == "confirmation" {
				tn = confirmationTenant(t, "publisher-confirmation")
				tn.Outbound = func(*http.Request, bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
				prepareBusinessConfirmation(t, tn, at, businessConfirmationSpec("hold", "hold"))
				decide(t, tn, "operator", build.ID, "build.receipt.create", "build.receipt", "RECEIPT", map[string]any{"status": "new"}, at)
				decide(t, tn, "operator", build.ID, "build.receipt.start", "build.receipt", "RECEIPT", map[string]any{"item": "RECEIPT"}, at)
				runs, _, err := platform.Find[platform.ConfirmationRun](tn.automation(build.ID, false), platform.Query{})
				if err != nil || len(runs) != 1 {
					t.Fatal("missing confirmation", runs, err)
				}
				run := runs[0]
				check = func() *kernel.Error { return (hostView{t: tn}).CheckConfirmationRead(run, 1, at) }
				sourceRaw, connectionRaw, revision, applicant = run.Parties[1].Source, run.Parties[1].Connection, run.Revision, run.Member
			} else {
				tn = deviceDispatchTenant(t)
				prepareDeviceDispatch(t, tn, at, "source", false)
				run := startDeviceDispatch(t, tn, at)
				check = func() *kernel.Error { return (hostView{t: tn}).CheckDispatchRead(run, at) }
				sourceRaw, connectionRaw, revision, applicant = run.Source, run.Connection, run.Revision, run.Member
			}
			var source build.Source
			if err := json.Unmarshal(sourceRaw, &source); err != nil {
				t.Fatal(err)
			}
			if source.Publisher != "builder" || source.Puller != "" {
				t.Fatalf("publication acquired pull execution, or lost publisher: %+v", source)
			}
			if err := check(); err != nil {
				t.Fatal("published source status read was refused before any explicit pull", err)
			}
			before, ok := tn.statusReadStamp(revision, applicant, sourceRaw, connectionRaw, false)
			if !ok {
				t.Fatal("missing original read stamp")
			}
			decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaRevoke, controlowner.MemberType, "builder", map[string]any{"app": build.ID}, at)
			if err := check(); err == nil {
				t.Fatal("revoked source publisher authorized a status read")
			}
			after, ok := tn.statusReadStamp(revision, applicant, sourceRaw, connectionRaw, false)
			if !ok || after == before {
				t.Fatal("status read stamp missed publisher revocation")
			}
		})
	}
}
