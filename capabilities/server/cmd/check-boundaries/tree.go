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
)

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
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := &measures{}
	for dir, stat := range perPackage {
		if stat == nil {
			continue // a directory of tests only
		}
		out.files = append(out.files, growth{kindFiles, dir, stat.files})
		out.loc = append(out.loc, growth{kindLOC, dir, stat.loc})
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
	return failures, counted
}

func writeCeilings(path string, measures *measures) error {
	var b strings.Builder
	b.WriteString("# Structure ratchet ceilings, regenerated by `go run ./cmd/check-boundaries -update`.\n")
	b.WriteString("# One line per guarded scope: kind, scope, count. A count may only fall,\n")
	b.WriteString("# and a fallen count must be recorded in the change that lowered it.\n")
	for _, group := range [][]growth{measures.files, measures.loc, measures.dup, measures.ui} {
		for _, g := range group {
			b.WriteString(g.line() + "\n")
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
