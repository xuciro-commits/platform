// Package gateway runs trusted versioned protocol processes. It stores technical
// command receipts, not business state or a second outbound queue.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"platformserver/driver"
	"platformserver/platform"
)

type Config struct {
	Directory    string
	StateDir     string
	Token        string
	LegacyTenant string // explicit owner of old, unscoped bindings; never inferred from a request
	Resolve      func(platform.DriverBinding) (platform.DriverRuntime, error)
	// Samples is an external raw-sample sink. The business host does not receive
	// this stream, and this gateway retains only the latest value of each point.
	Samples io.Writer
}

type rpcClient interface {
	Call(context.Context, string, any, any) error
	Available() bool
	Close()
	CloseWait(context.Context) error
}

type Server struct {
	config      Config
	mu          sync.Mutex
	processes   map[string]rpcClient
	sessions    map[string]*session
	active      map[string]string
	receipts    map[string]receipt
	start       func(string) (rpcClient, error)
	ctx         context.Context
	cancel      context.CancelFunc
	sampleMu    sync.Mutex
	resources   platform.Resources
	processRefs map[rpcClient]int
}

type session struct {
	binding   platform.DriverBinding
	profile   platform.DriverProfile
	open      platform.DriverOpen
	mu        sync.Mutex
	client    rpcClient
	opened    bool
	latest    map[string]platform.DriverSample
	sampling  bool
	stop      context.CancelFunc
	preview   *time.Timer
	done      chan struct{}
	resources platform.Resources
	draining  bool // persisted binding intent, guarded by Server.mu
	released  bool
}

func New(config Config) (*Server, error) {
	if config.Directory == "" || config.StateDir == "" || config.Token == "" || config.Resolve == nil {
		return nil, fmt.Errorf("gateway needs a trusted package directory, state directory, bearer token and package resolver")
	}
	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{config: config, processes: map[string]rpcClient{}, processRefs: map[rpcClient]int{}, sessions: map[string]*session{}, active: map[string]string{}, receipts: map[string]receipt{}, ctx: ctx, cancel: cancel}
	s.start = func(path string) (rpcClient, error) { return driver.Start(path) }
	if err := s.loadBindings(); err != nil {
		cancel()
		return nil, err
	}
	_ = s.resources.Track("driver sessions", s.closeSessions)
	return s, nil
}

func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = s.CloseWait(ctx)
}

func (s *Server) CloseWait(ctx context.Context) error {
	s.cancel()
	return s.resources.Close(ctx)
}

func sessionKey(connection, revision string) string { return connection + "/" + revision }
func processKey(binding platform.DriverBinding) string {
	return binding.Package + "@" + binding.Version + "/" + binding.Digest
}

func (s *Server) process(ctx context.Context, binding platform.DriverBinding) (rpcClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := processKey(binding)
	if client := s.processes[key]; client != nil {
		if client.Available() {
			s.processRefs[client]++
			return client, nil
		}
		if err := client.CloseWait(ctx); err != nil {
			return nil, err
		}
		delete(s.processes, key)
		if s.processRefs[client] == 0 {
			delete(s.processRefs, client)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime, err := s.config.Resolve(binding)
	if err != nil {
		return nil, err
	}
	target, err := runtime.Target()
	if err != nil || strings.TrimPrefix(target.Artifact, "sha256:") != strings.TrimPrefix(binding.Digest, "sha256:") {
		return nil, fmt.Errorf("this driver binary does not target the gateway operating system and architecture")
	}
	path, _, err := driver.Binary(s.config.Directory, binding.Digest)
	if err != nil {
		return nil, err
	}
	client, err := s.start(path)
	if err != nil {
		return nil, err
	}
	s.processes[key] = client
	s.processRefs[client] = 1
	abandon := func() {
		closing, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if client.CloseWait(closing) == nil {
			delete(s.processes, key)
			delete(s.processRefs, client)
		} else {
			s.processRefs[client] = 0
		}
	}
	var described platform.DriverRuntime
	if err = client.Call(ctx, "describe", struct{}{}, &described); err != nil {
		abandon()
		return nil, err
	}
	if described.ABI != platform.DriverABI || !reflect.DeepEqual(described.Profiles, runtime.Profiles) {
		abandon()
		return nil, fmt.Errorf("driver description differs from its installed package")
	}
	s.processes[key] = client
	return client, nil
}

// call restores a read session after a process restart. A failed execute is
// never replayed here: its durable attempt receipt decides subsequent requests.
func (s *Server) call(ctx context.Context, live *session, method string, params, result any) error {
	release, err := live.resources.Acquire()
	if err != nil {
		return err
	}
	defer release()
	live.mu.Lock()
	defer live.mu.Unlock()
	client, err := s.process(ctx, live.binding)
	if err != nil {
		return err
	}
	defer s.releaseProcess(context.Background(), live.binding, client)
	if client == live.client && !live.opened {
		return fmt.Errorf("the prior driver initialization needs cleanup before retry")
	}
	if client != live.client {
		var generation [16]byte
		if _, err = rand.Read(generation[:]); err != nil {
			return err
		}
		live.open.Generation = hex.EncodeToString(generation[:])
		s.mu.Lock()
		s.processRefs[client]++
		s.mu.Unlock()
		previous := live.client
		live.client, live.opened = client, false
		if previous != nil {
			_ = s.releaseProcess(ctx, live.binding, previous)
		}
		// Track the handle before open: timeout can mean the driver opened the
		// device but its reply was late. Cleanup still owns that exact session.
		if err = client.Call(ctx, "validate", live.open, nil); err != nil {
			return err
		}
		if err = client.Call(ctx, "open", live.open, nil); err != nil {
			return err
		}
		live.opened = true
		if live.sampling {
			if err = client.Call(ctx, "start", platform.DriverSession{Session: live.open.Session}, nil); err != nil {
				return err
			}
		}
	}
	if method == "" {
		return nil
	}
	return client.Call(ctx, method, params, result)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.config.Token)) != 1 {
		writeError(w, http.StatusUnauthorized, "gateway bearer token is required")
		return
	}
	release, err := s.resources.Acquire()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "gateway is draining")
		return
	}
	defer release()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "bindings" || parts[2] != "revisions" || !identity(parts[1]) || !platform.DriverDigest(parts[3]) {
		writeError(w, http.StatusNotFound, "unknown gateway route")
		return
	}
	connection, revision, operation := parts[1], parts[3], parts[4]
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if operation == "configure" && r.Method == http.MethodPost {
		var binding platform.DriverBinding
		if err := decode(r, &binding); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := binding.Check(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if tenant := r.Header.Get("X-Platform-Tenant"); tenant != "" && binding.Scope != "" && binding.Scope != platform.DriverScope(tenant) {
			writeError(w, http.StatusConflict, "binding belongs to another tenant scope")
			return
		}
		if binding.Revision() != revision {
			writeError(w, http.StatusConflict, "binding revision differs from its configuration")
			return
		}
		live, err := s.configure(ctx, connection, revision, binding, r.URL.Query().Get("readOnly") == "1")
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revision": revision, "ready": live != nil})
		return
	}
	s.mu.Lock()
	live := s.sessions[sessionKey(connection, revision)]
	s.mu.Unlock()
	if live == nil {
		if operation == "release" && r.Method == http.MethodPost {
			var binding platform.DriverBinding
			if err := decode(r, &binding); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if tenant := r.Header.Get("X-Platform-Tenant"); tenant != "" && binding.Scope != "" && binding.Scope != platform.DriverScope(tenant) {
				writeError(w, http.StatusConflict, "binding belongs to another tenant scope")
				return
			}
			if binding.Revision() != revision {
				writeError(w, http.StatusConflict, "release needs the original binding revision")
				return
			}
			prepared, err := s.newSession(connection, revision, binding)
			if err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			s.mu.Lock()
			live = s.sessions[sessionKey(connection, revision)]
			if live == nil {
				live = prepared
				s.sessions[sessionKey(connection, revision)] = live
			}
			s.mu.Unlock()
		} else {
			writeError(w, http.StatusNotFound, "configure this exact binding revision first")
			return
		}
	}
	if tenant := r.Header.Get("X-Platform-Tenant"); tenant != "" {
		if live.binding.Scope != "" && live.binding.Scope != platform.DriverScope(tenant) {
			writeError(w, http.StatusConflict, "binding belongs to another tenant scope")
			return
		}
		if operation == "release" && live.binding.Scope == "" && s.config.LegacyTenant != tenant {
			writeError(w, http.StatusConflict, "Legacy bindings require an explicit gateway tenant before release")
			return
		}
	}
	switch {
	case operation == "lifecycle" && r.Method == http.MethodGet:
		status, err := s.sessionStatus(connection, revision, live)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	case operation == "release" && r.Method == http.MethodPost:
		status, err := s.releaseSession(ctx, connection, revision, live)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		code := http.StatusOK
		if len(status.Blockers) != 0 {
			code = http.StatusConflict
		}
		writeJSON(w, code, status)
	case operation == "check" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		if strings.HasPrefix(connection, "preview-") {
			defer s.discardPreview(connection, revision, live)
		}
		result, err := s.read(ctx, live, platform.DriverRead{Session: live.open.Session, Mode: "latest"})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	case operation == "source" && r.Method == http.MethodGet:
		if r.URL.Query().Get("commands") == "1" {
			s.commandSource(w, connection, revision)
			return
		}
		if err := s.startSampling(ctx, live); err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		result, err := s.read(ctx, live, platform.DriverRead{Session: live.open.Session, Mode: "latest"})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		rows := make([]map[string]any, 0, len(result.Samples))
		for _, sample := range result.Samples {
			rows = append(rows, map[string]any{"id": sample.Device + ":" + sample.Signal, "device": sample.Device, "signal": sample.Signal, "type": sample.Type, "value": sample.Value, "unit": sample.Unit, "quality": sample.Quality, "at": sample.At, "receivedAt": sample.ReceivedAt, "reason": sample.Reason, "source": sample.Source})
		}
		writeJSON(w, http.StatusOK, rows)
	case operation == "observation" && r.Method == http.MethodPost:
		var input struct {
			Device string    `json:"device"`
			Signal string    `json:"signal"`
			Since  time.Time `json:"since"`
			Until  time.Time `json:"until"`
			Mode   string    `json:"mode"`
			Limit  int       `json:"limit"`
		}
		if err := decode(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		latest := input.Mode == "latest"
		if latest {
			if input.Device == "" || input.Signal == "" || !input.Since.IsZero() || !input.Until.IsZero() || input.Limit != 0 {
				writeError(w, http.StatusBadRequest, "current observation needs one device and signal without a time window or point limit")
				return
			}
		} else {
			if !input.Since.Before(input.Until) || !slices.Contains([]string{"summary", "points"}, input.Mode) || input.Limit < 0 || input.Limit > 10000 {
				writeError(w, http.StatusBadRequest, "observation needs a bounded time window and summary or points mode")
				return
			}
			if err := s.startSampling(ctx, live); err != nil {
				writeError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
		}
		result, err := s.read(ctx, live, platform.DriverRead{Session: live.open.Session, Device: input.Device, Signals: []string{input.Signal}, Since: input.Since, Until: input.Until, Mode: input.Mode, Limit: input.Limit})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if latest {
			if len(result.Samples) != 1 || result.Samples[0].Device != input.Device || result.Samples[0].Signal != input.Signal {
				writeError(w, http.StatusServiceUnavailable, "the declared current device point is unavailable")
				return
			}
			writeJSON(w, http.StatusOK, platform.DriverReadResult{Samples: result.Samples})
			return
		}
		if result.Window == nil {
			writeError(w, http.StatusServiceUnavailable, "this driver has no durable observation history; connect an external time-series collector")
			return
		}
		writeJSON(w, http.StatusOK, result)
	case operation == "execute" && r.Method == http.MethodPost:
		s.execute(w, r.WithContext(ctx), connection, revision, live)
	default:
		writeError(w, http.StatusMethodNotAllowed, "unsupported gateway operation")
	}
}

func (s *Server) configure(ctx context.Context, connection, revision string, binding platform.DriverBinding, readOnly bool) (*session, error) {
	prepared, err := s.newSession(connection, revision, binding)
	if err != nil {
		return nil, err
	}
	if len(binding.Configuration.Commands) > 0 && !slices.Contains(prepared.profile.Operations, "execute") {
		return nil, fmt.Errorf("this protocol profile is read-only")
	}
	s.mu.Lock()
	key := sessionKey(connection, revision)
	live := s.sessions[key]
	if live == nil {
		live = prepared
		s.sessions[key] = live
	} else if (live.released || live.resources.State().State == "closed") && !readOnly {
		if live.resources.State().State != "closed" {
			s.mu.Unlock()
			return nil, fmt.Errorf("the prior binding has not finished cleanup")
		}
		live = prepared
		s.sessions[key] = live
	} else if live.draining && !readOnly {
		s.mu.Unlock()
		return nil, fmt.Errorf("this binding still has unresolved retirement work")
	}
	s.mu.Unlock()
	if !readOnly || strings.HasPrefix(connection, "preview-") {
		if err = s.call(ctx, live, "", nil, nil); err != nil {
			closing, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			cleanupErr := s.closeSession(closing, live)
			cancel()
			if cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
			if strings.HasPrefix(connection, "preview-") {
				s.discardPreview(connection, revision, live)
			}
			return nil, err
		}
	}
	if strings.HasPrefix(connection, "preview-") {
		live.mu.Lock()
		if live.preview != nil {
			live.preview.Stop()
		}
		live.preview = time.AfterFunc(time.Minute, func() { s.discardPreview(connection, revision, live) })
		live.mu.Unlock()
		return live, nil
	}
	s.mu.Lock()
	active := s.active[connection]
	if !readOnly {
		active = revision
	}
	if err = s.persistBinding(connection, active); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	old := s.sessions[sessionKey(connection, s.active[connection])]
	s.active[connection] = active
	s.mu.Unlock()
	if !readOnly {
		if old != nil && old != live {
			if err = s.stopSampling(ctx, old); err != nil {
				return nil, err
			}
			if err = s.closeConnection(ctx, old); err != nil {
				return nil, err
			}
		}
	}
	return live, nil
}

func (s *Server) read(ctx context.Context, live *session, input platform.DriverRead) (platform.DriverReadResult, error) {
	var result platform.DriverReadResult
	err := s.call(ctx, live, "read", input, &result)
	if err != nil {
		return result, err
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	for i := range result.Samples {
		sample := &result.Samples[i]
		sample.Source.Connection, sample.Source.Revision, sample.Source.Generation = live.open.Connection, live.open.Revision, live.open.Generation
		if sample.ReceivedAt.IsZero() {
			sample.ReceivedAt = time.Now().UTC()
		}
		live.latest[sample.Device+":"+sample.Signal] = *sample
	}
	return result, nil
}

func identity(value string) bool {
	return value != "" && len(value) <= 200 && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n\t ")
}
func decode(r *http.Request, value any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 512<<10))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid gateway request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("gateway request must contain one JSON value")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
