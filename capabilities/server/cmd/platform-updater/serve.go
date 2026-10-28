// The updater supervises exactly one host. current.json is both the selected
// release and its unfinished activation outcome, so a restart resumes the same
// activation instead of inventing another request.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"platformserver/internal/systembuild"
	"platformserver/internal/systemscope"
)

type releaseState struct {
	Release         int             `json:"release"`
	Serving         int             `json:"serving,omitempty"`
	Binary          string          `json:"binary"`
	Digest          string          `json:"digest,omitempty"`
	Web             string          `json:"web,omitempty"`
	WebDigest       string          `json:"webDigest,omitempty"`
	Source          string          `json:"source,omitempty"`
	SourceDigest    string          `json:"sourceDigest,omitempty"`
	Previous        *releaseState   `json:"previous,omitempty"`
	Activation      string          `json:"activation,omitempty"`
	Result          string          `json:"result,omitempty"`
	Evidence        string          `json:"evidence,omitempty"`
	PendingRollback *rollbackIntent `json:"pendingRollback,omitempty"`
}

// A completed release that fails on restart needs a new rollback decision.
// Keep that one request in current.json until its exact activation is known.
type rollbackIntent struct {
	Release     int    `json:"release"`
	Authority   int    `json:"authority,omitempty"`
	Key         string `json:"key"`
	Maintenance bool   `json:"maintenance,omitempty"`
	Evidence    string `json:"evidence"`
}

type activationView struct {
	ID      string    `json:"id"`
	Release int       `json:"release"`
	From    int       `json:"from"`
	Result  string    `json:"result"`
	Window  int       `json:"window"`
	At      time.Time `json:"at"`
}

func (a activationView) ready(now time.Time) bool {
	return !now.Before(a.At.Add(time.Duration(a.Window) * time.Second))
}

// Maintenance commands and the supervisor touch this file. Each replacement is
// atomic; the lock prevents a successful report clearing a newer selection.
var currentMu sync.Mutex

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func readState(path string) (releaseState, error) {
	var state releaseState
	raw, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.Release == 0 {
		return state, errors.New("current.json names no release")
	}
	if state.Binary == "" {
		state.Binary = filepath.Join(filepath.Dir(path), "bin", fmt.Sprintf("platform-server-%04d", state.Release))
	}
	return state, nil
}

func readRelease(root string, version int) (releaseState, error) {
	dir := filepath.Join(root, ".build", "system")
	stamp := fmt.Sprintf("%04d", version)
	raw, err := os.ReadFile(filepath.Join(dir, "releases", stamp+".json"))
	if err != nil {
		return releaseState{}, err
	}
	var manifest releaseManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return releaseState{}, err
	}
	state := releaseState{Release: version, Binary: filepath.Join(dir, "bin", "platform-server-"+stamp), Digest: manifest.Binary,
		Web: filepath.Join(dir, "web", stamp), WebDigest: manifest.Web, Source: manifest.Source, SourceDigest: manifest.SourceDigest}
	if err := validateRelease(state); err != nil {
		return state, err
	}
	return state, nil
}

func validateRelease(state releaseState) error {
	got := systembuild.DigestFile(state.Binary)
	if got == "" {
		return fmt.Errorf("release %04d has no binary", state.Release)
	}
	if state.Digest != "" && got != state.Digest {
		// Historical builds hashed a one-entry tree instead of the file bytes.
		// Accepted history stays readable; all sealed-source releases use raw SHA256.
		legacy := systemscope.Digest([]byte(got + "  .\n"))
		if state.SourceDigest != "" || legacy != state.Digest {
			return fmt.Errorf("release %04d binary digest mismatch", state.Release)
		}
	}
	if state.SourceDigest != "" {
		if err := systembuild.VerifySource(state.Source, state.SourceDigest); err != nil {
			return err
		}
		if state.WebDigest == "" {
			return errors.New("sealed release has no workspace digest")
		}
	}
	if state.WebDigest != "" {
		got, err := systembuild.DigestTree(state.Web)
		if err != nil {
			return err
		}
		if got != state.WebDigest {
			return fmt.Errorf("release %04d workspace digest mismatch", state.Release)
		}
	}
	return nil
}

func serving(state releaseState) releaseState {
	state.Activation, state.Result, state.Evidence, state.PendingRollback, state.Serving = "", "", "", nil, 0
	return state
}

func selection(state releaseState) releaseState {
	state.Previous, state.Activation, state.Result, state.Evidence, state.PendingRollback, state.Serving = nil, "", "", "", nil, 0
	return state
}

func stageActivation(c client, root string, activation activationView) error {
	if activation.Result != "requested" || !activation.ready(time.Now()) {
		return errors.New("activation is not ready")
	}
	current := filepath.Join(root, ".build", "system", "current.json")
	currentMu.Lock()
	defer currentMu.Unlock()
	previous, err := readState(current)
	if os.IsNotExist(err) {
		if err := verifyFirstActivation(c, root, activation); err != nil {
			return err
		}
		first, err := resolveRelease(c, root, activation.Release)
		if err != nil {
			return err
		}
		first.Activation = activation.ID
		return writeJSON(current, first)
	}
	if err != nil {
		return err
	}
	if previous.Activation == activation.ID {
		return nil
	}
	if previous.Activation != "" || previous.PendingRollback != nil {
		return errors.New("the current activation has not finished")
	}
	rollback := selection(previous)
	if previous.Release != activation.From {
		logical, err := resolveRelease(c, root, activation.From)
		if err != nil {
			return err
		}
		if previous.SourceDigest == "" || logical.SourceDigest != previous.SourceDigest {
			return fmt.Errorf("activation %s predecessor has different running source", activation.ID)
		}
		rollback = selection(logical)
	}
	next, err := resolveRelease(c, root, activation.Release)
	if err != nil {
		return err
	}
	next.Previous, next.Activation, next.Serving = &rollback, activation.ID, previous.Release
	return writeJSON(current, next)
}

// An unmanaged initial host can have already published hot definitions. It is
// still the first supervised code release only when every predecessor is a
// definition over that initial frozen source, ending at version zero.
func verifyFirstActivation(c client, root string, a activationView) error {
	var health struct {
		Release int `json:"release"`
	}
	if err := c.call("GET", "/healthz", nil, &health); err != nil {
		return err
	}
	if health.Release != 0 {
		return errors.New("current.json is missing for an already versioned host; restore its supervision state")
	}
	if a.From == 0 {
		return nil
	}
	var releases []struct {
		Version, Previous          int
		Kind, Change, SourceDigest string
	}
	if err := c.call("GET", "/v1/system-releases", nil, &releases); err != nil {
		return err
	}
	targetChange := ""
	for _, release := range releases {
		if release.Version == a.Release {
			targetChange = release.Change
			break
		}
	}
	if targetChange == "" {
		return errors.New("first code release names no approved source change")
	}
	change, err := readChange(c, targetChange)
	if err != nil {
		return err
	}
	if change.Base != a.From || change.SourceDigest == "" {
		return errors.New("first code release has no matching initial source baseline")
	}
	if err := systembuild.VerifySource(filepath.Join(root, ".build", "system", "sources", change.SourceDigest), change.SourceDigest); err != nil {
		return err
	}
	seen := map[int]bool{}
	for version := a.From; version != 0; {
		if seen[version] {
			return errors.New("initial release predecessor chain cycles")
		}
		seen[version] = true
		found := false
		for _, release := range releases {
			if release.Version != version {
				continue
			}
			found = true
			if release.Kind != "definitions" || release.SourceDigest != change.SourceDigest {
				return errors.New("missing current.json is not an initial definition-only lineage")
			}
			version = release.Previous
			break
		}
		if !found {
			return errors.New("initial release predecessor chain is incomplete")
		}
	}
	return nil
}

// The saved outcome and stable key are reused verbatim after a lost response.
func reportPending(c client, path string) error {
	currentMu.Lock()
	state, err := readState(path)
	currentMu.Unlock()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.Activation == "" || state.Result == "" || c.token == "" {
		return nil
	}
	_, err = c.submitKey("system.release.finished", systemscope.ActivationType, state.Activation, systemscope.App,
		"system-finished:"+state.Activation+":"+state.Result, map[string]any{"result": state.Result, "evidence": state.Evidence})
	if err != nil {
		return err
	}
	currentMu.Lock()
	defer currentMu.Unlock()
	latest, err := readState(path)
	if err != nil {
		return err
	}
	if latest.Activation != state.Activation || latest.Result != state.Result || latest.Evidence != state.Evidence {
		return nil
	}
	latest.Activation, latest.Result, latest.Evidence = "", "", ""
	return writeJSON(path, latest)
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	root := flags.String("root", ".", "repository root holding .build/system")
	addr := flags.String("addr", "127.0.0.1:8495", "host listen address")
	web := flags.String("web", "", "workspace dist; must match the release digest")
	data := flags.String("data", "", "host data directory")
	tenants := flags.String("tenants", "", "tenants file")
	tenant := flags.String("tenant", "", "development tenant")
	admin := flags.String("admin", "", "development administrator")
	profile := flags.String("profile", "lightweight", "host profile")
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host URL for outcome reports")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "system publisher token")
	drain := flags.Duration("drain", 30*time.Second, "old host shutdown deadline")
	health := flags.Duration("health-timeout", 45*time.Second, "started host health deadline")
	watch := flags.Bool("watch", false, "retired dynamic self-hosting option")
	flags.Duration("every", 5*time.Second, "retired watch interval")
	flags.Parse(args)
	if *watch {
		return errors.New("dynamic self-hosting is frozen; serve does not accept -watch")
	}
	serveArgs := []string{"-addr", *addr, "-profile", *profile}
	if *tenant != "" {
		serveArgs = append(serveArgs, "-tenant", *tenant)
	}
	if *admin != "" {
		serveArgs = append(serveArgs, "-admin", *admin)
	}
	serveArgs = append(serveArgs, flags.Args()...)
	s := &supervisor{root: *root, current: filepath.Join(*root, ".build", "system", "current.json"), args: serveArgs,
		web: *web, data: *data, tenants: *tenants, healthURL: "http://" + *addr + "/healthz", health: *health, drain: *drain,
		c: client{host: *hostURL, token: *token, tenant: systemscope.Tenant}}
	return s.run()
}

type supervisor struct {
	mu                 sync.Mutex
	root               string
	state              releaseState // the actual running selection, never an unproven candidate
	running            *exec.Cmd
	current            string
	args               []string
	web, data, tenants string
	healthURL          string
	health, drain      time.Duration
	c                  client
}

func (s *supervisor) run() error {
	desired, err := readState(s.current)
	if err != nil {
		return err
	}
	if err := s.startInitial(desired); err != nil {
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-signals:
			s.stop(s.drain)
			return nil
		case <-ticker.C:
		}
		if err := s.reload(); err != nil {
			fmt.Fprintln(os.Stderr, "platform-updater:", err)
		}
		s.mu.Lock()
		stopped := s.running == nil
		s.mu.Unlock()
		if stopped {
			desired, readErr := readState(s.current)
			if readErr != nil {
				fmt.Fprintln(os.Stderr, "platform-updater restart:", readErr)
				continue
			}
			if err := s.startInitial(desired); err != nil {
				fmt.Fprintln(os.Stderr, "platform-updater restart:", err)
			}
		}
	}
}

// Resume the last proven process first when an unfinished activation was
// staged. A failure of an already completed version is a new rollback request,
// never a revision of its original terminal outcome.
func (s *supervisor) startInitial(desired releaseState) error {
	initial := desired
	if desired.Activation != "" && desired.Result == "" && desired.Previous != nil {
		initial = *desired.Previous
	}
	if err := s.start(initial); err != nil {
		if desired.Previous == nil {
			return err
		}
		if desired.Activation != "" && desired.Result == "" {
			return s.recover(*desired.Previous, desired, "aborted", fmt.Sprintf("initial host could not start: %v", err))
		}
		previous := selection(*desired.Previous)
		if recoverErr := s.start(previous); recoverErr != nil {
			return fmt.Errorf("release %d startup failed (%v), recovery failed: %w", desired.Release, err, recoverErr)
		}
		previous.Activation, previous.Result, previous.Evidence = desired.Activation, desired.Result, desired.Evidence
		previous.PendingRollback = &rollbackIntent{Release: desired.Release, Maintenance: true, Key: "system-restart-rollback:" + randomKey(), Evidence: fmt.Sprintf("release %04d failed on restart: %v; release %04d is healthy", desired.Release, err, previous.Release)}
		currentMu.Lock()
		saveErr := writeJSON(s.current, previous)
		currentMu.Unlock()
		if saveErr != nil {
			return saveErr
		}
		s.state = previous
	}
	return nil
}

func (s *supervisor) activation(id string) (activationView, error) {
	var activations []activationView
	if err := s.c.call("GET", "/v1/system-activations", nil, &activations); err != nil {
		return activationView{}, err
	}
	for _, a := range activations {
		if a.ID == id {
			return a, nil
		}
	}
	return activationView{}, fmt.Errorf("activation %s no longer exists", id)
}

// A mixed code/definition activation can be refused by its owner after the
// binary became healthy. Unknown responses retain the exact original request;
// a confirmed refusal restores the prior process before reporting failure.
func (s *supervisor) flushOutcome() error {
	err := reportPending(s.c, s.current)
	if !refusedDecision(err) {
		return err
	}
	state, readErr := readState(s.current)
	if readErr != nil {
		return readErr
	}
	if state.Result != "active" {
		return err
	}
	activation, readErr := s.activation(state.Activation)
	if readErr != nil {
		return readErr
	}
	if activation.Result == "active" || activation.Result == "superseded" {
		// An earlier attempt was accepted and only its response was lost.
		currentMu.Lock()
		defer currentMu.Unlock()
		latest, readErr := readState(s.current)
		if readErr != nil {
			return readErr
		}
		if latest.Activation != state.Activation || latest.Result != state.Result {
			return nil
		}
		latest.Activation, latest.Result, latest.Evidence = "", "", ""
		return writeJSON(s.current, latest)
	}
	if state.PendingRollback != nil {
		// Restart recovery has already restored the process. The original active
		// report was rejected, so finish that request as failed instead of asking
		// to roll back a release that never became active in the authority.
		state.Result, state.Evidence = "rolled-back", state.PendingRollback.Evidence+"; activation rejected: "+err.Error()
		state.PendingRollback = nil
		if activation.Result != "requested" {
			state.Activation, state.Result, state.Evidence = "", "", ""
		}
		currentMu.Lock()
		saveErr := writeJSON(s.current, state)
		currentMu.Unlock()
		if saveErr != nil {
			return saveErr
		}
		s.state = state
		return reportPending(s.c, s.current)
	}
	if state.Previous == nil {
		return fmt.Errorf("activation refused and no previous release is retained: %w", err)
	}
	previous := selection(*state.Previous)
	if activation.Result != "requested" {
		s.stop(s.drain)
		if downErr := s.waitDown(s.drain); downErr != nil {
			return downErr
		}
		if startErr := s.start(previous); startErr != nil {
			return startErr
		}
		return s.restoreCancelled(previous)
	}
	return s.recover(previous, state, "rolled-back", "activation rejected: "+err.Error())
}

func (s *supervisor) resumeRollback() error {
	state, err := readState(s.current)
	if err != nil {
		return err
	}
	intent := state.PendingRollback
	if intent == nil || state.Result != "" {
		return nil
	}
	if intent.Maintenance {
		return errors.New("the retained predecessor is serving; static maintenance must reconcile the failed release without requesting a new dynamic activation")
	}
	if s.c.token == "" {
		return errors.New("restart rollback needs a system publisher token")
	}
	if intent.Authority == 0 {
		var releases []struct {
			Version      int    `json:"version"`
			State        string `json:"state"`
			SourceDigest string `json:"sourceDigest"`
		}
		if err := s.c.call("GET", "/v1/system-releases", nil, &releases); err != nil {
			return err
		}
		failed, err := readRelease(s.root, intent.Release)
		// Historical current files may have no ordinal manifest. Their code
		// identity is still in the saved selection's binary, never a live tree.
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, release := range releases {
			if release.State != "active" {
				continue
			}
			if failed.SourceDigest != "" && release.SourceDigest != failed.SourceDigest {
				return errors.New("the authority now runs a different source; restart recovery cannot roll it back")
			}
			intent.Authority = release.Version
			break
		}
		if intent.Authority == 0 {
			return errors.New("restart recovery found no active system release")
		}
		currentMu.Lock()
		saveErr := writeJSON(s.current, state)
		currentMu.Unlock()
		if saveErr != nil {
			return saveErr
		}
	}
	if _, err := s.c.submitKey("system.release.rollback", systemscope.ReleaseType, fmt.Sprintf("%04d", intent.Authority), systemscope.App, intent.Key, map[string]any{"window": 0, "targetVersion": state.Release}); err != nil {
		if refusedDecision(err) {
			// The original rollback was definitively not accepted. Keep its
			// identity and the proven running predecessor for static repair.
			intent.Maintenance = true
			currentMu.Lock()
			saveErr := writeJSON(s.current, state)
			currentMu.Unlock()
			if saveErr != nil {
				return saveErr
			}
		}
		return err
	}
	var activations []activationView
	if err := s.c.call("GET", "/v1/system-activations", nil, &activations); err != nil {
		return err
	}
	slices.SortFunc(activations, func(a, b activationView) int { return b.At.Compare(a.At) })
	for _, a := range activations {
		if a.From != intent.Authority || a.Release != state.Release || a.Result != "requested" || !a.ready(time.Now()) {
			continue
		}
		state.Activation, state.Result, state.Evidence = a.ID, "active", intent.Evidence
		state.PendingRollback = nil
		currentMu.Lock()
		saveErr := writeJSON(s.current, state)
		currentMu.Unlock()
		if saveErr != nil {
			return saveErr
		}
		s.state = state
		return s.flushOutcome()
	}
	return fmt.Errorf("rollback of release %d returned no matching requested activation", intent.Release)
}

func (s *supervisor) reload() error {
	if err := s.flushOutcome(); err != nil {
		return err
	}
	if err := s.resumeRollback(); err != nil {
		return err
	}
	currentMu.Lock()
	next, err := readState(s.current)
	currentMu.Unlock()
	if err != nil {
		return err
	}
	if next.Result != "" {
		return nil
	}
	changed := next.Release != s.state.Release || next.Binary != s.state.Binary || next.Digest != s.state.Digest
	if !changed {
		if next.Activation == "" {
			return nil
		}
		requested, err := s.requested(next.Activation)
		if err != nil {
			return err
		}
		if !requested {
			return s.restoreCancelled(selection(s.state))
		}
		if err := waitHealth(s.healthURL, next.Release, next.Digest, s.health); err != nil {
			return err
		}
		return s.complete(next, "active", fmt.Sprintf("release %04d is healthy", next.Release))
	}
	if next.Activation == "" {
		return errors.New("a different release requires an activation")
	}
	return s.replace(serving(s.state), next)
}

func (s *supervisor) requested(id string) (bool, error) {
	a, err := s.activation(id)
	if err != nil {
		return false, err
	}
	return a.Result == "requested" && a.ready(time.Now()), nil
}

func (s *supervisor) replace(previous, next releaseState) error {
	requested, err := s.requested(next.Activation)
	if err != nil {
		return err
	}
	if !requested {
		return s.restoreCancelled(previous)
	}
	if err := s.probe(next); err != nil {
		return s.completeSelection(previous, next, "aborted", fmt.Sprintf("candidate probe failed: %v", err))
	}
	// Cancellation can happen while the candidate is being probed. Check the
	// owner immediately before disturbing the running process.
	requested, err = s.requested(next.Activation)
	if err != nil {
		return err
	}
	if !requested {
		return s.restoreCancelled(previous)
	}
	rollback := selection(previous)
	if next.Previous != nil {
		rollback = selection(*next.Previous)
	}
	s.stop(s.drain)
	if err := s.waitDown(s.drain); err != nil {
		return s.recover(rollback, next, "rolled-back", err.Error())
	}
	if err := s.start(next); err != nil {
		return s.recover(rollback, next, "rolled-back", fmt.Sprintf("candidate startup failed: %v", err))
	}
	next.Previous = &rollback
	return s.complete(next, "active", fmt.Sprintf("release %04d answered healthy with binary %s", next.Release, next.Digest))
}

func (s *supervisor) restoreCancelled(previous releaseState) error {
	currentMu.Lock()
	defer currentMu.Unlock()
	if err := writeJSON(s.current, previous); err != nil {
		return err
	}
	s.state = previous
	return nil
}

func (s *supervisor) recover(previous, next releaseState, result, evidence string) error {
	s.stop(s.drain)
	if err := s.waitDown(s.drain); err != nil {
		evidence += "; listener did not stop: " + err.Error()
		result = "aborted"
	}
	if err := s.start(previous); err != nil {
		evidence += "; recovery startup failed: " + err.Error()
		result = "aborted"
	}
	return s.completeSelection(previous, next, result, evidence)
}

func (s *supervisor) completeSelection(selected, next releaseState, result, evidence string) error {
	selected.Activation, selected.Result, selected.Evidence = next.Activation, result, evidence
	return s.complete(selected, result, evidence)
}

func (s *supervisor) complete(state releaseState, result, evidence string) error {
	state.Result, state.Evidence, state.Serving = result, evidence, 0
	currentMu.Lock()
	err := writeJSON(s.current, state)
	currentMu.Unlock()
	if err != nil {
		return err
	}
	s.state = state
	return s.flushOutcome()
}

func (s *supervisor) probe(next releaseState) error {
	if err := validateRelease(next); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "platform-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	parsed, err := url.Parse(s.healthURL)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return err
	}
	addr := listener.Addr().String()
	listener.Close()
	args := []string{"-addr", addr, "-profile", "lightweight", "-data", dir, "-release", fmt.Sprint(next.Release), "-release-digest", next.Digest}
	if next.Source != "" {
		args = append(args, "-source-root", next.Source)
	}
	if s.data != "" {
		args = append(args, "-idp-key", filepath.Join(s.data, "idp.key"))
	}
	cmd := exec.Command(next.Binary, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	return waitHealth("http://"+addr+"/healthz", next.Release, next.Digest, s.health)
}

func (s *supervisor) start(state releaseState) error {
	if err := validateRelease(state); err != nil {
		return err
	}
	args := append([]string{}, s.args...)
	args = append(args, "-release", fmt.Sprint(state.Release), "-release-digest", state.Digest)
	if state.Source != "" {
		args = append(args, "-source-root", state.Source)
	}
	web := state.Web
	if web == "" {
		web = filepath.Join(s.root, ".build", "system", "web", fmt.Sprintf("%04d", state.Release))
		if _, err := os.Stat(web); os.IsNotExist(err) {
			web = ""
		}
	}
	if s.web != "" {
		if state.WebDigest != "" {
			digest, err := systembuild.DigestTree(s.web)
			if err != nil {
				return err
			}
			if digest != state.WebDigest {
				return errors.New("-web does not match the selected release")
			}
		}
		web = s.web
	}
	if web != "" {
		args = append(args, "-web", web)
	}
	if s.data != "" {
		args = append(args, "-data", s.data)
	}
	if s.tenants != "" {
		args = append(args, "-tenants", s.tenants)
	}
	cmd := exec.Command(state.Binary, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	s.running = cmd
	s.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		s.mu.Lock()
		if s.running == cmd {
			s.running = nil
		}
		s.mu.Unlock()
	}()
	if err := waitHealth(s.healthURL, state.Release, state.Digest, s.health); err != nil {
		s.stop(s.drain)
		return err
	}
	s.state = state
	return nil
}

func (s *supervisor) stop(within time.Duration) {
	s.mu.Lock()
	cmd := s.running
	s.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		gone := s.running != cmd
		s.mu.Unlock()
		if gone {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	s.mu.Lock()
	if s.running == cmd {
		s.running = nil
	}
	s.mu.Unlock()
}

func (s *supervisor) waitDown(within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		response, err := (&http.Client{Timeout: time.Second}).Get(s.healthURL)
		if err != nil {
			return nil
		}
		response.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("previous host still answers on the serving address")
}

func waitHealth(endpoint string, release int, digest string, within time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	last := errors.New("no health response")
	for {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return err
		}
		response, err := (&http.Client{Timeout: time.Second}).Do(req)
		if err == nil {
			var body struct {
				Release     int    `json:"release"`
				Digest      string `json:"releaseDigest"`
				Status      string `json:"status"`
				Quarantined int    `json:"quarantined"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&body)
			response.Body.Close()
			if response.StatusCode == 200 && decodeErr == nil && body.Release == release && (digest == "" || body.Digest == digest) && body.Status == "ok" && body.Quarantined == 0 {
				return nil
			}
			last = fmt.Errorf("health reports HTTP %d, release %d, digest %q, status %q, quarantined %d", response.StatusCode, body.Release, body.Digest, body.Status, body.Quarantined)
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(100 * time.Millisecond):
		}
	}
}
