// Command api-types writes the TypeScript types of the host API from the
// host's Go types (ADR-0023 D7): the web edge's types are generated, never
// written by hand. Run it in capabilities/server after changing a type the
// host answers with; TestAPIContract fails while the file is stale.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"platformserver"
)

func main() {
	out := flag.String("out", "../../web/packages/kernel/src/gen/host.ts", "the TypeScript file to write")
	flag.Parse()
	h := platformserver.NewHost(nil)
	h.Handler() // registers the routes the contract describes
	ts := platformserver.TypeScript(h.OpenAPI(nil, nil), platformserver.KernelModules(filepath.Dir(*out)))
	if err := os.WriteFile(*out, []byte(ts), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
