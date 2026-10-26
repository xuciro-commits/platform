package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"platformserver/internal/host"
	"platformserver/internal/systembuild"
)

// The candidate is a real child process of the supervisor. Its health can
// deliberately fail only with real data, after the scratch probe succeeded.
func TestMain(m *testing.M) {
	if os.Getenv("PLATFORM_UPDATER_TEST_HOST") == "1" {
		if len(os.Args) > 1 && os.Args[1] == "serve" {
			if err := serve(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		} else {
			testHost()
		}
		return
	}
	os.Exit(m.Run())
}

func testHost() {
	flags := flag.NewFlagSet("host", flag.ExitOnError)
	addr := flags.String("addr", "", "")
	release := flags.Int("release", 0, "")
	digest := flags.String("release-digest", "", "")
	data := flags.String("data", "", "")
	admins := flags.String("host-admins", "", "")
	database := flags.String("database", "", "")
	for _, name := range []string{"profile", "tenant", "admin", "tenants", "idp-key", "web", "source-root"} {
		flags.String(name, "", "")
	}
	flags.Parse(os.Args[1:])
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		status, quarantined := "ok", 0
		if fmt.Sprint(*release) == os.Getenv("PLATFORM_UPDATER_TEST_FAIL_REAL") && strings.HasSuffix(*data, "real") {
			status, quarantined = "degraded", 1
		}
		json.NewEncoder(w).Encode(map[string]any{"release": *release, "releaseDigest": *digest, "status": status, "quarantined": quarantined, "hostAdmins": *admins, "database": *database, "data": *data})
	})
	if err := http.ListenAndServe(*addr, nil); err != nil {
		os.Exit(1)
	}
}

func fixture(t *testing.T) (string, changeView, host.SystemBuildResult) {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	for name, body := range map[string]string{"README.md": "the full source", ".build/platform-server": "binary", "web/apps/workspace/dist/index.html": "workspace"} {
		path := filepath.Join(work, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source, err := systembuild.CaptureSource(work, filepath.Join(root, ".build", "system", "sources"))
	if err != nil {
		t.Fatal(err)
	}
	web, err := systembuild.DigestTree(filepath.Join(work, "web", "apps", "workspace", "dist"))
	if err != nil {
		t.Fatal(err)
	}
	change := changeView{ID: "change-1", State: "approved", Manifest: "frozen-change", SourceDigest: source.Digest}
	result := host.SystemBuildResult{OK: true, Work: work, Binary: systembuild.DigestFile(filepath.Join(work, ".build", "platform-server")), Web: web, Source: source.Root, SourceDigest: source.Digest, Manifest: map[string]string{"README.md": source.Files["README.md"].Digest}}
	return root, change, result
}

func TestBuildReceiptLossReusesRetainedArtifacts(t *testing.T) {
	root, change, result := fixture(t)
	retained, err := retainBuild(root, change, result)
	if err != nil {
		t.Fatal(err)
	}
	// The original build workspace is disposable. Publication cannot depend on it.
	if err := os.RemoveAll(result.Work); err != nil {
		t.Fatal(err)
	}
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/submissions":
			if _, err := os.Stat(filepath.Join(retained.Work, ".build", "platform-server")); err != nil {
				t.Error("built was submitted before artifacts were retained")
			}
			var body struct {
				Key string `json:"idempotencyKey"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			keys = append(keys, body.Key)
			if len(keys) == 1 {
				http.Error(w, "reply lost after acceptance", 503)
				return
			}
			fmt.Fprint(w, `{}`)
		case "/v1/system-releases":
			fmt.Fprint(w, `[{"version":1,"change":"change-1"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := client{host: server.URL, token: "test", tenant: "system", principal: "updater"}
	if err := buildFor(c, root, change); err == nil {
		t.Fatal("expected interrupted response")
	}
	if err := buildFor(c, root, change); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatalf("build identity changed: %v", keys)
	}
	if _, err := readRelease(root, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPublishedBuildResumesArtifactInstallation(t *testing.T) {
	root, change, result := fixture(t)
	if _, err := retainBuild(root, change, result); err != nil {
		t.Fatal(err)
	}
	change.State, change.Release = "published", 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/system-changes":
			json.NewEncoder(w).Encode([]changeView{change})
		case "/v1/system-activations":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := watchOnce(client{host: server.URL}, root); err != nil {
		t.Fatal(err)
	}
	if _, err := readRelease(root, 3); err != nil {
		t.Fatal(err)
	}
}

func TestActivationWindowAndExactRollbackIdentity(t *testing.T) {
	root, _, result := fixture(t)
	if err := keep(root, 4, result); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".build", "system", "current.json")
	if err := writeJSON(path, releaseState{Release: 5, Binary: "previous"}); err != nil {
		t.Fatal(err)
	}
	activation := activationView{ID: "0005-r", Release: 4, From: 5, Result: "requested", At: time.Now(), Window: 30}
	if err := stageActivation(client{}, root, activation); err == nil {
		t.Fatal("staged inside cancellation window")
	}
	state, _ := readState(path)
	if state.Release != 5 {
		t.Fatal("changed current inside cancellation window")
	}
	activation.At = activation.At.Add(-time.Minute)
	if err := stageActivation(client{}, root, activation); err != nil {
		t.Fatal(err)
	}
	state, err := readState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Activation != "0005-r" || state.Release != 4 || state.Previous.Release != 5 {
		t.Fatalf("lost rollback identity: %+v", state)
	}
}

func newProcessSupervisor(t *testing.T, ownerURL string) (*supervisor, releaseState, releaseState) {
	t.Helper()
	t.Setenv("PLATFORM_UPDATER_TEST_HOST", "1")
	root := t.TempDir()
	binary := filepath.Join(root, "host")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyFile(executable, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	previous := releaseState{Release: 1, Binary: binary, Digest: systembuild.DigestFile(binary)}
	next := releaseState{Release: 2, Binary: binary, Digest: previous.Digest, Activation: "arbitrary-rollback-id", Previous: &previous}
	s := &supervisor{root: root, current: filepath.Join(root, "current.json"), state: previous, args: []string{"-addr", addr}, data: filepath.Join(root, "real"), healthURL: "http://" + addr + "/healthz", health: 600 * time.Millisecond, drain: time.Second, c: client{host: ownerURL, token: "test", tenant: "system", principal: "updater"}}
	if err := writeJSON(s.current, next); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.stop(time.Second) })
	return s, previous, next
}

type finishedRequest struct {
	Key    string `json:"idempotencyKey"`
	Target struct {
		ID string `json:"id"`
	} `json:"target"`
	Payload string `json:"payload"`
}

func activationOwner(t *testing.T, loseFirst bool) (*httptest.Server, *[]finishedRequest) {
	t.Helper()
	requests := []finishedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			fmt.Fprint(w, `{"release":0}`)
			return
		}
		if r.URL.Path == "/v1/system-activations" {
			json.NewEncoder(w).Encode([]activationView{{ID: "arbitrary-rollback-id", Release: 2, From: 1, Result: "requested", At: time.Now().Add(-time.Minute)}})
			return
		}
		if r.URL.Path != "/v1/submissions" {
			http.NotFound(w, r)
			return
		}
		var request finishedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request)
		if loseFirst && len(requests) == 1 {
			http.Error(w, "accepted, reply lost", 503)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	return server, &requests
}

func resultOf(t *testing.T, request finishedRequest) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body.Result
}

func TestCandidateProbeFailureRestoresSelectionAndReportsExactActivation(t *testing.T) {
	owner, requests := activationOwner(t, false)
	defer owner.Close()
	s, previous, next := newProcessSupervisor(t, owner.URL)
	next.Digest = "invalid"
	if err := s.replace(previous, next); err != nil {
		t.Fatal(err)
	}
	state, err := readState(s.current)
	if err != nil {
		t.Fatal(err)
	}
	if state.Release != previous.Release || s.state.Release != previous.Release || state.Activation != "" {
		t.Fatalf("failed probe poisoned current: %+v", state)
	}
	if len(*requests) != 1 || (*requests)[0].Target.ID != next.Activation || resultOf(t, (*requests)[0]) != "aborted" {
		t.Fatalf("wrong failure report: %+v", *requests)
	}
}

func TestRealDataStartupFailureRollsBackHostAndSelection(t *testing.T) {
	t.Setenv("PLATFORM_UPDATER_TEST_FAIL_REAL", "2")
	owner, requests := activationOwner(t, false)
	defer owner.Close()
	s, previous, next := newProcessSupervisor(t, owner.URL)
	if err := s.start(previous); err != nil {
		t.Fatal(err)
	}
	if err := s.replace(previous, next); err != nil {
		t.Fatal(err)
	}
	state, err := readState(s.current)
	if err != nil {
		t.Fatal(err)
	}
	if state.Release != 1 || s.state.Release != 1 {
		t.Fatalf("rollback selection: %+v", state)
	}
	if err := waitHealth(s.healthURL, 1, previous.Digest, time.Second); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 || resultOf(t, (*requests)[0]) != "rolled-back" {
		t.Fatalf("wrong rollback outcome: %+v", *requests)
	}
}

func TestSuccessfulSwapLostReportResumesAfterSupervisorRestart(t *testing.T) {
	owner, requests := activationOwner(t, true)
	defer owner.Close()
	s, previous, next := newProcessSupervisor(t, owner.URL)
	if err := s.start(previous); err != nil {
		t.Fatal(err)
	}
	if err := s.replace(previous, next); err == nil {
		t.Fatal("expected lost outcome response")
	}
	state, err := readState(s.current)
	if err != nil {
		t.Fatal(err)
	}
	if state.Release != 2 || state.Result != "active" || state.Activation != next.Activation {
		t.Fatalf("outcome not durable: %+v", state)
	}
	s.stop(time.Second)
	// A new updater instance has only current.json, no in-memory retry maps.
	resumed := &supervisor{root: s.root, current: s.current, state: state, args: s.args, data: s.data, healthURL: s.healthURL, health: s.health, drain: s.drain, c: s.c}
	defer resumed.stop(time.Second)
	if err := resumed.start(state); err != nil {
		t.Fatal(err)
	}
	if err := resumed.reload(); err != nil {
		t.Fatal(err)
	}
	state, err = readState(s.current)
	if err != nil {
		t.Fatal(err)
	}
	if state.Activation != "" || state.Release != 2 || state.Previous.Release != 1 {
		t.Fatalf("reconciled state: %+v", state)
	}
	if len(*requests) != 2 || (*requests)[0].Key != (*requests)[1].Key || (*requests)[0].Payload != (*requests)[1].Payload || (*requests)[1].Target.ID != next.Activation {
		t.Fatalf("outcome retry changed identity: %+v", *requests)
	}
}

func TestHealthDeadlineIncludesHungResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	started := time.Now()
	if err := waitHealth(server.URL, 1, "", 30*time.Millisecond); err == nil {
		t.Fatal("expected health timeout")
	}
	if time.Since(started) > time.Second {
		t.Fatal("health deadline did not bound the request")
	}
}

func TestExportUsesCompleteSealedSource(t *testing.T) {
	root, _, result := fixture(t)
	if err := keep(root, 1, result); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(result.Work); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "export")
	if err := export([]string{"-root", root, "-release", "1", "-out", out}); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(out, "README.md")); err != nil || string(raw) != "the full source" {
		t.Fatalf("complete source missing: %q %v", raw, err)
	}
	if err := os.WriteFile(filepath.Join(result.Source, "files", "README.md"), []byte("tampered untouched file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := export([]string{"-root", root, "-release", "1", "-out", out}); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("accepted altered sealed source: %v", err)
	}
}

func TestCancellationDuringProbeKeepsRunningRelease(t *testing.T) {
	reads, reports := 0, 0
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/system-activations" {
			reads++
			result := "requested"
			if reads > 1 {
				result = "aborted"
			}
			json.NewEncoder(w).Encode([]activationView{{ID: "arbitrary-rollback-id", Release: 2, From: 1, Result: result, At: time.Now().Add(-time.Minute)}})
			return
		}
		reports++
		http.Error(w, "cancelled activation must not be finished again", 409)
	}))
	defer owner.Close()
	s, previous, next := newProcessSupervisor(t, owner.URL)
	if err := s.start(previous); err != nil {
		t.Fatal(err)
	}
	if err := s.replace(previous, next); err != nil {
		t.Fatal(err)
	}
	state, err := readState(s.current)
	if err != nil {
		t.Fatal(err)
	}
	if state.Release != 1 || state.Activation != "" || reports != 0 {
		t.Fatalf("cancelled candidate changed current: %+v, reports %d", state, reports)
	}
	if err := waitHealth(s.healthURL, 1, previous.Digest, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestPublishedWatchDoesNotResurrectPrunedBuilds(t *testing.T) {
	root, change, result := fixture(t)
	changes := []changeView{}
	for version := 1; version <= 7; version++ {
		change.ID, change.State, change.Release = fmt.Sprintf("change-%d", version), "published", version
		retained, err := retainBuild(root, change, result)
		if err != nil {
			t.Fatal(err)
		}
		if err := keep(root, version, retained); err != nil {
			t.Fatal(err)
		}
		changes = append(changes, change)
	}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/system-changes":
			json.NewEncoder(w).Encode(changes)
		case "/v1/system-activations":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer owner.Close()
	if err := watchOnce(client{host: owner.URL}, root); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{1, 2} {
		if _, err := os.Stat(filepath.Join(root, ".build", "system", "releases", fmt.Sprintf("%04d.json", version))); !os.IsNotExist(err) {
			t.Fatalf("pruned release %d resurrected: %v", version, err)
		}
	}
}

func TestCompletedReleaseRestartFailureCreatesNewRollback(t *testing.T) {
	for _, pendingActive := range []bool{false, true} {
		t.Run(fmt.Sprint("pending-active=", pendingActive), func(t *testing.T) {
			t.Setenv("PLATFORM_UPDATER_TEST_FAIL_REAL", "2")
			var requests []finishedRequest
			var rollbackKeys []string
			rollbackRequested := false
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/system-releases" {
					fmt.Fprint(w, `[{"version":7,"state":"active"}]`)
					return
				}
				if r.URL.Path == "/v1/system-activations" {
					if !rollbackRequested {
						fmt.Fprint(w, `[]`)
						return
					}
					json.NewEncoder(w).Encode([]activationView{{ID: "fresh-authoritative-rollback", Release: 1, From: 7, Result: "requested", At: time.Now().Add(-time.Minute)}})
					return
				}
				if r.URL.Path != "/v1/submissions" {
					http.NotFound(w, r)
					return
				}
				var body struct {
					finishedRequest
					Schema struct {
						Name string `json:"name"`
					} `json:"schema"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Schema.Name == "system.release.rollback" {
					if body.Target.ID != "0007" {
						t.Errorf("rollback skipped the global definitions release: %s", body.Target.ID)
					}
					rollbackKeys = append(rollbackKeys, body.Key)
					rollbackRequested = true
					raw, _ := base64.StdEncoding.DecodeString(body.Payload)
					if string(raw) != `{"targetVersion":1,"window":0}` {
						t.Errorf("automatic rollback must explicitly be immediate: %s", raw)
					}
					if len(rollbackKeys) == 1 {
						http.Error(w, "accepted but reply lost", 503)
						return
					}
				} else {
					requests = append(requests, body.finishedRequest)
				}
				fmt.Fprint(w, `{}`)
			}))
			defer owner.Close()
			s, previous, desired := newProcessSupervisor(t, owner.URL)
			desired.Previous = &previous
			if pendingActive {
				desired.Result, desired.Evidence = "active", "original durable active evidence"
			} else {
				desired.Activation = ""
			}
			if err := writeJSON(s.current, desired); err != nil {
				t.Fatal(err)
			}
			if err := s.startInitial(desired); err != nil {
				t.Fatal(err)
			}
			state, err := readState(s.current)
			if err != nil {
				t.Fatal(err)
			}
			if state.Release != 1 || state.PendingRollback == nil {
				t.Fatalf("startup did not persist recovery: %+v", state)
			}
			if pendingActive && (state.Result != "active" || state.Evidence != desired.Evidence) {
				t.Fatal("recovery rewrote the original outcome")
			}
			if err := s.reload(); err == nil {
				t.Fatal("expected rollback response loss")
			}
			state, err = readState(s.current)
			if err != nil {
				t.Fatal(err)
			}
			if state.PendingRollback == nil {
				t.Fatal("lost response discarded the rollback request identity")
			}
			s.stop(time.Second)
			resumed := &supervisor{root: s.root, current: s.current, args: s.args, data: s.data, healthURL: s.healthURL, health: s.health, drain: s.drain, c: s.c}
			defer resumed.stop(time.Second)
			if err := resumed.startInitial(state); err != nil {
				t.Fatal(err)
			}
			if err := resumed.reload(); err != nil {
				t.Fatal(err)
			}
			state, err = readState(s.current)
			if err != nil {
				t.Fatal(err)
			}
			if state.Release != 1 || state.Activation != "" || state.PendingRollback != nil {
				t.Fatalf("recovery not reconciled: %+v", state)
			}
			if len(rollbackKeys) != 2 || rollbackKeys[0] != rollbackKeys[1] {
				t.Fatalf("rollback request identity changed: %v", rollbackKeys)
			}
			expected := 1
			if pendingActive {
				expected++
			}
			if len(requests) != expected {
				t.Fatalf("unexpected finished requests: %+v", requests)
			}
			last := requests[len(requests)-1]
			if last.Target.ID != "fresh-authoritative-rollback" || resultOf(t, last) != "active" {
				t.Fatalf("did not use the new rollback activation: %+v", last)
			}
			if pendingActive {
				raw, _ := base64.StdEncoding.DecodeString(requests[0].Payload)
				var outcome map[string]string
				json.Unmarshal(raw, &outcome)
				if requests[0].Target.ID != desired.Activation || outcome["result"] != "active" || outcome["evidence"] != desired.Evidence {
					t.Fatal("changed the original terminal report")
				}
			}
		})
	}
}

func TestDefiniteActivationRefusalRestoresPriorHost(t *testing.T) {
	var requests []finishedRequest
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/system-activations" {
			json.NewEncoder(w).Encode([]activationView{{ID: "arbitrary-rollback-id", Release: 2, From: 1, Result: "requested", At: time.Now().Add(-time.Minute)}})
			return
		}
		var request finishedRequest
		json.NewDecoder(r.Body).Decode(&request)
		requests = append(requests, request)
		if resultOf(t, request) == "active" {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"error":{"code":"ERROR_CODE_CONFLICT","message":"declarative candidate was refused"}}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer owner.Close()
	s, previous, next := newProcessSupervisor(t, owner.URL)
	if err := s.start(previous); err != nil {
		t.Fatal(err)
	}
	if err := s.replace(previous, next); err != nil {
		t.Fatal(err)
	}
	if err := waitHealth(s.healthURL, 1, previous.Digest, time.Second); err != nil {
		t.Fatal(err)
	}
	state, err := readState(s.current)
	if err != nil {
		t.Fatal(err)
	}
	if state.Release != 1 || state.Activation != "" {
		t.Fatalf("refused activation was left running: %+v", state)
	}
	if len(requests) != 2 || resultOf(t, requests[0]) != "active" || resultOf(t, requests[1]) != "rolled-back" || requests[0].Key == requests[1].Key || requests[1].Target.ID != next.Activation {
		t.Fatalf("failure changed a request payload under its old key: %+v", requests)
	}
}

func TestDefinitionPredecessorReusesMatchingCodeArtifacts(t *testing.T) {
	root, _, result := fixture(t)
	if err := keep(root, 1, result); err != nil {
		t.Fatal(err)
	}
	if err := keep(root, 3, result); err != nil {
		t.Fatal(err)
	}
	previous, err := readRelease(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(root, ".build", "system", "current.json")
	if err := writeJSON(current, previous); err != nil {
		t.Fatal(err)
	}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/system-releases" {
			json.NewEncoder(w).Encode([]map[string]any{{"version": 2, "sourceDigest": result.SourceDigest, "kind": "definitions"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer owner.Close()
	activation := activationView{ID: "code-after-hot-definitions", Release: 3, From: 2, Result: "requested", At: time.Now().Add(-time.Minute)}
	if err := stageActivation(client{host: owner.URL}, root, activation); err != nil {
		t.Fatal(err)
	}
	state, err := readState(current)
	if err != nil {
		t.Fatal(err)
	}
	if state.Release != 3 || state.Previous == nil || state.Previous.Release != 2 || state.Previous.SourceDigest != previous.SourceDigest || state.Serving != previous.Release {
		t.Fatalf("logical predecessor was skipped: %+v", state)
	}
	alias, err := readRelease(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if alias.Digest != previous.Digest || alias.SourceDigest != previous.SourceDigest {
		t.Fatal("definition predecessor did not reuse the same code")
	}
	pruneReleases(root, 0)
	if _, err := os.Stat(previous.Binary); err != nil {
		t.Fatal("pruned the still-running physical predecessor during a staged activation:", err)
	}
	// Each ordinal owns its physical artifacts; pruning its source ordinal must
	// not delete the installed rollback target.
	if err := os.Remove(previous.Binary); err != nil {
		t.Fatal(err)
	}
	if err := validateRelease(alias); err != nil {
		t.Fatal(err)
	}
}

func TestFirstReleaseStartsFromRequestedActivation(t *testing.T) {
	owner, requests := activationOwner(t, false)
	defer owner.Close()
	s, previous, _ := newProcessSupervisor(t, owner.URL)
	dir := filepath.Join(s.root, ".build", "system")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(previous.Binary, filepath.Join(dir, "bin", "platform-server-0002"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "releases", "0002.json"), releaseManifest{Release: 2, Binary: previous.Digest}); err != nil {
		t.Fatal(err)
	}
	// The alias has no workspace in this historical fixture.
	if err := os.MkdirAll(filepath.Join(dir, "web", "0002"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.current); err != nil {
		t.Fatal(err)
	}
	s.current = filepath.Join(dir, "current.json")
	activation := activationView{ID: "arbitrary-rollback-id", Release: 2, From: 0, Result: "requested", At: time.Now().Add(-time.Minute)}
	if err := stageActivation(s.c, s.root, activation); err != nil {
		t.Fatal(err)
	}
	first, err := readState(s.current)
	if err != nil {
		t.Fatal(err)
	}
	if first.Previous != nil || first.Activation != activation.ID {
		t.Fatalf("bootstrap invented a predecessor: %+v", first)
	}
	if err := s.startInitial(first); err != nil {
		t.Fatal(err)
	}
	if err := s.reload(); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 || (*requests)[0].Target.ID != activation.ID || resultOf(t, (*requests)[0]) != "active" {
		t.Fatalf("first activation not finished: %+v", *requests)
	}
}

func TestFirstCodeReleaseAfterInitialHotDefinitions(t *testing.T) {
	for _, scenario := range []string{"initial-definitions", "code-predecessor", "wrong-source", "already-versioned"} {
		t.Run(scenario, func(t *testing.T) {
			root, change, result := fixture(t)
			if err := keep(root, 3, result); err != nil {
				t.Fatal(err)
			}
			change.Base = 2
			kind, digest, processVersion := "definitions", result.SourceDigest, 0
			if scenario == "code-predecessor" {
				kind = "code"
			}
			if scenario == "wrong-source" {
				digest = "different-initial-source"
			}
			if scenario == "already-versioned" {
				processVersion = 1
			}
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/healthz":
					json.NewEncoder(w).Encode(map[string]any{"release": processVersion})
				case "/v1/system-changes":
					json.NewEncoder(w).Encode([]changeView{change})
				case "/v1/system-releases":
					json.NewEncoder(w).Encode([]map[string]any{
						{"version": 1, "previous": 0, "kind": kind, "sourceDigest": digest},
						{"version": 2, "previous": 1, "kind": "definitions", "sourceDigest": result.SourceDigest},
						{"version": 3, "previous": 2, "kind": "code", "change": change.ID, "sourceDigest": result.SourceDigest},
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer owner.Close()
			activation := activationView{ID: "initial-code-after-definitions", Release: 3, From: 2, Result: "requested", At: time.Now().Add(-time.Minute)}
			err := stageActivation(client{host: owner.URL}, root, activation)
			path := filepath.Join(root, ".build", "system", "current.json")
			if scenario != "initial-definitions" {
				if err == nil {
					t.Fatal("treated a lost current or unrelated source as first supervision")
				}
				if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
					t.Fatal("refused first supervision wrote current.json")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err := readState(path)
			if err != nil {
				t.Fatal(err)
			}
			if state.Release != 3 || state.Activation != activation.ID || state.Previous != nil {
				t.Fatalf("first supervision invented a runnable seed: %+v", state)
			}
		})
	}
}

func TestServeForwardsOriginalHostArguments(t *testing.T) {
	s, previous, _ := newProcessSupervisor(t, "")
	path := filepath.Join(s.root, ".build", "system", "current.json")
	if err := writeJSON(path, previous); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(previous.Binary, "serve", "-root", s.root, "-addr", s.args[1], "-data", s.data, "-health-timeout", "2s", "-drain", "1s", "--", "-host-admins", "operator-1", "-database", "test-database", "-release", "999", "-data", "must-be-overridden")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(syscall.SIGTERM); _ = cmd.Wait() }()
	if err := waitHealth(s.healthURL, previous.Release, previous.Digest, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(s.healthURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		Admins   string `json:"hostAdmins"`
		Database string `json:"database"`
		Data     string `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Admins != "operator-1" || body.Database != "test-database" || body.Data != s.data {
		t.Fatalf("lost deployment arguments or supervised override: %+v", body)
	}
}
