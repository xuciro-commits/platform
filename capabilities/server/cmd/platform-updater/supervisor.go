package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"platformserver/kernel/systemscope"
	"platformserver/services/apps/system/build"
	"slices"
	"sync"
	"syscall"
	"time"
)

type supervisor struct {
	mu                 sync.Mutex
	root               string
	state              releaseState
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
