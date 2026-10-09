// Command api-types writes the TypeScript types of the host API from the
// host's Go types (ADR-0023 D7): the web edge's types are generated, never
// written by hand. The same run writes the enterprise SDK (ADR-0094 D4) —
// query/ref/write contracts typed from the app's own declarations. Run it in
// capabilities/server after changing a type the host answers with or a
// declaration the SDK types; TestAPIContract fails while either file is stale.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"platformserver"
	"platformserver/apps/enterprise"
)

func main() {
	out := flag.String("out", "../../web/packages/kernel/src/gen/host.ts", "the TypeScript file to write")
	sdk := flag.String("sdk", "../../web/packages/kernel/src/gen/enterprise-sdk.ts", "the enterprise SDK TypeScript file to write")
	flag.Parse()
	h := platformserver.NewHost(nil)
	h.Handler() // registers the routes the contract describes
	ts := platformserver.TypeScript(h.OpenAPI(nil, nil), platformserver.KernelModules(filepath.Dir(*out)))
	if err := os.WriteFile(*out, []byte(ts), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*sdk, []byte(enterprise.SDK()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
