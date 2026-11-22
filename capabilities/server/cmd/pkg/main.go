package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const usage = "usage: pkg build|check [-packages dir] [package-dir ...]"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pkg:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf(usage)
	}
	var step func(string) (packed, error)
	verb := "checked"
	switch args[0] {
	case "build":
		step, verb = build, "packed"
	case "check":
		step = check
	default:
		return fmt.Errorf(usage)
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("packages", "deploy/packages", "directory holding package sources")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w; %s", err, usage)
	}
	dirs, err := sources(*root, flags.Args())
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		p, err := step(dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s %s@%s -> %s (%d assets)\n", verb, p.id, p.version, filepath.Base(p.artifactPath), p.assets)
	}
	return nil
}
