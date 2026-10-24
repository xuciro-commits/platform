// Package systembuild materializes an approved system change into its own
// work tree, builds the host and the workspace with fixed commands, and
// reports what it produced. It never runs a script from the repository: the
// repository's Makefile and scripts are editable by a self-hosting change, so
// running them would let a change escape the kernel gate.
package systembuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"platformserver/internal/host"
	"platformserver/internal/systemscope"
)

// Config fixes where a build runs. Root is the repository whose toolchain and
// dependency caches are reused; Work is the disposable tree per change.
type Config struct {
	Root    string
	Work    string
	Timeout time.Duration
	// SkipWeb skips the workspace build when Node is unavailable; the host
	// still builds and reports the missing evidence.
	SkipWeb bool
}

// Fixed commands. Nothing else is executed, whatever the change edited.
var (
	HostCommand = []string{"go", "build", "-trimpath", "-o", "{out}", "./cmd/platform-server"}
	// The workspace is built by vite itself, never by the repository's build
	// script: a change may rewrite that script, and the gate would not stop it.
	WebCommand = []string{"pnpm", "--dir", "apps/workspace", "exec", "vite", "build"}
)

// Materialize seeds a fresh work tree from the repository the running host was
// built from, overlays the change's frozen files, and removes its deletes. The
// seed is only a starting point: every byte a change touches is its own, and
// nothing outside the work tree is ever written. It re-checks the kernel gate,
// so a tampered change never reaches a compiler.
func Materialize(cfg Config, files map[string]string, deleted []string) (string, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if err := systemscope.Check(append(paths, deleted...)); err != nil {
		return "", err
	}
	work := filepath.Join(cfg.Work, time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", err
	}
	if err := seedTree(cfg.Root, work); err != nil {
		return "", err
	}
	copyWorkspace(cfg.Root, work)
	for _, p := range paths {
		target := filepath.Join(work, filepath.FromSlash(p))
		if !strings.HasPrefix(target, work+string(os.PathSeparator)) {
			return "", fmt.Errorf("%s: outside the work tree", p)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, []byte(files[p]), 0o644); err != nil {
			return "", err
		}
	}
	for _, p := range deleted {
		if err := os.RemoveAll(filepath.Join(work, filepath.FromSlash(p))); err != nil {
			return "", err
		}
	}
	return work, nil
}

// seedTree copies the repository's source into work, skipping what a build
// regenerates or must not inherit: version control, earlier build trees,
// dependency directories and web output. Dependency caches are linked in after
// the copy, so a build can run offline against the machine's cache without the
// seed carrying node_modules.
func seedTree(root, work string) error {
	skip := map[string]bool{".git": true, ".build": true, "node_modules": true, "dist": true,
		".next": true, ".turbo": true, "coverage": true}
	root = filepath.Clean(root)
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != root && skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode&0o111 != 0 {
			mode = 0o755
		} else {
			mode = 0o644
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, mode)
	})
}

// copyWorkspace carries the machine's Go workspace into the copy, naming the
// copy's module directories. Without it the build would compile whatever the
// original workspace points at instead of the change; with it, a deployment
// that runs without a workspace is unaffected.
func copyWorkspace(root, work string) {
	source := filepath.Join(root, ".build", "sandbox-go", "go.work")
	body, err := os.ReadFile(source)
	if err != nil {
		return
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil || real == "" {
		real = root
	}
	// One prefix, replaced once: the copy's path contains the original, so a
	// second replacement would nest the copy inside itself.
	target := filepath.Join(work, ".build", "sandbox-go", "go.work")
	if fileExists(target) {
		return
	}
	text := strings.ReplaceAll(string(body), real, work)
	if os.MkdirAll(filepath.Dir(target), 0o755) != nil {
		return
	}
	_ = os.WriteFile(target, []byte(text), 0o644)
}

// Build materializes and builds one change. A failed step returns evidence
// rather than an error, so the change records exactly why it did not build.
func Build(cfg Config, req host.SystemBuildRequest) (host.SystemBuildResult, error) {
	started := time.Now()
	result := host.SystemBuildResult{Toolchain: toolchain(), Manifest: digests(req.Files)}
	work, err := Materialize(cfg, req.Files, req.Deleted)
	if err != nil {
		result.Diagnostics = err.Error()
		return result, err
	}
	result.Work = work
	linkDependencies(cfg.Root, work)
	out := filepath.Join(work, ".build", "platform-server")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return result, err
	}
	hostBuild := replace(HostCommand, "{out}", out)
	if diagnostics, err := run(cfg, work, module(work), hostBuild); err != nil {
		result.Diagnostics = "host build failed: " + diagnostics
		result.Duration = time.Since(started)
		return result, nil
	}
	result.Binary = digestFile(out)
	if result.Binary == "" {
		result.Diagnostics = "the host build produced no binary"
		result.Duration = time.Since(started)
		return result, nil
	}
	var web strings.Builder
	if !cfg.SkipWeb {
		if diagnostics, err := run(cfg, work, filepath.Join(work, "web"), command(cfg, WebCommand)); err != nil {
			result.Diagnostics = "workspace build failed: " + diagnostics
			result.Duration = time.Since(started)
			return result, nil
		}
		dist := filepath.Join(work, "web", "apps", "workspace", "dist")
		web.WriteString(digestFile(dist))
	}
	result.Web = web.String()
	result.OK = true
	result.Duration = time.Since(started)
	return result, nil
}

// digests is the change's own path→digest list, recorded with the release so
// an export can prove the tree it copies is the tree that was built.
func digests(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for path, body := range files {
		sum := sha256.Sum256([]byte(body))
		out[path] = hex.EncodeToString(sum[:])
	}
	return out
}

// RequestActivation leaves the activation order where the updater looks for
// it. The host never swaps its own process.
func RequestActivation(root string, req host.SystemActivationRequest) error {
	dir := filepath.Join(root, ".build", "system")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(map[string]any{
		"release": req.Release, "previous": req.Previous, "manifest": req.Manifest,
		"requestedAt": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "activate.json"), append(body, '\n'), 0o644)
}

// Activations lists requested activations, newest first (for the updater).
func Activations(root string) ([]host.SystemActivationRequest, error) {
	var out []host.SystemActivationRequest
	body, err := os.ReadFile(filepath.Join(root, ".build", "system", "activate.json"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var one host.SystemActivationRequest
	if err := json.Unmarshal(body, &one); err != nil {
		return nil, err
	}
	return append(out, one), nil
}

// linkDependencies reuses the machine's installed node_modules instead of
// restoring them: the workspace build is a type check of the change, not a
// dependency install, and the lockfile is part of the change's evidence.
func linkDependencies(root, work string) {
	for _, rel := range []string{"node_modules", "web/node_modules", "web/apps/workspace/node_modules"} {
		source := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(source); err != nil {
			continue
		}
		target := filepath.Join(work, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.Symlink(source, target)
	}
}

// module is the work tree's host module, where the fixed Go commands run.
func module(work string) string { return filepath.Join(work, "capabilities", "server") }

// withoutEnv drops every entry naming key, so one value of it survives.
func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// command uses the package manager the machine has: pnpm itself, or corepack
// when only that is installed.
func command(cfg Config, argv []string) []string {
	if argv[0] != "pnpm" {
		return argv
	}
	if _, err := exec.LookPath("pnpm"); err == nil {
		return argv
	}
	return append([]string{"corepack"}, argv...)
}

// output runs one command in dir and returns its trimmed standard output.
func output(dir string, argv ...string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	raw, err := cmd.Output()
	return strings.TrimSpace(string(raw)), err
}

func run(cfg Config, work, dir string, argv []string) (string, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	// Reuse the machine's toolchain and caches exactly as they are, but point
	// a workspace at the copy: the repository's go.work would compile the
	// repository, not the change. No repository script is ever the entry point.
	env := withoutEnv(os.Environ(), "GOWORK")
	if workspace := filepath.Join(work, ".build", "sandbox-go", "go.work"); fileExists(workspace) {
		env = append(env, "GOWORK="+workspace)
	}
	cmd.Env = append(env, "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	text := string(output)
	// A command that never started (no compiler on PATH) says so in the
	// error, and the change must record it: an empty diagnostic is a failed
	// build with no reason, which nobody can act on.
	if err != nil && strings.TrimSpace(text) == "" {
		text = err.Error()
	}
	if len(text) > 4000 {
		text = text[len(text)-4000:]
	}
	if ctx.Err() != nil {
		return text + "\nthe build exceeded its time budget", ctx.Err()
	}
	return text, err
}

func replace(argv []string, key, value string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = strings.ReplaceAll(a, key, value)
	}
	return out
}

func digestFile(p string) string {
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	h := sha256.New()
	err := filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(p, path)
		fmt.Fprintf(h, "%x  %s\n", sha256.Sum256(body), filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func toolchain() string {
	return fmt.Sprintf("%s %s", runtime.Version(), runtime.GOOS+"/"+runtime.GOARCH)
}
