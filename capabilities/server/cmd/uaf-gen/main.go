// Command uaf-gen generates the runtime metamodel from the retained OMG files.
// Invoke through make generate-uaf or make check-uaf from the repository root.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"platformserver/internal/enterprise/uafgen"
)

func main() {
	check := flag.Bool("check", false, "check generated data without modifying files")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool) error {
	base := filepath.Join("internal", "enterprise", "uaf")
	generated, err := uafgen.Generate(os.DirFS(filepath.Join(base, "spec")))
	if err != nil {
		return err
	}
	output := filepath.Join(base, "gen")
	if !check {
		if err := os.MkdirAll(output, 0755); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(generated))
	for name := range generated {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(output, name)
		if check {
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, generated[name]) {
				return fmt.Errorf("UAF data %s is stale; run make generate-uaf", path)
			}
		} else if err := os.WriteFile(path, generated[name], 0644); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, expected := generated[entry.Name()]; expected || entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if check {
			return fmt.Errorf("obsolete UAF data %s; run make generate-uaf", entry.Name())
		}
		if err := os.Remove(filepath.Join(output, entry.Name())); err != nil {
			return err
		}
	}
	fmt.Printf("UAF data: %d retained release(s)\n", len(generated))
	return nil
}
