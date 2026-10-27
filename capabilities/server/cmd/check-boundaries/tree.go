// Structure ratchet: a package may not gain files or lines, a name may not
// appear in a second package, and the backend may not declare another visual
// widget. The ceiling is what exists today, so the guard is honest without a
// migration quota; every step down is recorded in the same change.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// visualWidgets are frontend layout and chart components. Their limits are
// declared once in the widget table and drawn by the web kit; a Go file that
// names one is a second authority over pixels.
var visualWidgets = []string{"spacer", "separator", "buttongroup", "kanban", "gantt", "heatmap", "sparkline", "treemap", "gauge"}

const (
	kindFiles = "files"
	kindLOC   = "loc"
	kindDup   = "dup"
	kindUI    = "ui"
	kindUAF   = "uaf"
	// scopeTotal guards the sum over the area, so a scope may not grow just
	// because another shrank.
	scopeTotal = "*"
)

// uafImports are the packages an external standard metamodel may reach. The
// enterprise app may shrink this set only by removing a dependency on it; no
// new importer, and no new file inside the generator, is admitted.
var uafImports = []string{"platformserver/internal/enterprise/uaf", "platformserver/internal/enterprise/uafgen"}

// growth is one measured quantity of one scope.
type growth struct {
	kind  string
	scope string
	value int
}

func (g growth) key() string { return g.kind + "\t" + g.scope }

func (g growth) line() string { return g.key() + "\t" + strconv.Itoa(g.value) }

type measures struct {
	files []growth // per-package non-test file count
	loc   []growth // per-package non-test lines
	dup   []growth // per cross-package duplicate name
	ui    []growth // per-file visual widget declarations
	uaf   []growth // per-file external standard metamodel imports
}

// scanTree measures the source tree the ratchet guards.
func scanTree(root, area string) (*measures, error) {
	type stat struct {
		files int
		loc   int
	}
	perPackage := map[string]*stat{}
	perFileUI := map[string]int{}
	var uiFiles []string
	perFileUAF := map[string]int{}
	var uafFiles []string
	baseNames := map[string]map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, area), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" || entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if perPackage[dir] == nil {
			perPackage[dir] = &stat{}
		}
		perPackage[dir].files++
		perPackage[dir].loc += 1 + strings.Count(string(body), "\n")
		base := filepath.Base(rel)
		if baseNames[base] == nil {
			baseNames[base] = map[string]bool{}
		}
		baseNames[base][dir] = true
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		if n := visualWidgetDeclarations(file); n > 0 {
			perFileUI[rel] = n
			uiFiles = append(uiFiles, rel)
		}
		if n := uafDependents(rel, file); n > 0 {
			perFileUAF[rel] = n
			uafFiles = append(uafFiles, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := &measures{}
	totalFiles, totalLOC := 0, 0
	for dir, stat := range perPackage {
		out.files = append(out.files, growth{kindFiles, dir, stat.files})
		out.loc = append(out.loc, growth{kindLOC, dir, stat.loc})
		totalFiles += stat.files
		totalLOC += stat.loc
	}
	sort.Slice(out.files, func(i, j int) bool { return out.files[i].scope < out.files[j].scope })
	sort.Slice(out.loc, func(i, j int) bool { return out.loc[i].scope < out.loc[j].scope })
	for base, dirs := range baseNames {
		if len(dirs) > 1 && base != "main.go" {
			out.dup = append(out.dup, growth{kindDup, base, len(dirs)})
		}
	}
	sort.Slice(out.dup, func(i, j int) bool {
		if out.dup[i].value != out.dup[j].value {
			return out.dup[i].value > out.dup[j].value
		}
		return out.dup[i].scope < out.dup[j].scope
	})
	for _, f := range uiFiles {
		out.ui = append(out.ui, growth{kindUI, f, perFileUI[f]})
	}
	sort.Slice(out.ui, func(i, j int) bool { return out.ui[i].scope < out.ui[j].scope })
	for _, f := range uafFiles {
		out.uaf = append(out.uaf, growth{kindUAF, f, perFileUAF[f]})
	}
	sort.Slice(out.uaf, func(i, j int) bool { return out.uaf[i].scope < out.uaf[j].scope })
	// Totals close the loophole the per-scope ceilings leave: a scope may not
	// grow just because another shrank, and declarations may not be relocated
	// to a different file or package to dodge their own ceiling.
	out.files = append([]growth{{kindFiles, scopeTotal, totalFiles}}, out.files...)
	out.loc = append([]growth{{kindLOC, scopeTotal, totalLOC}}, out.loc...)
	out.dup = append([]growth{{kindDup, scopeTotal, len(out.dup)}}, out.dup...)
	out.ui = append([]growth{{kindUI, scopeTotal, len(perFileUI)}}, out.ui...)
	out.uaf = append([]growth{{kindUAF, scopeTotal, len(perFileUAF)}}, out.uaf...)
	return out, nil
}

// visualWidgetDeclarations counts backend declarations that name a frontend
// visual component: a type, function, method, constant, variable or struct
// field is a new authority over that widget, while reading an existing
// widget's field is not.
func visualWidgetDeclarations(file *ast.File) (found int) {
	blocked := func(name string) bool {
		return matchesVisualWidget(name)
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if blocked(decl.Name.Name) {
				found++
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if blocked(spec.Name.Name) {
						found++
					}
					if str, ok := spec.Type.(*ast.StructType); ok {
						for _, field := range str.Fields.List {
							for _, name := range field.Names {
								if blocked(name.Name) {
									found++
								}
							}
						}
					}
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if blocked(name.Name) {
							found++
						}
					}
				}
			}
		}
	}
	return found
}

func matchesVisualWidget(name string) bool {
	clean := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "_", ""), "-", ""))
	for _, widget := range visualWidgets {
		if strings.Contains(clean, widget) {
			return true
		}
	}
	return false
}

func readCeilings(path string) (map[string]int, error) {
	ceilings := map[string]int{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ceilings, nil
	}
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s: want kind, scope, count separated by tabs, got %q", path, line)
		}
		value, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a count", path, fields[3])
		}
		ceilings[fields[0]+"\t"+fields[1]] = value
		if fields[1] == "" {
			return nil, fmt.Errorf("%s: %q has an empty scope", path, line)
		}
	}
	return ceilings, nil
}

// ratchet fails on growth and on an unrecorded step down, so the ceiling can
// never drift away from the code it describes.
func ratchet(measures *measures, ceilings map[string]int) (failures []string, counted int) {
	check := func(g growth, why string) {
		ceiling, known := ceilings[g.key()]
		counted++
		switch {
		case !known:
			failures = append(failures, fmt.Sprintf("%s: %s is new (%d); %s", g.kind, g.scope, g.value, why))
		case g.value > ceiling:
			failures = append(failures, fmt.Sprintf("%s: %s grew to %d, ceiling %d; %s", g.kind, g.scope, g.value, ceiling, why))
		case g.value < ceiling:
			failures = append(failures, fmt.Sprintf("%s: %s is down to %d; record the new ceiling in boundaries.tsv", g.kind, g.scope, g.value))
		}
	}
	for _, g := range measures.files {
		check(g, "merge a file into its owner instead of adding one (AGENTS.md rule 6)")
	}
	for _, g := range measures.loc {
		check(g, "fold duplicate paths into the owner instead of extending both")
	}
	for _, g := range measures.dup {
		check(g, fmt.Sprintf("%s is defined in %d packages; one name needs one owner", g.scope, g.value))
	}
	for _, g := range measures.ui {
		check(g, "the backend declares a frontend visual component; its limits belong to the widget table and the web kit")
	}
	for _, g := range measures.uaf {
		check(g, "another package now depends on the external UAF metamodel; the enterprise model converges on the minimal org catalog, it does not spread")
	}
	return failures, counted
}

func writeCeilings(path string, measures *measures) error {
	var b strings.Builder
	b.WriteString("# Structure ratchet ceilings, regenerated by `go run ./cmd/check-boundaries -update`.\n")
	b.WriteString("# One line per guarded scope: kind, scope, count. A count may only fall,\n")
	b.WriteString("# and a fallen count must be recorded in the change that lowered it.\n")
	for _, group := range [][]growth{measures.files, measures.loc, measures.dup, measures.ui, measures.uaf} {
		for _, g := range group {
			b.WriteString(g.line() + "\n")
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// uafDependents counts imports of the external standard metamodel inside one
// file. The owning packages are exempt: they are the dependency, not a new
// consumer of it.
func uafDependents(path string, file *ast.File) int {
	for _, owned := range []string{
		"capabilities/server/internal/enterprise/uaf/",
		"capabilities/server/internal/enterprise/uafgen/",
	} {
		if strings.HasPrefix(path, owned) {
			return 0
		}
	}
	found := 0
	for _, imported := range file.Imports {
		name, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		for _, blocked := range uafImports {
			if name == blocked || strings.HasPrefix(name, blocked+"/") {
				found++
			}
		}
	}
	return found
}
