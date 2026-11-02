package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"platformserver/kernel/systemscope"
	"platformserver/services/apps/system/build"
	"sync"
	"time"
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
