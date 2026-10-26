package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"platformserver/driver"
	"platformserver/drivers/simulator"
	"platformserver/platform"
)

type localClient struct {
	handler          driver.Handler
	mu               sync.Mutex
	closed           bool
	starts, executes int
	beforeExecute    func()
}

func (c *localClient) Available() bool                 { c.mu.Lock(); defer c.mu.Unlock(); return !c.closed }
func (c *localClient) Close()                          { c.mu.Lock(); c.closed = true; c.mu.Unlock() }
func (c *localClient) CloseWait(context.Context) error { c.Close(); return nil }
func (c *localClient) Call(ctx context.Context, method string, input, output any) error {
	c.mu.Lock()
	if method == "start" {
		c.starts++
	}
	if method == "execute" {
		c.executes++
	}
	c.mu.Unlock()
	if method == "execute" && c.beforeExecute != nil {
		c.beforeExecute()
	}
	raw, _ := json.Marshal(input)
	answer, err := c.handler.Handle(ctx, method, raw)
	if err != nil {
		return err
	}
	if output != nil {
		raw, err = json.Marshal(answer)
		if err == nil {
			err = json.Unmarshal(raw, output)
		}
	}
	return err
}

func gatewayFixture(t *testing.T) (Config, platform.DriverBinding) {
	t.Helper()
	directory := t.TempDir()
	binary := []byte("trusted test process")
	sum := sha256.Sum256(binary)
	digest := hex.EncodeToString(sum[:])
	if err := os.Mkdir(filepath.Join(directory, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "artifacts", digest), binary, 0700); err != nil {
		t.Fatal(err)
	}
	binding := platform.DriverBinding{Package: "test.simulator", Version: "1.0.0", Digest: digest, Profile: simulator.Profile, Configuration: platform.DriverConfiguration{Devices: []platform.DriverDevice{{ID: "unit", Title: "Unit"}}, Points: []platform.DriverPoint{{ID: "temperature", Device: "unit", Address: "58.4", Type: "decimal", Unit: "°C", IntervalMS: 1000}}, Commands: []platform.DriverCommand{{ID: "set", Point: "temperature", Min: "0", Max: "100"}}}}
	config := Config{Directory: directory, StateDir: t.TempDir(), Token: "test-token", Resolve: func(b platform.DriverBinding) (platform.DriverRuntime, error) {
		if b.Package != binding.Package || b.Version != binding.Version || b.Digest != digest {
			return platform.DriverRuntime{}, fmt.Errorf("unknown package")
		}
		runtime := simulator.Runtime()
		runtime.Targets = []platform.DriverTarget{{OS: goruntime.GOOS, Arch: goruntime.GOARCH, Artifact: digest}}
		return runtime, nil
	}}
	return config, binding
}

func gatewayRequest(t *testing.T, s *Server, binding platform.DriverBinding, operation string, input any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			t.Fatal(err)
		}
	}
	method := http.MethodPost
	if operation == "check" || operation == "source" || operation == "source?commands=1" || operation == "lifecycle" {
		method = http.MethodGet
	}
	r := httptest.NewRequest(method, "/bindings/"+binding.ConnectionID("heating")+"/revisions/"+binding.Revision()+"/"+operation, &body)
	r.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func newLocalGateway(t *testing.T, config Config) (*Server, *localClient) {
	t.Helper()
	s, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	client := &localClient{handler: simulator.New()}
	s.start = func(string) (rpcClient, error) { return client, nil }
	t.Cleanup(s.Close)
	return s, client
}

func TestProtocolGatewayAttemptRecoveryAndRevision(t *testing.T) {
	config, binding := gatewayFixture(t)
	binding.Configuration.Scenario = "unknown"
	s, client := newLocalGateway(t, config)
	if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	input := platform.DriverExecute{Dispatch: platform.DriverDispatchIdentity{RunID: "run", AttemptID: "attempt-1", BusinessKey: "business-1", Deadline: time.Now().Add(time.Hour).UTC(), BindingRevision: binding.Revision(), PackageDigest: binding.Digest}, Command: platform.DriverCommandValue{Name: "set", Value: "64.2"}}
	client.beforeExecute = func() {
		raw, err := os.ReadFile(s.receiptPath(receiptID(input.Dispatch)))
		if err != nil || !bytes.Contains(raw, []byte(`"state":"unknown"`)) {
			t.Error("attempt was not durable before execute", err)
		}
	}
	w := gatewayRequest(t, s, binding, "execute", input)
	var answer platform.DriverExecution
	_ = json.Unmarshal(w.Body.Bytes(), &answer)
	if w.Code != 200 || answer.State != "unknown" || client.executes != 1 {
		t.Fatalf("lost answer: %d %s calls=%d", w.Code, w.Body.String(), client.executes)
	}
	s.Close()
	reopened, nextClient := newLocalGateway(t, config)
	if w = gatewayRequest(t, reopened, binding, "check", nil); w.Code != 200 {
		t.Fatal("stored revision did not recover", w.Body.String())
	}
	if w = gatewayRequest(t, reopened, binding, "configure?readOnly=1", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = gatewayRequest(t, reopened, binding, "execute", input)
	if w.Code != 200 || nextClient.executes != 0 || !bytes.Contains(w.Body.Bytes(), []byte(`"state":"unknown"`)) {
		t.Fatalf("recovery sent the attempt again: %d %s calls=%d", w.Code, w.Body.String(), nextClient.executes)
	}
	changed := input
	changed.Command.Value = "65"
	if w = gatewayRequest(t, reopened, binding, "execute", changed); w.Code != 409 {
		t.Fatalf("changed retry: %d %s", w.Code, w.Body.String())
	}
	current := binding
	current.Configuration.Scenario = "normal"
	if w = gatewayRequest(t, reopened, current, "configure", current); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = gatewayRequest(t, reopened, binding, "configure?readOnly=1", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	input.Dispatch.AttemptID = "attempt-2"
	if w = gatewayRequest(t, reopened, binding, "execute", input); w.Code != 409 {
		t.Fatalf("old revision accepted a new write: %d %s", w.Code, w.Body.String())
	}
	stored, _ := newLocalGateway(t, config)
	if w = gatewayRequest(t, stored, binding, "execute", input); w.Code != 409 {
		t.Fatalf("restart made old revision writable: %d %s", w.Code, w.Body.String())
	}
	input.Dispatch.BindingRevision = current.Revision()
	w = gatewayRequest(t, reopened, current, "execute", input)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"state":"accepted"`)) || nextClient.executes != 1 {
		t.Fatalf("new revision: %d %s", w.Code, w.Body.String())
	}
	w = gatewayRequest(t, reopened, current, "source?commands=1", nil)
	for _, field := range []string{`"id":"business-1"`, `"attemptID":"attempt-2"`, `"runID":"run"`, `"at":`} {
		if !bytes.Contains(w.Body.Bytes(), []byte(field)) {
			t.Fatalf("command source lacks %s: %s", field, w.Body.String())
		}
	}
}

func TestPreviewReleasesOnlyItsSharedProcessReference(t *testing.T) {
	config, binding := gatewayFixture(t)
	s, client := newLocalGateway(t, config)
	if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	raw, _ := json.Marshal(binding)
	r := httptest.NewRequest(http.MethodPost, "/bindings/preview-check/revisions/"+binding.Revision()+"/configure?readOnly=1", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	live := s.sessions[sessionKey("preview-check", binding.Revision())]
	s.discardPreview("preview-check", binding.Revision(), live)
	if !client.Available() || len(s.processes) != 1 || s.processRefs[client] != 1 {
		t.Fatal("preview closed another connection's process")
	}
	if w = gatewayRequest(t, s, binding, "check", nil); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.CloseWait(ctx); err != nil {
		t.Fatal(err)
	}
	if client.Available() || len(s.processes) != 0 || len(s.processRefs) != 0 {
		t.Fatal("last session did not release its process")
	}
	if w = gatewayRequest(t, s, binding, "check", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatal("closed gateway accepted work", w.Code)
	}
}

func TestReleasePreservesSharedPeerAndDoesNotReopenAfterRestart(t *testing.T) {
	config, binding := gatewayFixture(t)
	s, client := newLocalGateway(t, config)
	if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.configure(ctx, "peer", binding.Revision(), binding, false); err != nil {
		t.Fatal(err)
	}
	w := gatewayRequest(t, s, binding, "release", struct{}{})
	var status platform.DriverLifecycle
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if w.Code != 200 || !status.Released || status.State != "closed" || !client.Available() || s.processRefs[client] != 1 {
		t.Fatal("release changed peer resources", w.Code, w.Body.String())
	}
	if w = gatewayRequest(t, s, binding, "source", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatal("released connection restarted sampling")
	}
	s.Close()
	reopened, _ := newLocalGateway(t, config)
	if w = gatewayRequest(t, reopened, binding, "check", nil); w.Code != http.StatusServiceUnavailable || len(reopened.processes) != 0 {
		t.Fatal("restart reopened released resource", w.Code, w.Body.String())
	}
	if w = gatewayRequest(t, reopened, binding, "release", struct{}{}); w.Code != 200 {
		t.Fatal("release retry", w.Body.String())
	}
}

func TestUnknownAttemptBlocksReleaseAcrossRestartWithoutResending(t *testing.T) {
	config, binding := gatewayFixture(t)
	binding.Configuration.Scenario = "unknown"
	s, client := newLocalGateway(t, config)
	if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	input := platform.DriverExecute{Dispatch: platform.DriverDispatchIdentity{RunID: "run", AttemptID: "attempt", BusinessKey: "business", Deadline: time.Now().Add(time.Hour), BindingRevision: binding.Revision(), PackageDigest: binding.Digest}, Command: platform.DriverCommandValue{Name: "set", Value: "64.2"}}
	if w := gatewayRequest(t, s, binding, "execute", input); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := gatewayRequest(t, s, binding, "release", struct{}{})
	var status platform.DriverLifecycle
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if w.Code != 409 || len(status.Blockers) != 1 || status.State != "blocked" || !client.Available() {
		t.Fatal("unknown command lost its readback scope", w.Code, w.Body.String())
	}
	if w = gatewayRequest(t, s, binding, "execute", input); w.Code != 200 || client.executes != 1 {
		t.Fatal("release resent the original command")
	}
	s.Close()
	reopened, next := newLocalGateway(t, config)
	w = gatewayRequest(t, reopened, binding, "lifecycle", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if status.State != "blocked" || len(status.Blockers) != 1 || next.executes != 0 || len(reopened.processes) != 0 {
		t.Fatal("restart hid a retained unknown attempt", w.Body.String())
	}
}

func TestProtocolGatewayCheckAndObservation(t *testing.T) {
	config, binding := gatewayFixture(t)
	s, client := newLocalGateway(t, config)
	if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := gatewayRequest(t, s, binding, "check", nil)
	var check platform.DriverReadResult
	_ = json.Unmarshal(w.Body.Bytes(), &check)
	if w.Code != 200 || len(check.Samples) != 1 || check.Samples[0].Value != "58.4" || check.Samples[0].Source.Revision != binding.Revision() || client.starts != 0 {
		t.Fatalf("read-only preview: %d %s starts=%d", w.Code, w.Body.String(), client.starts)
	}
	until := time.Now().UTC().Truncate(time.Second)
	w = gatewayRequest(t, s, binding, "observation", map[string]any{"device": "unit", "signal": "temperature", "since": until.Add(-3 * time.Second), "until": until, "mode": "points", "limit": 2})
	var result platform.DriverReadResult
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.Window == nil || result.Window.Count != 3 || result.Window.Mean != "58.4" || len(result.Points) != 2 || result.Coverage == nil || !result.Coverage.Truncated || client.starts != 1 {
		t.Fatalf("deterministic observation: %d %s", w.Code, w.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/bindings/"+binding.ConnectionID("heating")+"/revisions/"+binding.Revision()+"/source", nil)
	unauthorized := httptest.NewRecorder()
	s.ServeHTTP(unauthorized, request)
	if unauthorized.Code != 401 {
		t.Fatalf("unauthorized gateway: %d", unauthorized.Code)
	}
	previewURL := "/bindings/preview-check/revisions/" + binding.Revision() + "/"
	for _, operation := range []string{"configure", "check"} {
		var body bytes.Buffer
		method := http.MethodGet
		if operation == "configure" {
			method = http.MethodPost
			_ = json.NewEncoder(&body).Encode(binding)
		}
		request := httptest.NewRequest(method, previewURL+operation, &body)
		request.Header.Set("Authorization", "Bearer test-token")
		response := httptest.NewRecorder()
		s.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("preview %s: %d %s", operation, response.Code, response.Body.String())
		}
	}
	if s.sessions[sessionKey("preview-check", binding.Revision())] != nil || s.active["preview-check"] != "" {
		t.Fatal("preview left a session or writable revision")
	}
	if _, err := os.Stat(filepath.Join(config.StateDir, "bindings", bindingFile("preview-check"))); !os.IsNotExist(err) {
		t.Fatal("preview configuration was persisted", err)
	}
}

func TestProtocolGatewayTenantScopesPreservePeersWithIdenticalNamesAndAttemptKeys(t *testing.T) {
	config, original := gatewayFixture(t)
	s, client := newLocalGateway(t, config)
	a, b := original, original
	a.Scope, b.Scope = platform.DriverScope("tenant-a"), platform.DriverScope("tenant-b")
	for _, binding := range []platform.DriverBinding{a, b} {
		if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		input := platform.DriverExecute{Dispatch: platform.DriverDispatchIdentity{Scope: binding.Scope, RunID: "same-run", AttemptID: "same-attempt", BusinessKey: "same-business", Deadline: time.Now().Add(time.Hour), BindingRevision: binding.Revision(), PackageDigest: binding.Digest}, Command: platform.DriverCommandValue{Name: "set", Value: "64.2"}}
		if w := gatewayRequest(t, s, binding, "execute", input); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if client.executes != 2 || len(s.processes) != 1 {
		t.Fatal("tenant scopes collided or duplicated the shared provider", client.executes, len(s.processes))
	}
	if w := gatewayRequest(t, s, a, "release", a); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := gatewayRequest(t, s, b, "check", nil); w.Code != 200 || !client.Available() {
		t.Fatal("releasing tenant A changed tenant B", w.Code, w.Body.String())
	}
	s.Close()
	restored, _ := newLocalGateway(t, config)
	if w := gatewayRequest(t, restored, b, "check", nil); w.Code != 200 {
		t.Fatal("tenant B did not recover", w.Code, w.Body.String())
	}
	if w := gatewayRequest(t, restored, a, "configure", a); w.Code != 200 {
		t.Fatal("explicit reinstall could not reactivate its own scope", w.Code, w.Body.String())
	}
}

func TestFailedPreviewCleansItsHandleAndKeepsSharedPeer(t *testing.T) {
	config, binding := gatewayFixture(t)
	s, client := newLocalGateway(t, config)
	if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	original := client.handler
	client.handler = driver.HandlerFunc(func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		var open platform.DriverOpen
		_ = json.Unmarshal(raw, &open)
		if method == "open" && strings.HasPrefix(open.Connection, "preview-") {
			// The device really opens, then its caller loses the answer.
			if _, err := original.Handle(ctx, method, raw); err != nil {
				return nil, err
			}
			return nil, context.DeadlineExceeded
		}
		return original.Handle(ctx, method, raw)
	})
	if _, err := s.configure(context.Background(), "preview-failed", binding.Revision(), binding, true); err == nil {
		t.Fatal("failed preview reported success")
	}
	client.handler = original
	if !client.Available() || len(s.processes) != 1 {
		t.Fatal("preview failure terminated shared provider")
	}
	if _, found := s.sessions[sessionKey("preview-failed", binding.Revision())]; found {
		t.Fatal("failed preview retained its registration")
	}
	if w := gatewayRequest(t, s, binding, "check", nil); w.Code != 200 {
		t.Fatal("peer failed after preview error", w.Code, w.Body.String())
	}
}

func TestCleanupFailureRetainsResponsibilityAndRetriesOnlyItsScope(t *testing.T) {
	config, binding := gatewayFixture(t)
	s, client := newLocalGateway(t, config)
	if w := gatewayRequest(t, s, binding, "configure", binding); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	original := client.handler
	fail := true
	client.handler = driver.HandlerFunc(func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "close" && fail {
			return nil, fmt.Errorf("device close is unavailable")
		}
		return original.Handle(ctx, method, raw)
	})
	if w := gatewayRequest(t, s, binding, "release", binding); w.Code == 200 {
		t.Fatal("failed cleanup reported released", w.Body.String())
	}
	if w := gatewayRequest(t, s, binding, "lifecycle", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "cleanup-failed") || !strings.Contains(w.Body.String(), "device close") {
		t.Fatal("cleanup responsibility disappeared", w.Body.String())
	}
	fail = false
	if w := gatewayRequest(t, s, binding, "release", binding); w.Code != 200 || !strings.Contains(w.Body.String(), `"released":true`) {
		t.Fatal("cleanup retry failed", w.Code, w.Body.String())
	}
	if client.Available() || len(s.processes) != 0 {
		t.Fatal("last process survived cleanup")
	}
}
