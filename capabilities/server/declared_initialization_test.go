package platformserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"platformserver/apps/core"
	"platformserver/apps/enterprise"
)

func TestDeclaredInitializationUsesOriginalOwnersAndDoesNotRepeat(t *testing.T) {
	for _, industry := range []string{"hospitality", "manufacturing"} {
		t.Run(industry, func(t *testing.T) {
			at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
			seats := []Seat{seatOf("builder", "build:builder", "platform:admin", "enterprise:admin", "core:accountant"), seatOf("limited", "build:builder")}
			var entries []Entry
			tn := declaredIndustryTenant(t, "initialize-"+industry, industry, seats, at, declaredFixtureOptions{Entries: &entries})
			organization := tn.app(enterprise.ID).(*enterprise.Enterprise)
			if len(organization.Model().Elements) != 0 {
				t.Fatal("installing or activating a package seeded an organization")
			}
			host := NewHost(Tokens(map[string]string{"builder-token": "builder", "limited-token": "limited"}), tn)
			host.Now = func() time.Time { return at }
			var lost atomic.Bool
			handler := host.Handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/v1/submissions" {
					raw, _ := io.ReadAll(r.Body)
					r.Body = io.NopCloser(bytes.NewReader(raw))
					if (bytes.Contains(raw, []byte(`"build.pmsroomtype.create"`)) || bytes.Contains(raw, []byte(`"build.erpitem.create"`))) && lost.CompareAndSwap(false, true) {
						answer := httptest.NewRecorder()
						handler.ServeHTTP(answer, r)
						if answer.Code != http.StatusOK || !bytes.Contains(answer.Body.Bytes(), []byte(`"record"`)) {
							t.Errorf("the interrupted request was not accepted: %s", answer.Body)
						}
						connection, _, _ := w.(http.Hijacker).Hijack()
						_ = connection.Close() // Lose an accepted reply, not the original request.
						return
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			initialize := func(token string) ([]byte, error) {
				t.Helper()
				command := exec.Command("python3", "../../deploy/local/initialize.py", "--package", "../../deploy/packages/"+industry+".json", "--host", server.URL, "--tenant", tn.ID, "--token-env", "PLATFORM_INITIALIZE_TEST_TOKEN", "--run-key", "initialize-v1")
				command.Env = append(os.Environ(), "PLATFORM_INITIALIZE_TEST_TOKEN="+token)
				return command.CombinedOutput()
			}
			// A builder role alone does not authorize Enterprise/Core writes.
			if output, err := initialize("limited-token"); err == nil || !strings.Contains(string(output), "the action is unavailable to this member") || len(organization.Model().Elements) != 0 {
				t.Fatalf("initialization bypassed its source owners: %v %s", err, output)
			}
			if output, err := initialize("builder-token"); err == nil || !lost.Load() || len(organization.Model().Elements) == 0 {
				t.Fatalf("the lost reply did not interrupt the accepted prefix: %v %s", err, output)
			}
			if output, err := initialize("builder-token"); err != nil {
				t.Fatalf("authorized initialization: %v %s", err, output)
			}
			wantUnits := 2
			if industry == "manufacturing" {
				wantUnits = 3
				if item := declaredRecord(t, tn, "builder", "build.erpitem", "samplepump", at); item["cost"] != float64(12) || item["currency"] != "CNY" {
					t.Fatalf("declared cost/currency: %+v", item)
				}
				if route := declaredRecord(t, tn, "builder", "build.mesrouting", "sampleassemblyroute", at); route["state"] != "published" {
					t.Fatal("the original routing release did not run")
				}
				if _, err := tn.RecordOf(memberOf(t, tn, "builder"), core.AccountType, "acc-5001", at); err != nil {
					t.Fatal("the original Core action did not create work in progress")
				}
			} else if room := declaredRecord(t, tn, "builder", "build.pmsroomtype", "sampledaily", at); room["capacity"] != float64(3) {
				t.Fatal("the original room-type creation did not run")
			}
			if got := len(organization.Model().Elements); got != wantUnits || len(organization.Model().OrgSeed().Memberships) != 0 {
				t.Fatal("initialization changed its declared organization or implicitly assigned members")
			}
			// Drain the original event work before comparing an idle restart.
			// Creating samples may enqueue normal business subscribers.
			tn.Work(at.Add(time.Minute))
			before := snapshot(tn)
			count := len(entries)
			if output, err := initialize("builder-token"); err != nil || len(entries) != count || snapshot(tn) != before {
				t.Fatalf("the same plan/key repeated business writes: %v %s", err, output)
			}
			for _, grant := range memberOf(t, tn, "limited").Grants {
				if grant.App != "build" {
					t.Fatal("the package granted an initialization role")
				}
			}
			CheckReplay(t, tn, entries, func() *Tenant { return composeDeclaredTenant(t, tn.ID, seats) })
			recovered := composeDeclaredTenant(t, tn.ID, seats)
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			recovered.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
				entries = append(entries, entry)
				return entry.Body, nil
			}
			recovered.Work(at.Add(time.Minute))
			if len(entries) != count || snapshot(recovered) != before {
				t.Fatalf("restart repeated initialization or changed its configuration: entries %d -> %d, same image %v", count, len(entries), snapshot(recovered) == before)
			}
		})
	}
}
