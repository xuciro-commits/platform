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
}

// Materialize copies the immutable running baseline and overlays only the
// approved change. Later edits of the deployment repository cannot enter it.
func Materialize(cfg Config, source Source, files map[string]string, deleted []string) (string, error) {
	paths := make([]string, 0, len(files))
	for p, content := range files {
		if err := systemscope.CheckContent(p, content); err != nil {
			return "", err
		}
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
	if err := CopySource(source.Root, work); err != nil {
		return "", err
	}
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

// Build materializes and builds one change. A failed step returns evidence
// rather than an error, so the change records exactly why it did not build.
func Build(cfg Config, req host.SystemBuildRequest) (host.SystemBuildResult, error) {
	started := time.Now()
	result := host.SystemBuildResult{Toolchain: toolchain()}
	source, err := OpenSource(req.Source)
	if err == nil && source.Digest != req.SourceDigest {
		err = fmt.Errorf("the approved source baseline differs")
	}
	if err != nil {
		result.Diagnostics = "source baseline: " + err.Error()
		return result, nil
	}
	work, err := Materialize(cfg, source, req.Files, req.Deleted)
	if err != nil {
		result.Diagnostics = err.Error()
		return result, err
	}
	result.Work = work
	sealed, err := CaptureSource(work, filepath.Join(cfg.Root, ".build", "system", "sources"))
	if err != nil {
		return result, err
	}
	result.Source, result.SourceDigest = sealed.Root, sealed.Digest
	result.Manifest = map[string]string{}
	for path, file := range sealed.Files {
		result.Manifest[path] = file.Digest
	}
	out := filepath.Join(work, ".build", "platform-server")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return result, err
	}
	hostBuild := []string{"go", "build", "-trimpath", "-o", out, "./cmd/platform-server"}
	if diagnostics, err := run(cfg, work, module(work), hostBuild); err != nil {
		result.Diagnostics = "host build failed: " + diagnostics
		result.Duration = time.Since(started)
		return result, nil
	}
	if diagnostics, err := run(cfg, work, module(work), []string{"go", "vet", "./..."}); err != nil {
		result.Diagnostics = "host vet failed: " + diagnostics
		result.Duration = time.Since(started)
		return result, nil
	}
	result.Binary = DigestFile(out)
	if result.Binary == "" {
		result.Diagnostics = "the host build produced no binary"
		result.Duration = time.Since(started)
		return result, nil
	}
	if diagnostics, err := buildWeb(cfg, work); err != nil {
		result.Diagnostics = "workspace build failed: " + diagnostics
		result.Duration = time.Since(started)
		return result, nil
	}
	result.Web = digestFile(filepath.Join(work, "web", "apps", "workspace", "dist"))
	if result.Web == "" {
		result.Diagnostics = "the workspace build produced no resources"
		return result, nil
	}
	for _, cmd := range [][]string{{"node", "--version"}, command([]string{"pnpm", "--version"})} {
		if version, err := output(cfg.Root, cmd...); err == nil {
			result.Toolchain += "; " + cmd[0] + " " + version
		}
	}
	result.OK = true
	result.Duration = time.Since(started)
	return result, nil
}

// Dependency resolution consumes the approved lockfile but never runs its
// lifecycle hooks, pnpmfile, package scripts, Vite config or PostCSS config.
func buildWeb(cfg Config, work string) (string, error) {
	web := filepath.Join(work, "web")
	install := command([]string{"pnpm", "install", "--frozen-lockfile", "--ignore-scripts", "--ignore-pnpmfile"})
	if diagnostics, err := run(cfg, work, web, install); err != nil {
		return "dependency restore: " + diagnostics, err
	}
	tsc := filepath.Join(web, "apps", "workspace", "node_modules", "typescript", "bin", "tsc")
	projects, err := filepath.Glob(filepath.Join(web, "packages", "*", "tsconfig.json"))
	if err != nil {
		return "typecheck projects", err
	}
	projects = append(projects, filepath.Join(web, "apps", "workspace", "tsconfig.json"))
	for _, project := range projects {
		if diagnostics, err := run(cfg, work, web, []string{"node", tsc, "-p", project}); err != nil {
			return "typecheck " + project + ": " + diagnostics, err
		}
	}
	plugin := filepath.Join(web, "apps", "workspace", "node_modules", "@vitejs", "plugin-react", "dist", "index.js")
	encoded, _ := json.Marshal(plugin)
	config := filepath.Join(work, ".build", "updater.vite.mjs")
	body := "import react from " + string(encoded) + ";\nexport default { plugins:[react()], clearScreen:false, envDir:false, css:{postcss:{}}, build:{target:'safari17',outDir:'dist'} };\n"
	if err := os.WriteFile(config, []byte(body), 0o644); err != nil {
		return "bundler config", err
	}
	vite := filepath.Join(web, "apps", "workspace", "node_modules", "vite", "bin", "vite.js")
	return run(cfg, work, filepath.Join(web, "apps", "workspace"), []string{"node", vite, "build", "--config", config, "--configLoader", "native"})
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
func command(argv []string) []string {
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
	// a workspace at the copy: an external go.work must not compile the
	// mutable deployment repository instead of the change. No repository script is ever the entry point.
	env := withoutEnv(os.Environ(), "GOWORK")
	if workspace := filepath.Join(work, "go.work"); fileExists(workspace) {
		env = append(env, "GOWORK="+workspace)
	} else {
		env = append(env, "GOWORK=off")
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
