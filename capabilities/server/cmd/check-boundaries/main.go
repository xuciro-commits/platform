// Command check-boundaries checks source imports even when build tags exclude
// a production file on the checking machine. Counts and unrelated deletions
// cannot excuse a reverse dependency or an anonymous public runtime probe.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	root := flag.String("root", "../..", "repository root")
	flag.Parse()
	var appModules []string
	modules, err := filepath.Glob(filepath.Join(*root, "apps", "*", "server", "go.mod"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "boundary:", err)
		os.Exit(1)
	}
	for _, module := range modules {
		raw, err := os.ReadFile(module)
		if err != nil {
			fmt.Fprintln(os.Stderr, "boundary:", err)
			os.Exit(1)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" {
				appModules = append(appModules, fields[1])
			}
		}
	}
	var failures []string
	for _, area := range []string{"capabilities/server", "apps", "protocols"} {
		err := filepath.WalkDir(filepath.Join(*root, area), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "vendor" || entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(*root, path)
			if err != nil {
				return err
			}
			set := token.NewFileSet()
			file, err := parser.ParseFile(set, path, nil, 0)
			if err != nil {
				return err
			}
			for _, finding := range checkFile(filepath.ToSlash(rel), file, set, appModules...) {
				failures = append(failures, finding)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "boundary:", err)
			os.Exit(1)
		}
	}
	sort.Strings(failures)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, "boundary:", failure)
	}
	if len(failures) != 0 {
		os.Exit(1)
	}
	fmt.Println("source boundaries ok: every production Go file checked")
}

func checkFile(path string, file *ast.File, set *token.FileSet, appModules ...string) []string {
	if strings.Contains(path, "/cmd/") {
		return nil // binaries and generators compose compiled implementations
	}
	root := strings.HasPrefix(path, "capabilities/server/") && !strings.Contains(strings.TrimPrefix(path, "capabilities/server/"), "/")
	public := strings.HasPrefix(path, "capabilities/server/platform/")
	internal := strings.HasPrefix(path, "capabilities/server/internal/host/")
	external := strings.HasPrefix(path, "apps/") || strings.HasPrefix(path, "protocols/")
	harness := external && strings.HasSuffix(file.Name.Name, "test")
	self := ""
	if rest, ok := strings.CutPrefix(path, "apps/"); ok {
		self, _, _ = strings.Cut(rest, "/")
	}
	app := ""
	if rest, ok := strings.CutPrefix(path, "capabilities/server/apps/"); ok {
		app, _, _ = strings.Cut(rest, "/")
	}
	var failures []string
	report := func(node ast.Node, rule string) {
		failures = append(failures, fmt.Sprintf("%s:%d: %s", path, set.Position(node.Pos()).Line, rule))
	}
	hostNames := map[string]bool{}
	for _, imported := range file.Imports {
		name, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			report(imported, "invalid import")
			continue
		}
		if root && strings.HasPrefix(name, "platformserver/apps/") {
			report(imported, "runtime imports concrete platform app "+name)
		}
		if (public || internal) && (name == "platformserver" || strings.HasPrefix(name, "platformserver/apps/")) {
			report(imported, "owner contract depends on implementation "+name)
		}
		if external && !harness {
			if name == "platformserver" || strings.HasPrefix(name, "platformserver/apps/") || strings.HasPrefix(name, "platformserver/internal/") {
				report(imported, "business owner imports runtime or private host implementation "+name)
			}
			for _, other := range appModules {
				if other != self && (name == other || strings.HasPrefix(name, other+"/")) {
					report(imported, "business owner or protocol imports app "+name)
				}
			}
		}
		if app != "" {
			if name == "platformserver" {
				report(imported, "platform owner imports its host runtime")
			}
			if rest, ok := strings.CutPrefix(name, "platformserver/apps/"); ok {
				other, _, _ := strings.Cut(rest, "/")
				if other != app {
					report(imported, "platform owner imports sibling "+name)
				}
			}
		}
		if name == "platformserver/internal/host" {
			alias := "host"
			if imported.Name != nil {
				alias = imported.Name.Name
			}
			if app != "" && alias == "." {
				report(imported, "platform owner must name its host consumer role import")
			}
			hostNames[alias] = true
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if root {
			if assertion, ok := node.(*ast.TypeAssertExpr); ok {
				if pointer, ok := assertion.Type.(*ast.StarExpr); ok {
					if name, ok := pointer.X.(*ast.Ident); ok && (name.Name == "Console" || name.Name == "Agents") {
						report(assertion, "runtime asserts a concrete state owner; consume its named role")
					}
				}
			}
		}
		if public {
			if assertion, ok := node.(*ast.TypeAssertExpr); ok {
				if _, anonymous := assertion.Type.(*ast.InterfaceType); anonymous {
					report(assertion, "public API probes an anonymous runtime capability; declare a named owner role")
				}
			}
		}
		if app != "" {
			if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel.Name == "Host" {
				if name, ok := selector.X.(*ast.Ident); ok && hostNames[name.Name] {
					report(selector, "platform owner receives the composition Host instead of its consumer role")
				}
			}
		}
		return true
	})
	return failures
}
