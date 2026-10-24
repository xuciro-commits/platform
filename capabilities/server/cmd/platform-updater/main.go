// Command platform-updater builds and swaps the platform's own releases. It is
// the only process that runs a compiler or replaces the running host: the host
// records a request and reports the outcome, and never rebuilds itself.
//
// Three steps, each a separate invocation, so an operator (or a Make target)
// sees exactly what happened:
//
//	platform-updater build    -host URL -token T -change <id>
//	platform-updater activate -host URL -token T -release <n>
//	platform-updater swap     -root DIR -health URL
//
// A build reads the approved change from the system tenant, materializes its
// frozen files into a fresh work tree, builds with the fixed commands, and
// reports the evidence back as a decision. `swap` starts the built host on a
// probe address, checks it, replaces the running one, and rolls back when the
// new one fails.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
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
platform-updater swap     -root DIR -release N [-addr HOST:PORT] [-health-timeout 30s]
platform-updater serve    -root DIR -addr HOST:PORT [-web DIR] [-data DIR] [-tenants FILE] [-host URL -token T]
platform-updater watch    -root DIR -host URL -token T [-every 5s] [-addr HOST:PORT] [-data DIR]
platform-updater export   -root DIR -release N [-out DIR]

build compiles an approved change and reports what it produced.
activate asks the host for a window before the swap.
swap installs a built release as the one the supervisor will run.
serve runs the host, and replaces it when a newer release is installed.
watch builds every approved change and swaps every requested activation it
      sees, so an approval in the Platform becomes a release with no terminal.
finish reports a swap's outcome to the system tenant.
export copies a release's whole work tree out, after checking every file it
      froze against the digest recorded when the release was built.
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
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, response.Status, truncate(string(raw), 400))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// submit sends one decision for the system app and returns the accepted record.
func (c client) submit(schema, typ, id, authority string, payload map[string]any) (map[string]any, error) {
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
		"idempotencyKey": randomKey(),
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
	skipWeb := flags.Bool("skip-web", false, "build only the host binary")
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
	return buildFor(c, *root, change, *skipWeb)
}

// watch is the loop that closes self-hosting: an approval is a record, and
// this process turns records into releases without anybody typing a command.
// It builds each approved change once, then the release a publisher asks for.
// The supervisor still owns the swap: watch only writes what it decided, the
// same file `swap` writes, after proving the candidate healthy.
func watch(args []string) error {
	flags := flag.NewFlagSet("watch", flag.ExitOnError)
	root := flags.String("root", ".", "repository root")
	hostURL := flags.String("host", "http://127.0.0.1:8495", "host base URL")
	token := flags.String("token", os.Getenv("PLATFORM_TOKEN"), "access token of a system publisher")
	every := flags.Duration("every", 5*time.Second, "how often to look for work")
	addr := flags.String("addr", "127.0.0.1:8495", "the host's listen address")
	data := flags.String("data", "", "the host's data directory (its signing key is read, never written)")
	once := flags.Bool("once", false, "handle the work visible now, then exit")
	flags.Parse(args)
	c := client{host: *hostURL, token: *token, tenant: systemscope.Tenant}
	built := map[string]bool{}
	swapped := map[string]bool{}
	for {
		if err := watchOnce(c, *root, *addr, *data, built, swapped); err != nil {
			fmt.Fprintln(os.Stderr, "platform-updater watch:", err)
		}
		if *once {
			return nil
		}
		time.Sleep(*every)
	}
}

func watchOnce(c client, root, addr, data string, built, swapped map[string]bool) error {
	var changes []changeView
	if err := c.call("GET", "/v1/system-changes", nil, &changes); err != nil {
		return err
	}
	for _, change := range changes {
		if change.State != "approved" || built[change.ID] {
			continue
		}
		// A failed build is final for that change: a retry is another change,
		// so a compiler error cannot be looped over.
		built[change.ID] = true
		if err := buildFor(c, root, change, false); err != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n", change.ID, err)
		}
	}
	var activations []struct {
		ID      string `json:"id"`
		Release int    `json:"release"`
		Result  string `json:"result"`
	}
	if err := c.call("GET", "/v1/system-activations", nil, &activations); err != nil {
		return err
	}
	for _, activation := range activations {
		if activation.Result != "requested" || swapped[activation.ID] {
			continue
		}
		swapped[activation.ID] = true
		if err := swap([]string{"-root", root, "-release", fmt.Sprint(activation.Release), "-addr", addr, "-data", data}); err != nil {
			fmt.Fprintf(os.Stderr, "swap %s: %v\n", activation.ID, err)
		}
	}
	return nil
}

// buildFor compiles one approved change and reports what it produced, using
// the same path as the `build` command.
func buildFor(c client, root string, change changeView, skipWeb bool) error {
	files := map[string]string{}
	var deleted []string
	for _, f := range change.Files {
		if f.Deleted {
			deleted = append(deleted, f.Path)
			continue
		}
		files[f.Path] = f.Content
	}
	result, err := systembuild.Build(systembuild.Config{Root: root, Work: filepath.Join(root, ".build", "system", "work"), SkipWeb: skipWeb},
		host.SystemBuildRequest{Change: change.ID, Files: files, Deleted: deleted})
	if err != nil {
		return err
	}
	if _, err := c.submit("system.change.built", systemscope.ChangeType, change.ID, systemscope.App, map[string]any{
		"ok": result.OK, "binary": result.Binary, "web": result.Web, "toolchain": result.Toolchain, "diagnostics": result.Diagnostics,
	}); err != nil {
		return err
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

// keep copies the built artifacts into the release set the updater starts from
// and records the manifest beside them. Only the newest five are kept.
func keep(root string, version int, result host.SystemBuildResult) error {
	bin := filepath.Join(root, ".build", "system", "bin")
	dist := filepath.Join(root, ".build", "system", "web")
	manifest := filepath.Join(root, ".build", "system", "releases")
	for _, dir := range []string{bin, dist, manifest} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	source := filepath.Join(result.Work, ".build", "platform-server")
	target := filepath.Join(bin, fmt.Sprintf("platform-server-%04d", version))
	if err := copyFile(source, target, 0o755); err != nil {
		return err
	}
	sourceDist := filepath.Join(result.Work, "web", "apps", "workspace", "dist")
	targetDist := filepath.Join(dist, fmt.Sprintf("%04d", version))
	if _, err := os.Stat(sourceDist); err == nil {
		if err := copyTree(sourceDist, targetDist); err != nil {
			return err
		}
	}
	body, err := json.MarshalIndent(map[string]any{"release": version, "binary": result.Binary, "web": result.Web,
		"toolchain": result.Toolchain, "work": result.Work, "files": result.Manifest}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(manifest, fmt.Sprintf("%04d.json", version)), append(body, '\n'), 0o644); err != nil {
		return err
	}
	pruneReleases(root, 5)
	return nil
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

// export writes a release's complete work tree to -out. The tree is the seed
// the build copied plus the change's frozen files; every frozen file is checked
// against the digest recorded at build time, so an export proves it is the
// bytes that were built and not merely the bytes on disk now.
func export(args []string) error {
	flags := flag.NewFlagSet("export", flag.ExitOnError)
	root := flags.String("root", ".", "repository root")
	release := flags.Int("release", 0, "the release to export")
	out := flags.String("out", "", "directory to write the tree into (default .build/system/exports/NNNN)")
	flags.Parse(args)
	if *release == 0 {
		return errors.New("export needs -release")
	}
	if *out == "" {
		*out = filepath.Join(*root, ".build", "system", "exports", fmt.Sprintf("%04d", *release))
	}
	record := filepath.Join(*root, ".build", "system", "releases", fmt.Sprintf("%04d.json", *release))
	raw, err := os.ReadFile(record)
	if err != nil {
		return fmt.Errorf("release %d has no build record: %w", *release, err)
	}
	var built struct {
		Work  string            `json:"work"`
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(raw, &built); err != nil {
		return err
	}
	if built.Work == "" {
		return fmt.Errorf("release %d's record names no work tree", *release)
	}
	for path, want := range built.Files {
		body, err := os.ReadFile(filepath.Join(built.Work, filepath.FromSlash(path)))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("%s: digest %s, the release recorded %s", path, got, want)
		}
	}
	if err := os.RemoveAll(*out); err != nil {
		return err
	}
	if err := copySourceTree(built.Work, *out); err != nil {
		return err
	}
	count := 0
	_ = filepath.Walk(*out, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			count++
		}
		return nil
	})
	fmt.Printf("release %04d exported to %s (%d files, %d frozen files verified)\n", *release, *out, count, len(built.Files))
	return nil
}

// copySourceTree copies a work tree as source: no version control, no build
// output and no linked dependency caches, so the export is what a person would
// commit rather than what a build left lying around.
func copySourceTree(source, target string) error {
	skip := map[string]bool{".git": true, ".build": true, "node_modules": true, "dist": true}
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			if rel != "." && skip[info.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, rel), 0o755)
		}
		return copyFile(path, filepath.Join(target, rel), 0o644)
	})
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

// swap installs a built release as the one the supervisor runs. It proves the
// binary starts and reports itself before the file names it, so an unhealthy
// release never reaches the running host.
func swap(args []string) error {
	flags := flag.NewFlagSet("swap", flag.ExitOnError)
	root := flags.String("root", ".", "repository root")
	addr := flags.String("addr", "127.0.0.1:8495", "the host's listen address")
	release := flags.Int("release", 0, "the release to run")
	data := flags.String("data", "", "the host's data directory (its signing key is read, never written)")
	timeout := flags.Duration("health-timeout", 45*time.Second, "how long the candidate may take to become healthy")
	flags.Parse(args)
	if *release == 0 {
		return errors.New("swap needs -release")
	}
	dir := filepath.Join(*root, ".build", "system")
	binary := filepath.Join(dir, "bin", fmt.Sprintf("platform-server-%04d", *release))
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("release %d has no binary at %s", *release, binary)
	}
	key := ""
	if *data != "" {
		key = filepath.Join(*data, "idp.key")
	}
	if err := probe(binary, *addr, *release, *timeout, key); err != nil {
		return fmt.Errorf("release %d is not healthy, nothing was installed: %w", *release, err)
	}
	current := filepath.Join(dir, "current.json")
	var state releaseState
	if raw, err := os.ReadFile(current); err == nil {
		_ = json.Unmarshal(raw, &state)
	}
	digest := sha256File(binary)
	next := map[string]any{"release": *release, "binary": binary, "digest": digest}
	if state.Release != 0 {
		next["previous"] = map[string]any{"release": state.Release, "binary": state.Binary}
	}
	body, _ := json.MarshalIndent(next, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(current, append(body, '\n'), 0o644); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, "activate.json"))
	fmt.Printf("release %04d is installed; the supervisor swaps it in\n", *release)
	return nil
}

// probe starts a candidate with a scratch data directory and waits for its
// health endpoint to report the release it was started as. It reads the
// host's signing key without writing anything: the probe's own journal is a
// temporary directory, so a bad release cannot touch the running data.
func probe(binary, addr string, release int, timeout time.Duration, key string) error {
	dir, err := os.MkdirTemp("", "platform-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	port := sparePort(addr)
	args := []string{"-addr", port, "-profile", "lightweight", "-data", dir, "-release", fmt.Sprint(release)}
	if key != "" {
		args = append(args, "-idp-key", key)
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	}()
	return waitHealth("http://"+port+"/healthz", release, timeout)
}

func sparePort(addr string) string {
	hostPart, _, ok := strings.Cut(addr, ":")
	if !ok {
		hostPart = "127.0.0.1"
	}
	return hostPart + ":18510"
}

type changeView struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Files []struct {
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

// sha256File is the identity of the binary the supervisor will run. The host
// reports it, so "the release I installed" is a fact and not a label.
func sha256File(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
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
