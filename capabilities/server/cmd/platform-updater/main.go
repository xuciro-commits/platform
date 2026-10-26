// Command platform-updater builds and swaps the platform's own releases. It is
// the only process that runs a compiler or replaces the running host: the host
// records a request and reports the outcome, and never rebuilds itself.
//
// Three steps, each a separate invocation, so an operator (or a Make target)
// sees exactly what happened:
//
//	platform-updater build    -host URL -token T -change <id>
//	platform-updater activate -host URL -token T -release <n>
//	platform-updater swap     -root DIR -host URL -token T -activation ID
//
// A build reads the approved change from the system tenant, materializes its
// frozen files into a fresh work tree, builds with the fixed commands, and
// reports the evidence back as a decision. `swap` stages a requested activation;
// the supervisor probes it, replaces the host, and recovers on failure.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/internal/host"
	"platformserver/internal/systembuild"
	"platformserver/internal/systemscope"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "build":
		err = build(os.Args[2:])
	case "activate":
		err = activate(os.Args[2:])
	case "finish":
		err = finish(os.Args[2:])
	case "swap":
		err = swap(os.Args[2:])
	case "serve":
		err = serve(os.Args[2:])
	case "watch":
		err = watch(os.Args[2:])
	case "export":
		err = export(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "platform-updater:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `platform-updater build    -root DIR -host URL -token T -change ID
platform-updater activate -host URL -token T -release N
platform-updater finish   -host URL -token T -activation ID -result active|rolled-back|aborted -evidence TEXT
platform-updater swap     -root DIR -host URL -token T -activation ID
platform-updater serve    -root DIR -addr HOST:PORT [-web DIR] [-data DIR] [-tenants FILE] [-host URL -token T] [-- HOST_ARGS...]
platform-updater watch    -root DIR -host URL -token T [-every 5s]
platform-updater export   -root DIR -release N [-out DIR]

build compiles an approved change and reports what it produced.
activate asks the host for a window before the swap.
swap selects a requested release, including the first release on a new host.
serve runs the host, and replaces it when a newer release is installed.
watch builds every approved change and swaps every requested activation it
      sees, so an approval in the Platform becomes a release with no terminal.
finish reports a swap's outcome to the system tenant.
export verifies and copies the release's complete sealed source tree.
`)
	os.Exit(2)
}

type client struct {
	host, token, tenant string
	principal           string // the member this token is, as /v1/me answers
}

// me reads who this token is, so a decision is submitted for that member and
// not for an empty principal.
func (c *client) me() (string, error) {
	if c.principal != "" {
		return c.principal, nil
	}
	var out struct {
		PrincipalID string `json:"principalId"`
	}
	if err := c.call("GET", "/v1/me", nil, &out); err != nil {
		return "", err
	}
	if out.PrincipalID == "" {
		return "", errors.New("the token is no member of this tenant")
	}
	c.principal = out.PrincipalID
	return c.principal, nil
}

// A structured owner refusal proves that this attempt was not accepted. A
// connection failure or an unreadable proxy response leaves the result unknown.
type decisionRefusal struct {
	Code, Message string
	Status        int
}

func (e *decisionRefusal) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func refusedDecision(err error) bool {
	var refusal *decisionRefusal
	return errors.As(err, &refusal) && refusal.Status >= 400 && refusal.Status < 500 && refusal.Status != http.StatusTooManyRequests
}

// call performs one authenticated host request.
func (c client) call(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(c.host, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Platform-Tenant", c.tenant)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode >= 300 {
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &body) == nil && body.Error.Code != "" {
			return &decisionRefusal{Code: body.Error.Code, Message: body.Error.Message, Status: response.StatusCode}
		}
		return fmt.Errorf("%s %s: %s: %s", method, path, response.Status, truncate(string(raw), 400))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// submit sends one decision for the system app and returns the accepted record.
func (c client) submit(schema, typ, id, authority string, payload map[string]any) (map[string]any, error) {
	return c.submitKey(schema, typ, id, authority, randomKey(), payload)
}

func (c client) submitKey(schema, typ, id, authority, key string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	member, err := c.me()
	if err != nil {
		return nil, err
	}
	var out map[string]any
	callErr := c.call("POST", "/v1/submissions", map[string]any{
		"tenantId": c.tenant, "principalId": member, "authority": authority,
		"target":         map[string]string{"type": typ, "id": id},
		"schema":         map[string]any{"name": schema, "version": 1},
		"idempotencyKey": key,
		"payload":        base64.StdEncoding.EncodeToString(body),
	}, &out)
	return out, callErr
}

func build(args []string) error {
	flags := flag.NewFlagSet("build", flag.ExitOnError)
	root := flags.String("root", ".", "repository root the build reuses toolchain and caches from")
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host base URL")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "access token of a system publisher")
	changeID := flags.String("change", "", "the approved change id")
	flags.Parse(args)
	if *changeID == "" {
		return errors.New("build needs -change")
	}
	c := client{host: *hostURL, token: *token, tenant: systemscope.Tenant}
	change, err := readChange(c, *changeID)
	if err != nil {
		return err
	}
	if change.State != "approved" {
		return fmt.Errorf("change %s is %s, not approved", change.ID, change.State)
	}
	return buildFor(c, *root, change)
}

// watch is the loop that closes self-hosting: an approval is a record, and
// this process turns records into releases without anybody typing a command.
// It builds each approved change once, then the release a publisher asks for.
// The supervisor still owns the swap: watch only writes what it decided, the
// same file `swap` writes. The supervisor owns candidate health checks.
func watch(args []string) error {
	flags := flag.NewFlagSet("watch", flag.ExitOnError)
	root := flags.String("root", ".", "repository root")
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host base URL")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "access token of a system publisher")
	every := flags.Duration("every", 5*time.Second, "how often to look for work")
	once := flags.Bool("once", false, "handle the work visible now, then exit")
	flags.Parse(args)
	c := client{host: *hostURL, token: *token, tenant: systemscope.Tenant}
	for {
		if err := watchOnce(c, *root); err != nil {
			fmt.Fprintln(os.Stderr, "platform-updater watch:", err)
		}
		if *once {
			return nil
		}
		time.Sleep(*every)
	}
}

func watchOnce(c client, root string) error {
	if err := reportPending(c, filepath.Join(root, ".build", "system", "current.json")); err != nil {
		return err
	}
	var changes []changeView
	if err := c.call("GET", "/v1/system-changes", nil, &changes); err != nil {
		return err
	}
	for _, change := range changes {
		switch change.State {
		case "approved":
			if err := buildFor(c, root, change); err != nil {
				return fmt.Errorf("build %s: %w", change.ID, err)
			}
		case "published":
			// The accepted build may precede a crash while arranging its
			// versioned paths. Its final manifest marks completed installation;
			// do not rehash every historical source tree on each watch tick.
			if _, err := os.Stat(filepath.Join(root, ".build", "system", "releases", fmt.Sprintf("%04d.json", change.Release))); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return err
			}
			result, err := retainedBuild(root, change)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil {
				if err := keep(root, change.Release, result); err != nil {
					return err
				}
			}
		}
	}
	var activations []activationView
	if err := c.call("GET", "/v1/system-activations", nil, &activations); err != nil {
		return err
	}
	slices.SortFunc(activations, func(a, b activationView) int { return b.At.Compare(a.At) })
	for _, activation := range activations {
		if activation.Result != "requested" {
			continue
		}
		if !activation.ready(time.Now()) {
			return nil
		}
		return stageActivation(c, root, activation)
	}
	return nil
}

// buildFor compiles one approved change and reports what it produced, using
// the same path as the `build` command.
func buildFor(c client, root string, change changeView) error {
	result, err := retainedBuild(root, change)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		if change.SourceDigest == "" {
			result = host.SystemBuildResult{Diagnostics: fmt.Sprintf("change %s has no frozen source baseline; propose it again", change.ID)}
		} else {
			files := map[string]string{}
			var deleted []string
			for _, f := range change.Files {
				if f.Deleted {
					deleted = append(deleted, f.Path)
					continue
				}
				files[f.Path] = f.Content
			}
			result, err = systembuild.Build(systembuild.Config{Root: root, Work: filepath.Join(root, ".build", "system", "work")},
				host.SystemBuildRequest{Change: change.ID, Base: change.Base, Source: filepath.Join(root, ".build", "system", "sources", change.SourceDigest), SourceDigest: change.SourceDigest, Files: files, Deleted: deleted})
			if err != nil {
				result.OK, result.Diagnostics = false, err.Error()
			}
		}
		result, err = retainBuild(root, change, result)
		if err != nil {
			return err
		}
	}
	if _, err := c.submitKey("system.change.built", systemscope.ChangeType, change.ID, systemscope.App, "system-build:"+buildIdentity(change), map[string]any{
		"ok": result.OK, "binary": result.Binary, "web": result.Web, "toolchain": result.Toolchain, "diagnostics": result.Diagnostics,
		"manifest": result.Manifest, "sourceDigest": result.SourceDigest,
	}); err != nil {
		if !refusedDecision(err) {
			return err
		}
		// A definitive refusal is evidence, not a transport retry. Close an
		// otherwise still-approved build with the original owner's failure action.
		current, readErr := readChange(c, change.ID)
		if readErr != nil || current.State != "approved" {
			return err
		}
		diagnostic := "The build result was refused: " + err.Error()
		_, failureErr := c.submitKey("system.change.built", systemscope.ChangeType, change.ID, systemscope.App, "system-build-refused:"+buildIdentity(change), map[string]any{"ok": false, "diagnostics": diagnostic})
		return failureErr
	}
	if !result.OK {
		fmt.Printf("build %s: failed after %s: %s\n", change.ID, result.Duration.Round(time.Second), truncate(result.Diagnostics, 2000))
		return nil
	}
	version, err := c.releaseOf(change.ID)
	if err != nil {
		return err
	}
	fmt.Printf("build %s: ok=true work=%s %s\n", change.ID, result.Work, result.Duration.Round(time.Second))
	fmt.Printf("release %04d keeps %s\n", version, filepath.Join(root, ".build", "system", "bin", fmt.Sprintf("platform-server-%04d", version)))
	return keep(root, version, result)
}

// A build is retained before its accepted result is submitted. A lost reply or
// process restart reuses these exact artifacts rather than rebuilding.
type savedBuild struct {
	Change   string                 `json:"change"`
	Manifest string                 `json:"manifest"`
	Result   host.SystemBuildResult `json:"result"`
}

func buildIdentity(change changeView) string {
	return systemscope.Digest([]byte(fmt.Sprintf("%s\n%d\n%s\n%s", change.ID, change.Base, change.SourceDigest, change.Manifest)))
}

func retainedBuild(root string, change changeView) (host.SystemBuildResult, error) {
	var saved savedBuild
	raw, err := os.ReadFile(filepath.Join(root, ".build", "system", "builds", buildIdentity(change), ".build", "result.json"))
	if err != nil {
		return saved.Result, err
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return saved.Result, err
	}
	if saved.Change != change.ID || saved.Manifest != change.Manifest {
		return saved.Result, errors.New("retained build does not match the approved change")
	}
	if saved.Result.OK {
		if err := verifyBuild(saved.Result); err != nil {
			return saved.Result, err
		}
	}
	return saved.Result, nil
}

func verifyBuild(result host.SystemBuildResult) error {
	if result.Binary == "" || result.Web == "" || result.SourceDigest == "" {
		return errors.New("a release needs a binary, workspace, and complete source snapshot")
	}
	if got := systembuild.DigestFile(filepath.Join(result.Work, ".build", "platform-server")); got != result.Binary {
		return errors.New("retained binary digest does not match the build")
	}
	web, err := systembuild.DigestTree(filepath.Join(result.Work, "web", "apps", "workspace", "dist"))
	if err != nil {
		return err
	}
	if web != result.Web {
		return errors.New("retained workspace digest does not match the build")
	}
	return systembuild.VerifySource(result.Source, result.SourceDigest)
}

func retainBuild(root string, change changeView, result host.SystemBuildResult) (host.SystemBuildResult, error) {
	dir := filepath.Join(root, ".build", "system", "builds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return result, err
	}
	target := filepath.Join(dir, buildIdentity(change))
	temp, err := os.MkdirTemp(dir, ".build-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temp)
	if err := os.MkdirAll(filepath.Join(temp, ".build"), 0o755); err != nil {
		return result, err
	}
	if result.OK {
		if err := verifyBuild(result); err != nil {
			return result, err
		}
		if err := copyFile(filepath.Join(result.Work, ".build", "platform-server"), filepath.Join(temp, ".build", "platform-server"), 0o755); err != nil {
			return result, err
		}
		if err := copyTree(filepath.Join(result.Work, "web", "apps", "workspace", "dist"), filepath.Join(temp, "web", "apps", "workspace", "dist")); err != nil {
			return result, err
		}
	}
	result.Work = target
	if err := writeJSON(filepath.Join(temp, ".build", "result.json"), savedBuild{change.ID, change.Manifest, result}); err != nil {
		return result, err
	}
	if err := os.Rename(temp, target); err != nil {
		return result, err
	}
	return result, nil
}

type releaseManifest struct {
	Release      int               `json:"release"`
	Binary       string            `json:"binary"`
	Web          string            `json:"web"`
	Toolchain    string            `json:"toolchain"`
	Work         string            `json:"work,omitempty"`
	Source       string            `json:"source,omitempty"`
	SourceDigest string            `json:"sourceDigest,omitempty"`
	Files        map[string]string `json:"files"`
}

// keep publishes versioned paths from already retained artifacts. Repeating it
// completes interrupted copies; the manifest is written last.
func keep(root string, version int, result host.SystemBuildResult) error {
	dir := filepath.Join(root, ".build", "system")
	stamp := fmt.Sprintf("%04d", version)
	record := filepath.Join(dir, "releases", stamp+".json")
	if raw, err := os.ReadFile(record); err == nil {
		var existing releaseManifest
		if json.Unmarshal(raw, &existing) == nil && existing.Binary == result.Binary && existing.Web == result.Web && existing.SourceDigest == result.SourceDigest {
			if _, err := readRelease(root, version); err == nil {
				return nil
			}
		}
	}
	if err := verifyBuild(result); err != nil {
		return err
	}
	if err := installArtifacts(root, version, filepath.Join(result.Work, ".build", "platform-server"), filepath.Join(result.Work, "web", "apps", "workspace", "dist"), releaseManifest{version, result.Binary, result.Web, result.Toolchain, result.Work, result.Source, result.SourceDigest, result.Manifest}); err != nil {
		return err
	}
	pruneReleases(root, 5)
	return nil
}

func installArtifacts(root string, version int, binary, web string, manifest releaseManifest) error {
	dir := filepath.Join(root, ".build", "system")
	stamp := fmt.Sprintf("%04d", version)
	for _, name := range []string{"bin", "web", "releases"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			return err
		}
	}
	if err := copyFile(binary, filepath.Join(dir, "bin", "platform-server-"+stamp), 0o755); err != nil {
		return err
	}
	target := filepath.Join(dir, "web", stamp)
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := copyTree(web, target); err != nil {
		return err
	}
	manifest.Release = version
	if err := writeJSON(filepath.Join(dir, "releases", stamp+".json"), manifest); err != nil {
		return err
	}
	return nil
}

// Definitions can be a logical predecessor without having built new code.
// Bind its exact source identity to retained code artifacts, preserving the
// global version rather than silently skipping that predecessor.
func resolveRelease(c client, root string, version int) (releaseState, error) {
	state, err := readRelease(root, version)
	if err == nil {
		return state, nil
	}
	if !os.IsNotExist(err) {
		return state, err
	}
	var releases []struct {
		Version      int    `json:"version"`
		SourceDigest string `json:"sourceDigest"`
		Binary       string `json:"binary"`
		Web          string `json:"web"`
	}
	if err := c.call("GET", "/v1/system-releases", nil, &releases); err != nil {
		return state, err
	}
	digest, binary, web := "", "", ""
	for _, release := range releases {
		if release.Version == version {
			digest, binary, web = release.SourceDigest, release.Binary, release.Web
			break
		}
	}
	if digest == "" {
		return state, fmt.Errorf("release %d has no retained source identity", version)
	}
	dir := filepath.Join(root, ".build", "system", "releases")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return state, err
	}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return state, err
		}
		var manifest releaseManifest
		if json.Unmarshal(raw, &manifest) != nil || manifest.SourceDigest != digest || (binary != "" && manifest.Binary != binary) || (web != "" && manifest.Web != web) {
			continue
		}
		candidate, err := readRelease(root, manifest.Release)
		if err != nil {
			return state, err
		}
		// The alias owns its physical paths, so normal retention needs no extra
		// reference registry and pruning another ordinal cannot break rollback.
		manifest.Work = ""
		if err := installArtifacts(root, version, candidate.Binary, candidate.Web, manifest); err != nil {
			return state, err
		}
		return readRelease(root, version)
	}
	return state, fmt.Errorf("release %d source %s has no retained code artifacts", version, digest)
}

// pruneReleases keeps the newest few releases on disk. The running release and
// the one it could roll back to are never pruned, whatever their age: a release
// that cannot be started again is not a rollback target.
func pruneReleases(root string, newest int) {
	dir := filepath.Join(root, ".build", "system")
	entries, err := os.ReadDir(filepath.Join(dir, "releases"))
	if err != nil {
		return
	}
	versions := make([]int, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".json")
		if n, err := strconv.Atoi(name); err == nil {
			versions = append(versions, n)
		}
	}
	slices.Sort(versions)
	slices.Reverse(versions)
	protected := map[int]bool{}
	if raw, err := os.ReadFile(filepath.Join(dir, "current.json")); err == nil {
		var state releaseState
		if json.Unmarshal(raw, &state) == nil {
			protected[state.Release] = true
			protected[state.Serving] = true
			if state.PendingRollback != nil {
				protected[state.PendingRollback.Release] = true
			}
			if state.Previous != nil {
				protected[state.Previous.Release] = true
			}
		}
	}
	for i, version := range versions {
		if i < newest || protected[version] {
			continue
		}
		stamp := fmt.Sprintf("%04d", version)
		// The retained pre-publication copy has the same lifetime as the
		// installed release. Otherwise watch would resurrect a pruned release.
		if raw, err := os.ReadFile(filepath.Join(dir, "releases", stamp+".json")); err == nil {
			var manifest releaseManifest
			if json.Unmarshal(raw, &manifest) == nil && filepath.Clean(filepath.Dir(manifest.Work)) == filepath.Join(dir, "builds") {
				_ = os.RemoveAll(manifest.Work)
			}
		}
		_ = os.Remove(filepath.Join(dir, "releases", stamp+".json"))
		_ = os.Remove(filepath.Join(dir, "bin", "platform-server-"+stamp))
		_ = os.RemoveAll(filepath.Join(dir, "web", stamp))
	}
}

// releaseOf finds the release a change published.
func (c client) releaseOf(change string) (int, error) {
	var releases []struct {
		Version int    `json:"version"`
		Change  string `json:"change"`
	}
	if err := c.call("GET", "/v1/system-releases", nil, &releases); err != nil {
		return 0, err
	}
	for _, release := range releases {
		if release.Change == change {
			return release.Version, nil
		}
	}
	return 0, fmt.Errorf("change %s published no release", change)
}

// export verifies and copies the complete sealed source, independent of build caches.
func export(args []string) error {
	flags := flag.NewFlagSet("export", flag.ExitOnError)
	root := flags.String("root", ".", "repository root")
	release := flags.Int("release", 0, "the release to export")
	out := flags.String("out", "", "directory to write the source into")
	flags.Parse(args)
	if *release == 0 {
		return errors.New("export needs -release")
	}
	if *out == "" {
		*out = filepath.Join(*root, ".build", "system", "exports", fmt.Sprintf("%04d", *release))
	}
	raw, err := os.ReadFile(filepath.Join(*root, ".build", "system", "releases", fmt.Sprintf("%04d.json", *release)))
	if err != nil {
		return err
	}
	var built releaseManifest
	if err := json.Unmarshal(raw, &built); err != nil {
		return err
	}
	if built.Source == "" || built.SourceDigest == "" {
		return errors.New("this historical release has no sealed complete source and cannot be exported as a verified tree")
	}
	if err := systembuild.VerifySource(built.Source, built.SourceDigest); err != nil {
		return err
	}
	source, _ := filepath.Abs(built.Source)
	target, _ := filepath.Abs(*out)
	rel, _ := filepath.Rel(target, source)
	nested, _ := filepath.Rel(source, target)
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") || (!strings.HasPrefix(nested, ".."+string(filepath.Separator)) && nested != "..") {
		return errors.New("export destination must not overlap its sealed source")
	}
	if err := os.RemoveAll(*out); err != nil {
		return err
	}
	if err := systembuild.CopySource(built.Source, *out); err != nil {
		return err
	}
	fmt.Printf("release %04d exported to %s (%d source files verified)\n", *release, *out, len(built.Files))
	return nil
}

func copyFile(source, target string, mode os.FileMode) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, body, mode)
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		dest := filepath.Join(target, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		return copyFile(path, dest, 0o644)
	})
}

func activate(args []string) error {
	flags := flag.NewFlagSet("activate", flag.ExitOnError)
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host base URL")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "access token of a system publisher")
	release := flags.Int("release", 0, "the release version")
	flags.Parse(args)
	if *release == 0 {
		return errors.New("activate needs -release")
	}
	c := client{host: *hostURL, token: *token, tenant: systemscope.Tenant}
	_, err := c.submit("system.release.activate", systemscope.ReleaseType, fmt.Sprintf("%04d", *release), systemscope.App,
		map[string]any{"window": 30})
	return err
}

func finish(args []string) error {
	flags := flag.NewFlagSet("finish", flag.ExitOnError)
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host base URL")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "access token of a system publisher")
	activation := flags.String("activation", "", "the activation record id")
	result := flags.String("result", "", "active, rolled-back or aborted")
	evidence := flags.String("evidence", "", "what the swap observed")
	flags.Parse(args)
	if *activation == "" || *result == "" {
		return errors.New("finish needs -activation and -result")
	}
	c := client{host: *hostURL, token: *token, tenant: systemscope.Tenant}
	_, err := c.submit("system.release.finished", systemscope.ActivationType, *activation, systemscope.App,
		map[string]any{"result": *result, "evidence": *evidence})
	return err
}

// swap stages an existing requested activation. The supervisor alone probes,
// replaces, and reports it, using the activation's actual identity and window.
func swap(args []string) error {
	flags := flag.NewFlagSet("swap", flag.ExitOnError)
	root := flags.String("root", ".", "repository root")
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host base URL")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "access token of a system publisher")
	release := flags.Int("release", 0, "requested release")
	id := flags.String("activation", "", "exact activation id")
	flags.Parse(args)
	if *id == "" && *release == 0 {
		return errors.New("swap needs -activation or -release")
	}
	c := client{host: *hostURL, token: *token, tenant: systemscope.Tenant}
	var activations []activationView
	if err := c.call("GET", "/v1/system-activations", nil, &activations); err != nil {
		return err
	}
	for _, activation := range activations {
		if activation.Result == "requested" && ((*id != "" && activation.ID == *id) || (*id == "" && activation.Release == *release)) {
			if !activation.ready(time.Now()) {
				return errors.New("the activation cancellation window is still open")
			}
			return stageActivation(c, *root, activation)
		}
	}
	return errors.New("no matching requested activation")
}

type changeView struct {
	ID           string `json:"id"`
	State        string `json:"state"`
	Base         int    `json:"base"`
	Release      int    `json:"release"`
	Manifest     string `json:"manifest"`
	SourceDigest string `json:"sourceDigest"`
	Files        []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Deleted bool   `json:"deleted"`
	} `json:"files"`
}

func readChange(c client, id string) (changeView, error) {
	var changes []changeView
	if err := c.call("GET", "/v1/system-changes", nil, &changes); err != nil {
		return changeView{}, err
	}
	for _, change := range changes {
		if change.ID == id {
			return change, nil
		}
	}
	return changeView{}, fmt.Errorf("no change %s in the system tenant", id)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

func randomKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
