// Command check-boundaries checks source imports even when build tags exclude
// a production file on the checking machine. Counts and unrelated deletions
// cannot excuse a reverse dependency or an anonymous public runtime probe.
//
// It also enforces the anti-bloat constitution recorded in docs/Intent.md:
// a package file-count budget, a ban on cross-package duplicate base names, a
// ban on front-end visual component names in backend Go code, and a ratchet
// that keeps the recorded debt budget from growing.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// defaultFileCap is the maximum number of non-test Go files a single package
// directory may hold. Directories already above it carry an explicit, shrinking
// budget in boundaries.budget.
const defaultFileCap = 25

// uiComponentTokens are the explicit front-end visual component names the
// constitution bans from backend Go code.
var uiComponentTokens = []string{
	"spacer", "separator", "button_group", "kanban", "gantt", "heatmap",
	"sparkline", "treemap", "gauge",
}

// pageContractFiles are the only backend page files allowed to carry page
// component definitions; every other page_*.go under platform/ is debt.
var pageContractFiles = map[string]bool{
	"page_model.go":   true,
	"page_queries.go": true,
	"page_rules.go":   true,
}

func main() {
	root := flag.String("root", "../..", "repository root")
	updateBudget := flag.Bool("update-budget", false, "rewrite boundaries.budget from the current tree and exit")
	flag.Parse()

	budgetPath := filepath.Join(*root, "capabilities/server/cmd/check-boundaries/boundaries.budget")
	if *updateBudget {
		if err := writeBudget(budgetPath, collectBudget(*root)); err != nil {
			fmt.Fprintln(os.Stderr, "boundary:", err)
			os.Exit(1)
		}
		fmt.Println("boundary budget updated:", budgetPath)
		return
	}

	budget, err := loadBudget(budgetPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "boundary:", err)
		os.Exit(1)
	}

	var failures []string
	err = walkServer(*root, func(rel string, entry fs.DirEntry) error {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, filepath.Join(*root, rel), nil, 0)
		if err != nil {
			return err
		}
		for _, finding := range checkFile(rel, file, set) {
			failures = append(failures, finding)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "boundary:", err)
		os.Exit(1)
	}

	failures = append(failures, checkFileCaps(*root, budget)...)
	failures = append(failures, checkDuplicateBasenames(*root, budget)...)
	failures = append(failures, checkUIComponents(*root, budget)...)
	failures = append(failures, checkBudgetDrift(*root, "capabilities/server/cmd/check-boundaries/boundaries.budget")...)

	sort.Strings(failures)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, "boundary:", failure)
	}
	if len(failures) != 0 {
		os.Exit(1)
	}
	fmt.Println("source boundaries ok: every production Go file checked")
}

func checkFile(path string, file *ast.File, set *token.FileSet) []string {
	if strings.Contains(path, "/cmd/") {
		return nil // binaries and generators compose compiled implementations
	}
	root := strings.HasPrefix(path, "capabilities/server/") && !strings.Contains(strings.TrimPrefix(path, "capabilities/server/"), "/")
	public := strings.HasPrefix(path, "capabilities/server/platform/")
	internal := strings.HasPrefix(path, "capabilities/server/internal/")
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
			} // source-grant fact resolver consumed by authorizeDepth
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
			} // namespace-bound owner storage and private writes
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

// walkServer visits every non-test Go file under capabilities/server, reporting
// paths relative to the repository root.
func walkServer(root string, fn func(rel string, entry fs.DirEntry) error) error {
	return filepath.WalkDir(filepath.Join(root, "capabilities/server"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" || entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), entry)
	})
}

// budget records the debt the constitution tolerates today. Every entry must
// shrink as the corresponding files are merged or deleted.
type budget struct {
	caps map[string]int
	dups map[string]bool
	uis  map[string]bool
}

func newBudget() *budget {
	return &budget{caps: map[string]int{}, dups: map[string]bool{}, uis: map[string]bool{}}
}

func (b *budget) capOf(dir string) int {
	if limit, ok := b.caps[dir]; ok {
		return limit
	}
	return defaultFileCap
}

func parseBudget(data string) (*budget, error) {
	b := newBudget()
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "cap":
			if len(fields) != 3 {
				return nil, fmt.Errorf("bad cap line: %q", line)
			}
			limit, err := strconv.Atoi(fields[2])
			if err != nil {
				return nil, fmt.Errorf("bad cap value: %q", line)
			}
			b.caps[fields[1]] = limit
		case "dup":
			if len(fields) != 2 {
				return nil, fmt.Errorf("bad dup line: %q", line)
			}
			b.dups[fields[1]] = true
		case "ui":
			if len(fields) != 2 {
				return nil, fmt.Errorf("bad ui line: %q", line)
			}
			b.uis[fields[1]] = true
		default:
			return nil, fmt.Errorf("unknown budget directive: %q", line)
		}
	}
	return b, sc.Err()
}

func loadBudget(path string) (*budget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newBudget(), nil
		}
		return nil, err
	}
	return parseBudget(string(data))
}

func writeBudget(path string, b *budget) error {
	var sb strings.Builder
	sb.WriteString("# Boundary debt budget, generated by `go run ./cmd/check-boundaries -update-budget`.\n")
	sb.WriteString("# Recorded debt may only shrink: lower a cap, drop a dup/ui entry, as the\n")
	sb.WriteString("# corresponding files are merged or deleted. checkBudgetDrift fails when this\n")
	sb.WriteString("# file grows compared with the committed HEAD revision.\n")
	for _, dir := range sortedKeysInt(b.caps) {
		fmt.Fprintf(&sb, "cap %s %d\n", dir, b.caps[dir])
	}
	for _, name := range sortedKeys(b.dups) {
		fmt.Fprintf(&sb, "dup %s\n", name)
	}
	for _, rel := range sortedKeys(b.uis) {
		fmt.Fprintf(&sb, "ui %s\n", rel)
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedKeysInt(set map[string]int) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// collectBudget freezes the current debt into a budget.
func collectBudget(root string) *budget {
	b := newBudget()
	for dir, count := range goFileCounts(root) {
		if count > defaultFileCap {
			b.caps[dir] = count
		}
	}
	for name := range duplicateBasenames(root) {
		b.dups[name] = true
	}
	for rel := range uiFlaggedFiles(root) {
		b.uis[rel] = true
	}
	return b
}

func goFileCounts(root string) map[string]int {
	counts := map[string]int{}
	_ = walkServer(root, func(rel string, entry fs.DirEntry) error {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		counts[filepath.Dir(rel)]++
		return nil
	})
	return counts
}

func duplicateBasenames(root string) map[string][]string {
	owners := map[string]map[string]bool{}
	_ = walkServer(root, func(rel string, entry fs.DirEntry) error {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		base := strings.TrimSuffix(entry.Name(), ".go")
		if base == "doc" || base == "main" {
			return nil
		}
		dir := filepath.Dir(rel)
		if owners[base] == nil {
			owners[base] = map[string]bool{}
		}
		owners[base][dir] = true
		return nil
	})
	out := map[string][]string{}
	for base, dirs := range owners {
		if len(dirs) < 2 {
			continue
		}
		list := make([]string, 0, len(dirs))
		for dir := range dirs {
			list = append(list, dir)
		}
		sort.Strings(list)
		out[base] = list
	}
	return out
}

func uiFlaggedFiles(root string) map[string]bool {
	flagged := map[string]bool{}
	_ = walkServer(root, func(rel string, entry fs.DirEntry) error {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		base := entry.Name()
		if strings.HasPrefix(base, "page_") && strings.HasPrefix(rel, "capabilities/server/platform/") && !pageContractFiles[base] {
			flagged[rel] = true
			return nil
		}
		if uiToken(base) {
			flagged[rel] = true
			return nil
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, filepath.Join(root, rel), nil, 0)
		if err != nil {
			return nil
		}
		for _, decl := range file.Decls {
			if uiDeclName(decl) {
				flagged[rel] = true
				return nil
			}
		}
		return nil
	})
	return flagged
}

func uiDeclName(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if uiToken(s.Name.Name) {
					return true
				}
			case *ast.ValueSpec:
				for _, name := range s.Names {
					if uiToken(name.Name) {
						return true
					}
				}
			}
		}
	case *ast.FuncDecl:
		if d.Name != nil && uiToken(d.Name.Name) {
			return true
		}
	}
	return false
}

func uiToken(s string) bool {
	lower := strings.ToLower(s)
	for _, token := range uiComponentTokens {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func checkFileCaps(root string, b *budget) []string {
	counts := goFileCounts(root)
	var out []string
	for dir, count := range counts {
		if limit := b.capOf(dir); count > limit {
			out = append(out, fmt.Sprintf("package file budget: %s has %d non-test go files, budget %d", dir, count, limit))
		}
	}
	sort.Strings(out)
	return out
}

func checkDuplicateBasenames(root string, b *budget) []string {
	var out []string
	for base, dirs := range duplicateBasenames(root) {
		if b.dups[base] {
			continue
		}
		out = append(out, fmt.Sprintf("cross-package duplicate: %s.go declared in %d packages (%s)", base, len(dirs), strings.Join(dirs, ", ")))
	}
	sort.Strings(out)
	return out
}

func checkUIComponents(root string, b *budget) []string {
	var out []string
	for rel := range uiFlaggedFiles(root) {
		if b.uis[rel] {
			continue
		}
		out = append(out, fmt.Sprintf("backend UI coupling: %s carries a front-end visual component name", rel))
	}
	sort.Strings(out)
	return out
}

func checkBudgetDrift(root, rel string) []string {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	cmd := exec.Command("git", "-C", root, "show", "HEAD:"+rel)
	headData, err := cmd.Output()
	if err != nil {
		return nil // budget not committed yet; nothing to ratchet against
	}
	head, err := parseBudget(string(headData))
	if err != nil {
		return nil
	}
	current, err := loadBudget(filepath.Join(root, rel))
	if err != nil {
		return nil
	}
	var out []string
	for dir, limit := range current.caps {
		if previous, ok := head.caps[dir]; !ok || limit > previous {
			out = append(out, fmt.Sprintf("debt budget grew: package budget for %s raised to %d", dir, limit))
		}
	}
	for name := range current.dups {
		if !head.dups[name] {
			out = append(out, fmt.Sprintf("debt budget grew: cross-package duplicate %s tolerated", name))
		}
	}
	for rel := range current.uis {
		if !head.uis[rel] {
			out = append(out, fmt.Sprintf("debt budget grew: backend UI file %s tolerated", rel))
		}
	}
	sort.Strings(out)
	return out
}
