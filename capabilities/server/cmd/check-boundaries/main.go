package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed baseline.json
var baselineJSON []byte

type sourceInventory struct {
	Packages   map[string]int      `json:"packages"`
	Duplicates map[string][]string `json:"duplicates"`
	Visual     map[string]string   `json:"visual"`
	Retiring   map[string][]string `json:"retiring"`
}

func main() {
	root := flag.String("root", "../..", "repository root")
	inventoryOnly := flag.Bool("inventory", false, "print observed structural debt without changing its baseline")
	flag.Parse()
	inventory, failures, err := inspectSources(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "boundary:", err)
		os.Exit(1)
	}
	if *inventoryOnly {
		data, _ := json.MarshalIndent(inventory, "", "  ")
		fmt.Println(string(data))
	} else {
		var baseline sourceInventory
		if err := json.Unmarshal(baselineJSON, &baseline); err != nil {
			fmt.Fprintln(os.Stderr, "boundary: invalid structural baseline:", err)
			os.Exit(1)
		}
		failures = append(failures, checkInventory(inventory, baseline)...)
		previous, err := committedBaselines(*root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "boundary:", err)
			os.Exit(1)
		}
		for _, prior := range previous {
			failures = append(failures, checkRatchet(prior, baseline)...)
		}
	}
	sort.Strings(failures)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, "boundary:", failure)
	}
	if len(failures) != 0 {
		os.Exit(1)
	}
	if !*inventoryOnly {
		fmt.Println("source boundaries ok: imports, structural budgets, duplicate files and visual declarations checked")
	}
}

func inspectSources(root string) (sourceInventory, []string, error) {
	inventory := sourceInventory{Packages: map[string]int{}, Duplicates: map[string][]string{}, Visual: map[string]string{}, Retiring: map[string][]string{}}
	var failures []string
	for _, area := range []string{"capabilities/server"} {
		err := filepath.WalkDir(filepath.Join(root, area), func(path string, entry fs.DirEntry, err error) error {
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
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			set := token.NewFileSet()
			file, err := parser.ParseFile(set, path, nil, 0)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			dir := filepath.ToSlash(filepath.Dir(rel))
			inventory.Packages[dir]++
			if !strings.Contains(rel, "/cmd/") && entry.Name() != "doc.go" {
				inventory.Duplicates[entry.Name()] = append(inventory.Duplicates[entry.Name()], rel)
			}
			if dir == "capabilities/server/internal/construction" || dir == "capabilities/server/internal/host" {
				inventory.Retiring[dir] = append(inventory.Retiring[dir], rel)
			}
			for key, digest := range visualDeclarations(rel, file, set) {
				inventory.Visual[key] = digest
			}
			for _, finding := range checkFile(rel, file, set) {
				failures = append(failures, finding)
			}
			return nil
		})
		if err != nil {
			return inventory, nil, err
		}
	}
	for dir, count := range inventory.Packages {
		if count <= 25 {
			delete(inventory.Packages, dir)
		}
	}
	for name, paths := range inventory.Duplicates {
		if len(paths) < 2 {
			delete(inventory.Duplicates, name)
		} else {
			sort.Strings(paths)
		}
	}
	for _, paths := range inventory.Retiring {
		sort.Strings(paths)
	}
	return inventory, failures, nil
}

func checkInventory(current, baseline sourceInventory) []string {
	var failures []string
	for dir, count := range current.Packages {
		if limit, exists := baseline.Packages[dir]; !exists || count > limit {
			failures = append(failures, fmt.Sprintf("%s: %d production Go files exceed package budget %d", dir, count, max(25, limit)))
		}
	}
	for dir, limit := range baseline.Packages {
		if current.Packages[dir] < limit {
			failures = append(failures, dir+": lower or remove the stale package budget")
		}
	}
	compareSets := func(kind string, current, baseline map[string][]string) {
		for key, paths := range current {
			for _, path := range paths {
				if !contains(baseline[key], path) {
					failures = append(failures, path+": new "+kind+" ("+key+")")
				}
			}
		}
		for key, paths := range baseline {
			for _, path := range paths {
				if !contains(current[key], path) {
					failures = append(failures, path+": remove stale "+kind+" baseline entry")
				}
			}
		}
	}
	compareSets("cross-package filename", current.Duplicates, baseline.Duplicates)
	compareSets("file in retiring directory", current.Retiring, baseline.Retiring)
	for key, digest := range current.Visual {
		if baseline.Visual[key] != digest {
			failures = append(failures, key+": new or changed backend visual declaration; move presentation to the frontend")
		}
	}
	for key := range baseline.Visual {
		if _, exists := current.Visual[key]; !exists {
			failures = append(failures, key+": remove stale visual declaration baseline entry")
		}
	}
	return failures
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func checkRatchet(previous, next sourceInventory) []string {
	var failures []string
	for dir, limit := range next.Packages {
		if limit > previous.Packages[dir] {
			failures = append(failures, dir+": package budget may only decrease")
		}
	}
	for key, digest := range next.Visual {
		if previous.Visual[key] != digest {
			failures = append(failures, key+": visual debt baseline may only shrink")
		}
	}
	for _, pair := range [][2]map[string][]string{{previous.Duplicates, next.Duplicates}, {previous.Retiring, next.Retiring}} {
		for key, paths := range pair[1] {
			for _, path := range paths {
				if !contains(pair[0][key], path) {
					failures = append(failures, path+": structural debt baseline may only shrink")
				}
			}
		}
	}
	return failures
}

func committedBaselines(root string) ([]sourceInventory, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		return nil, nil
	}
	const path = "capabilities/server/cmd/check-boundaries/baseline.json"
	listed, err := exec.Command("git", "-C", root, "ls-tree", "--name-only", "HEAD", "--", path).Output()
	if err != nil {
		return nil, fmt.Errorf("read committed boundary baseline: %w", err)
	}
	if len(bytes.TrimSpace(listed)) == 0 {
		return nil, nil
	}
	var previous []sourceInventory
	for _, revision := range []string{"HEAD", "HEAD^"} {
		if revision == "HEAD^" && exec.Command("git", "-C", root, "cat-file", "-e", revision+":"+path).Run() != nil {
			continue
		}
		data, err := exec.Command("git", "-C", root, "show", revision+":"+path).Output()
		if err != nil {
			return nil, fmt.Errorf("read committed boundary baseline: %w", err)
		}
		var snapshot sourceInventory
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return nil, err
		}
		previous = append(previous, snapshot)
	}
	return previous, nil
}

func visualDeclarations(path string, file *ast.File, set *token.FileSet) map[string]string {
	out := map[string]string{}
	if strings.Contains(path, "/cmd/check-boundaries/") {
		return out
	}
	terms := []string{"spacer", "separator", "buttongroup", "kanban", "gantt", "heatmap", "sparkline", "treemap", "gauge"}
	for _, declaration := range file.Decls {
		name := ""
		switch d := declaration.(type) {
		case *ast.FuncDecl:
			name = d.Name.Name
			if d.Recv != nil {
				var b bytes.Buffer
				_ = printer.Fprint(&b, set, d.Recv.List[0].Type)
				name = b.String() + "." + name
			}
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			var names []string
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
				}
			}
			name = strings.Join(names, ",")
		}
		visual := false
		ast.Inspect(declaration, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.SelectorExpr:
				if n.Sel.Name == "Separator" || n.Sel.Name == "PathSeparator" || strings.HasSuffix(n.Sel.Name, "ObservableGauge") {
					return false
				}
			case *ast.Ident:
				word := strings.ToLower(n.Name)
				for _, term := range terms {
					if strings.Contains(word, term) {
						visual = true
					}
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					value, _ := strconv.Unquote(n.Value)
					if contains(terms, strings.ToLower(strings.ReplaceAll(value, "-", ""))) {
						visual = true
					}
					for _, word := range strings.Split(strings.ToLower(value), "-") {
						if contains(terms, word) {
							visual = true
						}
					}
				}
			}
			return true
		})
		if visual {
			var b bytes.Buffer
			_ = printer.Fprint(&b, set, declaration)
			out[path+":"+name] = fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))
		}
	}
	return out
}

func checkFile(path string, file *ast.File, set *token.FileSet) []string {
	var failures []string
	provider := strings.HasPrefix(path, "capabilities/server/aigateway/providers/")
	pureRules := strings.HasPrefix(path, "capabilities/server/internal/rules/")
	clockNames := map[string]bool{}
	for _, imported := range file.Imports {
		name, _ := strconv.Unquote(imported.Path.Value)
		modelSDK := strings.Contains(name, "anthropic-sdk") || strings.Contains(name, "openai-go") || strings.Contains(name, "generative-ai-go") || strings.HasPrefix(name, "google.golang.org/genai")
		if modelSDK && !provider {
			failures = append(failures, fmt.Sprintf("%s:%d: model SDK belongs in aigateway/providers", path, set.Position(imported.Pos()).Line))
		}
		if pureRules && (name == "C" || name == "unsafe" || name == "plugin" || name == "os" || strings.HasPrefix(name, "os/") || name == "syscall" || name == "net" || strings.HasPrefix(name, "net/") || strings.HasPrefix(name, "database/") || strings.HasPrefix(name, "platformserver") || name == "crypto/rand" || name == "math/rand" || strings.HasPrefix(name, "math/rand/") || strings.Contains(name, ".")) {
			failures = append(failures, fmt.Sprintf("%s:%d: pure rule engine imports I/O capability %s", path, set.Position(imported.Pos()).Line, name))
		}
		if pureRules && name == "time" {
			alias := "time"
			if imported.Name != nil {
				alias = imported.Name.Name
			}
			if alias == "." {
				failures = append(failures, path+": pure rule engine must name its time import")
			}
			clockNames[alias] = true
		}
	}
	if pureRules {
		ast.Inspect(file, func(node ast.Node) bool {
			if selection, ok := node.(*ast.SelectorExpr); ok {
				if name, ok := selection.X.(*ast.Ident); ok && clockNames[name.Name] && contains([]string{"Now", "Since", "Until", "Sleep", "After", "AfterFunc", "Tick", "NewTicker", "NewTimer", "Local"}, selection.Sel.Name) {
					failures = append(failures, fmt.Sprintf("%s:%d: pure rule engine reads live clock or schedules work", path, set.Position(selection.Pos()).Line))
				}
			}
			return true
		})
	}
	if !provider && !strings.Contains(path, "/cmd/check-boundaries/") {
		ast.Inspect(file, func(node ast.Node) bool {
			var rawAI bool
			switch n := node.(type) {
			case *ast.BasicLit:
				value, _ := strconv.Unquote(n.Value)
				rawAI = n.Kind == token.STRING && !strings.ContainsAny(value, " \t\r\n") && (strings.Contains(value, "/chat/completions") || strings.Contains(value, "/embeddings"))
			case *ast.CallExpr:
				if method, ok := n.Fun.(*ast.SelectorExpr); ok {
					rawAI = method.Sel.Name == "aiRequest" || method.Sel.Name == "aiSend" || method.Sel.Name == "aiHTTP"
				}
			}
			if rawAI {
				failures = append(failures, fmt.Sprintf("%s:%d: raw model transport belongs in aigateway/providers", path, set.Position(node.Pos()).Line))
			}
			return true
		})
	}
	if strings.Contains(path, "/cmd/") {
		return failures
	}
	root := strings.HasPrefix(path, "capabilities/server/") && !strings.Contains(strings.TrimPrefix(path, "capabilities/server/"), "/")
	public := strings.HasPrefix(path, "capabilities/server/platform/")
	internal := strings.HasPrefix(path, "capabilities/server/internal/")
	app := ""
	domain := ""
	if rest, ok := strings.CutPrefix(path, "capabilities/server/"); ok {
		first, _, _ := strings.Cut(rest, "/")
		if contains([]string{"kernel", "engine", "studio", "aigateway"}, first) {
			domain = first
		}
	}
	if rest, ok := strings.CutPrefix(path, "capabilities/server/apps/"); ok {
		app, _, _ = strings.Cut(rest, "/")
	}
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
		if rest, ok := strings.CutPrefix(name, "platformserver/"); ok {
			owner, _, _ := strings.Cut(rest, "/")
			concrete := contains([]string{"kernel", "engine", "studio", "aigateway", "composition", "apps"}, owner)
			retiring := name == "platformserver/internal/host" || strings.HasPrefix(name, "platformserver/internal/host/") || name == "platformserver/internal/construction" || strings.HasPrefix(name, "platformserver/internal/construction/")
			if domain != "" && (concrete && owner != domain || retiring) {
				report(imported, "domain consumes a concrete sibling or retiring contract; inject its narrow platform role: "+name)
			}
			if public && (concrete || retiring) {
				report(imported, "public API depends on concrete or retiring owner "+name)
			}
			if app != "" && contains([]string{"kernel", "engine", "studio", "aigateway", "composition"}, owner) {
				report(imported, "platform app consumes a concrete domain instead of its public role "+name)
			}
		}
		if domain != "" && name == "platformserver" {
			report(imported, "domain depends on the old host runtime")
		}
		pure := strings.HasPrefix(path, "capabilities/server/internal/construction/") ||
			strings.HasPrefix(path, "capabilities/server/internal/aistate/") ||
			strings.HasPrefix(path, "capabilities/server/internal/agents/") ||
			strings.HasPrefix(path, "capabilities/server/internal/records/") ||
			strings.HasPrefix(path, "capabilities/server/internal/acceptance/") ||
			strings.HasPrefix(path, "capabilities/server/internal/enterprise/")
		if pure && (name == "platformserver/internal/host" || strings.HasPrefix(name, "platformserver/internal/host/")) {
			report(imported, "pure internal module depends on host coordination")
		}
		if name == "platformserver/internal/enterprise/uafgen" {
			report(imported, "runtime imports generation-only UAF parser")
		}
		if strings.HasPrefix(path, "capabilities/server/internal/enterprise/uaf/") && name == "encoding/xml" {
			report(imported, "UAF runtime model depends on XML parsing")
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
	if root && !strings.HasSuffix(path, "/authorization_decision.go") {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || !memberProjection(function) {
				continue
			}
			if strings.HasSuffix(path, "/knowledge.go") && function.Name.Name == "knowledgeSourceReadable" {
				continue
			}
			reads, guarded := false, false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				var accessed ast.Expr
				switch expr := node.(type) {
				case *ast.IndexExpr:
					accessed = expr.X
				case *ast.RangeStmt:
					accessed = expr.X
				}
				if selection, ok := accessed.(*ast.SelectorExpr); ok && selection.Sel.Name == "rows" {
					reads = true
				}
				if call, ok := node.(*ast.CallExpr); ok {
					if selection, ok := call.Fun.(*ast.SelectorExpr); ok {
						switch selection.Sel.Name {
						case "authorizeFrom", "authorizeDepth", "authorizeNative", "authorizeCapability", "visible", "visibleIn", "Readable", "readableIn", "mayRead", "mayReadIn", "Records", "recordsFrom", "RecordOf", "readRecordFrom", "ReadRecord", "narrowed":
							guarded = true
						}
					}
				}
				return true
			})
			if reads && !guarded {
				report(function, "member record projection reads storage without the owner authorization decision")
			}
		}
	}
	return failures
}

func memberProjection(function *ast.FuncDecl) bool {
	if function.Recv != nil {
		for _, receiver := range function.Recv.List {
			typ := receiver.Type
			if pointer, ok := typ.(*ast.StarExpr); ok {
				typ = pointer.X
			}
			if name, ok := typ.(*ast.Ident); ok && (name.Name == "recordStore" || name.Name == "runtime" || name.Name == "stagedDecision") {
				return false
			}
		}
	}
	if function.Type.Params != nil {
		for _, parameter := range function.Type.Params.List {
			if typ, ok := parameter.Type.(*ast.SelectorExpr); ok && (typ.Sel.Name == "Member" || typ.Sel.Name == "Caller") {
				return true
			}
		}
	}
	return false
}
