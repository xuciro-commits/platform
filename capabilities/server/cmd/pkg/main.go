package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"platformserver/kernel/protocol"
)

const usage = `usage:
  pkg lock  [-packages dir] [package-dir ...]   refresh package.lock.json from the sources
  pkg check [-packages dir] [-install=false] [package-dir ...]   validate, check the lock, compare with the released snapshot,
      then (unless -install=false) install them together into an in-memory tenant and run each package's try.yaml
  pkg diff  [-packages dir] [package-dir ...]   review the upgrade from the released snapshot and check the version bump
  pkg build -o file package-dir                 write the artifact of one package
  pkg split artifact.json package-dir           turn a released artifact into a source directory
  pkg try [-script file] package-dir ...        install the packages in order into an in-memory tenant and run the script;
      the script defaults to try.yaml in the last package and lists steps of do/on/with or read, with show, expect and refused
  pkg catalog -o catalog.json [-title t] [-url template] package-dir ...   write a catalog entry per package;
      the template may use {id}, {version} and {artifact}; without {artifact} the file name is appended`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pkg:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("packages", "deploy/packages", "directory holding package sources")
	output := flags.String("o", "", "artifact file to write")
	title := flags.String("title", "", "catalog title")
	prefix := flags.String("url", "./", "address template of each artifact in the catalog")
	script := flags.String("script", "", "trial script; defaults to try.yaml in the last package")
	install := flags.Bool("install", true, "check: also install the packages into an in-memory tenant and run their try.yaml")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	switch args[0] {
	case "try":
		return try(flags.Args(), *script, out)
	case "split":
		if flags.NArg() != 2 {
			return errors.New(usage)
		}
		if err := split(flags.Arg(0), flags.Arg(1)); err != nil {
			return err
		}
		fmt.Fprintf(out, "split %s -> %s\n", flags.Arg(0), flags.Arg(1))
		return nil
	case "build":
		if flags.NArg() != 1 || *output == "" {
			return errors.New(usage)
		}
		previous, _, err := snapshot(flags.Arg(0))
		if err != nil {
			return err
		}
		b, err := build(flags.Arg(0), previous)
		if err != nil {
			return err
		}
		if err := writeFile(*output, b.artifact); err != nil {
			return err
		}
		fmt.Fprintf(out, "built %s@%s -> %s (%d resources, %s)\n", b.id, b.version, *output, b.assets, b.lock.Artifact)
		return nil
	case "catalog":
		if flags.NArg() == 0 || *output == "" {
			return errors.New(usage)
		}
		doc := catalogDocument{Version: 1, Title: *title, Packages: []protocol.PackageCatalogEntry{}}
		for _, dir := range flags.Args() {
			previous, _, err := snapshot(dir)
			if err != nil {
				return err
			}
			b, err := build(dir, previous)
			if err != nil {
				return err
			}
			doc.Packages = append(doc.Packages, catalogEntry(b, artifactURL(*prefix, b)))
		}
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFile(*output, string(raw)+"\n"); err != nil {
			return err
		}
		fmt.Fprintf(out, "catalog %s: %d packages\n", *output, len(doc.Packages))
		return nil
	case "lock", "check", "diff":
		step, verb := lockPackage, "locked"
		switch args[0] {
		case "check":
			step, verb = check, "checked"
		case "diff":
			step, verb = func(dir string) (built, error) { return diff(dir, out) }, "reviewed"
		}
		dirs, err := sources(*root, flags.Args())
		if err != nil {
			return err
		}
		var checked []built
		for _, dir := range dirs {
			b, err := step(dir)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "%s %s@%s (%d resources) in %s\n", verb, b.id, b.version, b.assets, filepath.Base(dir))
			checked = append(checked, b)
		}
		if args[0] == "check" && *install {
			return installCheck(dirs, checked, out)
		}
		return nil
	}
	return errors.New(usage)
}

func artifactURL(template string, b built) string {
	if !strings.Contains(template, "{artifact}") {
		template += "{artifact}"
	}
	return strings.NewReplacer("{id}", b.id, "{version}", b.version, "{artifact}", b.artifactName).Replace(template)
}
