package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"platformserver/kernel/transport"

	"platformserver"
	enterprise "platformserver/engine/organization"
)

func main() {
	out := flag.String("out", "../../web/packages/kernel/src/gen/host.ts", "the TypeScript file to write")
	sdk := flag.String("sdk", "../../web/packages/kernel/src/gen/enterprise-sdk.ts", "the enterprise SDK TypeScript file to write")
	flag.Parse()
	h := platformserver.NewHost(nil)
	h.Handler()
	ts := transport.TypeScript(h.OpenAPI(nil, nil), transport.KernelModules(filepath.Dir(*out)))
	if err := os.WriteFile(*out, []byte(ts), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*sdk, []byte(enterprise.SDK()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
