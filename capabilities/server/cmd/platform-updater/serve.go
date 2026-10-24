// The supervisor: the process that owns the running host. It starts the
// release named by .build/system/current.json, keeps it running, and replaces
// it when the file names a newer one. The host it starts never replaces
// itself; this process never serves a request.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// releaseState is what .build/system/current.json holds: the release to run
// and the one to fall back to.
type releaseState struct {
	Release  int    `json:"release"`
	Binary   string `json:"binary"`
	Digest   string `json:"digest,omitempty"`
	Previous *struct {
		Release int    `json:"release"`
		Binary  string `json:"binary"`
	} `json:"previous,omitempty"`
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	root := flags.String("root", ".", "repository root holding .build/system")
	addr := flags.String("addr", "127.0.0.1:8495", "the address the host serves on")
	web := flags.String("web", "", "workspace dist the host serves (empty: the release's own dist)")
	data := flags.String("data", "", "the host's data directory")
	tenants := flags.String("tenants", "", "tenants file")
	tenant := flags.String("tenant", "", "the development tenant when no tenants file is given")
	admin := flags.String("admin", "", "explicit administrator subject of the development tenant")
	profile := flags.String("profile", "lightweight", "the host's profile")
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host base URL to report the outcome to")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "access token of a system publisher")
	drain := flags.Duration("drain", 30*time.Second, "how long the old host may take to stop")
	health := flags.Duration("health-timeout", 45*time.Second, "how long a started host may take to report healthy")
	watch := flags.Bool("watch", false, "also build approved changes and swap requested activations: approval alone becomes a release")
	every := flags.Duration("every", 5*time.Second, "how often the watch loop looks for work")
	flags.Parse(args)
	if *root == "" {
		return errors.New("serve needs -root")
	}
	current := filepath.Join(*root, ".build", "system", "current.json")
	serveArgs := []string{"-addr", *addr, "-profile", *profile}
	if *tenant != "" {
		serveArgs = append(serveArgs, "-tenant", *tenant)
	}
	if *admin != "" {
		serveArgs = append(serveArgs, "-admin", *admin)
	}
	if *watch && *token == "" {
		return errors.New("serve -watch needs -token: a build nobody may approve is not a release")
	}
	supervisor := &supervisor{current: current, root: *root, args: serveArgs,
		web: *web, data: *data, tenants: *tenants, healthURL: "http://" + *addr + "/healthz",
		health: *health, drain: *drain, c: client{host: *hostURL, token: *token, tenant: "system"}}
	return supervisor.run(*watch, *every, *addr)
}

// supervisor owns exactly one host process and the file naming which release
// it should be.
type supervisor struct {
	mu        sync.Mutex
	root      string
	state     releaseState
	running   *exec.Cmd
	release   int
	served    bool
	current   string
	args      []string
	web, data string
	tenants   string
	healthURL string
	health    time.Duration
	drain     time.Duration
	c         client
}

func (s *supervisor) run(watch bool, every time.Duration, addr string) error {
	if err := s.load(); err != nil {
		return err
	}
	if err := s.start(s.state.Release, s.state.Binary, s.state.Digest); err != nil {
		return err
	}
	// The loop that closes self-hosting: with -watch the supervisor turns an
	// approval into a release by itself, and the swap below is the same one an
	// operator's `swap` writes, so both paths meet in current.json.
	if watch {
		go func() {
			built, swapped := map[string]bool{}, map[string]bool{}
			for {
				if err := watchOnce(s.c, s.root, addr, s.data, built, swapped); err != nil {
					fmt.Fprintln(os.Stderr, "platform-updater watch:", err)
				}
				time.Sleep(every)
			}
		}()
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case <-signals:
			// The supervisor owns the host: stopping it stops the child too,
			// with the same drain a swap uses, so nothing is orphaned.
			s.stop(s.drain)
			return nil
		case <-time.After(time.Second):
		}
		if err := s.reload(); err != nil {
			fmt.Fprintln(os.Stderr, "platform-updater:", err)
		}
		if s.running == nil {
			if err := s.start(s.state.Release, s.state.Binary, s.state.Digest); err != nil {
				fmt.Fprintln(os.Stderr, "platform-updater: restart:", err)
			}
		}
	}
}

func (s *supervisor) load() error {
	raw, err := os.ReadFile(s.current)
	if err != nil {
		return fmt.Errorf("%s: %w (a release must be installed before the supervisor starts)", s.current, err)
	}
	var state releaseState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if state.Binary == "" {
		state.Binary = filepath.Join(filepath.Dir(s.current), "bin", fmt.Sprintf("platform-server-%04d", state.Release))
	}
	s.state = state
	return nil
}

// reload notices a newly installed release and swaps to it.
func (s *supervisor) reload() error {
	raw, err := os.ReadFile(s.current)
	if err != nil {
		return nil
	}
	var next releaseState
	if json.Unmarshal(raw, &next) != nil {
		return nil
	}
	if next.Binary == "" {
		next.Binary = filepath.Join(filepath.Dir(s.current), "bin", fmt.Sprintf("platform-server-%04d", next.Release))
	}
	s.mu.Lock()
	changed := next.Release != s.state.Release || next.Binary != s.state.Binary
	s.mu.Unlock()
	if !changed {
		return nil
	}
	s.mu.Lock()
	previous := s.state
	s.state = next
	s.mu.Unlock()
	if !s.served {
		return nil // the first start is the release the file named
	}
	if err := s.replace(previous, next); err != nil {
		return err
	}
	return s.report(next, "active", fmt.Sprintf("release %04d answered /healthz with its own number", next.Release))
}

// replace proves the new release on a spare port and the same data directory,
// drains the old one, starts the new one on the real address and proves it
// again. A failure anywhere keeps (or returns to) the running release.
func (s *supervisor) replace(previous, next releaseState) error {
	if err := s.probe(next); err != nil {
		return fmt.Errorf("release %04d is not healthy on a probe, the running release stays: %w", next.Release, err)
	}
	s.stop(s.drain)
	// Starting before the old listener is gone makes the new process fail to
	// bind while /healthz still answers as the old release, which looks like a
	// bad release and starts a rollback that fights the real one.
	s.waitDown(s.drain)
	if err := s.start(next.Release, next.Binary, next.Digest); err != nil {
		_ = s.start(previous.Release, previous.Binary, previous.Digest)
		s.install(previous)
		return fmt.Errorf("release %04d did not start (%w); it was rolled back", next.Release, err)
	}
	if err := s.await(next.Release); err != nil {
		s.stop(5 * time.Second)
		s.waitDown(5 * time.Second)
		_ = s.start(previous.Release, previous.Binary, previous.Digest)
		_ = s.await(previous.Release)
		s.install(previous)
		_ = s.report(next, "rolled-back", fmt.Sprintf("release %04d did not answer /healthz as itself: %v", next.Release, err))
		return fmt.Errorf("release %04d failed its health check (%w); release %04d is serving again", next.Release, err, previous.Release)
	}
	_ = s.keepPrevious(previous)
	return nil
}

// probe starts the candidate on a spare port with a scratch data directory:
// it proves the binary starts, not that it may be served.
func (s *supervisor) probe(next releaseState) error {
	dir, err := os.MkdirTemp("", "platform-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	args := []string{"-addr", spareAddr(s.args), "-profile", "lightweight", "-data", dir, "-release", fmt.Sprint(next.Release)}
	if s.data != "" {
		args = append(args, "-idp-key", filepath.Join(s.data, "idp.key"))
	}
	cmd := exec.Command(next.Binary, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	}()
	return waitHealth("http://"+spareAddr(s.args)+"/healthz", next.Release, s.health)
}

func (s *supervisor) start(release int, binary, digest string) error {
	args := append([]string{}, s.args...)
	args = append(args, "-release", fmt.Sprint(release))
	if digest != "" {
		args = append(args, "-release-digest", digest)
	}
	// The workspace belongs to the release: a change that edits the Platform
	// ships its dist with the binary, so the two cannot disagree.
	if web := s.webDir(release); web != "" {
		args = append(args, "-web", web)
	}
	if s.data != "" {
		args = append(args, "-data", s.data)
	}
	if s.tenants != "" {
		args = append(args, "-tenants", s.tenants)
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	s.running, s.release, s.served = cmd, release, true
	s.mu.Unlock()
	go func(started *exec.Cmd) {
		_ = started.Wait()
		s.mu.Lock()
		if s.running == started {
			s.running = nil
		}
		s.mu.Unlock()
	}(cmd)
	if err := waitHealth(s.healthURL, release, s.health); err != nil {
		s.stop(5 * time.Second)
		return err
	}
	return nil
}

// stop asks the host to finish in-flight work and waits for it to go.
func (s *supervisor) stop(within time.Duration) {
	s.mu.Lock()
	cmd := s.running
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		gone := s.running == nil
		s.mu.Unlock()
		if gone {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	s.mu.Lock()
	s.running = nil
	s.mu.Unlock()
}

// waitDown waits until nothing answers on the health address, so the next
// release can bind it.
func (s *supervisor) waitDown(within time.Duration) {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		response, err := http.Get(s.healthURL)
		if err != nil {
			return
		}
		response.Body.Close()
		time.Sleep(200 * time.Millisecond)
	}
}

// await waits until the real address reports this release.
func (s *supervisor) await(release int) error {
	return waitHealth(s.healthURL, release, s.health)
}

// webDir is the dist the release was built with, unless the operator pinned
// one directory for every release.
func (s *supervisor) webDir(release int) string {
	if s.web != "" {
		return s.web
	}
	dir := filepath.Join(s.root, ".build", "system", "web", fmt.Sprintf("%04d", release))
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// install names the release the supervisor should run. It is also how a failed
// swap returns the file to the release that is actually serving: leaving the
// bad release named would make the supervisor retry it every second.
func (s *supervisor) install(state releaseState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	body, err := json.MarshalIndent(map[string]any{"release": state.Release, "binary": state.Binary, "digest": state.Digest}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.current, append(body, '\n'), 0o644)
}

func (s *supervisor) keepPrevious(previous releaseState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous.Release == 0 {
		return nil
	}
	body, err := json.MarshalIndent(map[string]any{"release": s.state.Release, "binary": s.state.Binary,
		"digest": s.state.Digest, "previous": map[string]any{"release": previous.Release, "binary": previous.Binary}}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.current, append(body, '\n'), 0o644)
}

// report tells the system tenant what happened, as the updater it is.
func (s *supervisor) report(next releaseState, result, evidence string) error {
	if s.c.token == "" {
		return nil
	}
	_, err := s.c.submit("system.release.finished", "system.activation", fmt.Sprintf("%04d", next.Release), "system",
		map[string]any{"result": result, "evidence": evidence})
	return err
}

// waitHealth waits for /healthz to answer 200 and report the expected release.
func waitHealth(url string, release int, within time.Duration) error {
	deadline := time.Now().Add(within)
	var last error
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			var body struct {
				Release     int    `json:"release"`
				Status      string `json:"status"`
				Quarantined int    `json:"quarantined"`
			}
			raw, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode == 200 {
				if json.Unmarshal(raw, &body) == nil && body.Release == release {
					// A release that serves its number but cannot start a
					// tenant is not healthy: quarantining a tenant is the
					// host saying it lost the ability to replay that data.
					if body.Status != "ok" || body.Quarantined != 0 {
						last = fmt.Errorf("/healthz reports status %q with %d quarantined tenants", body.Status, body.Quarantined)
					} else {
						return nil
					}
				} else {
					last = fmt.Errorf("/healthz reports release %d", body.Release)
				}
			} else {
				last = fmt.Errorf("/healthz answered %s", response.Status)
			}
		} else {
			last = err
		}
		time.Sleep(300 * time.Millisecond)
	}
	if last == nil {
		last = errors.New("no answer")
	}
	return last
}

func spareAddr(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-addr" {
			host, _, ok := strings.Cut(args[i+1], ":")
			if !ok {
				host = "127.0.0.1"
			}
			return host + ":18511"
		}
	}
	return "127.0.0.1:18511"
}
