package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"platformserver/kernel/transport"
	"platformserver/platform"

	platformserver "platformserver/host"
	enterprise "platformserver/services/security/organization"
)

func main() {
	out := flag.String("out", "../../web/packages/api/src/gen/host.ts", "the TypeScript file to write")
	sdk := flag.String("sdk", "../../web/packages/api/src/gen/enterprise-sdk.ts", "the enterprise SDK TypeScript file to write")
	flag.Parse()
	h := platformserver.NewHost(nil)
	h.Handler()
	ts := transport.TypeScript(h.OpenAPI(nil, nil), transport.KernelModules(filepath.Dir(*out)))
	edits, _ := json.Marshal(platform.EditProposalSchema())
	ts += "\nexport const editProposalSchema = " + string(edits) + " as const;\n"
	if err := os.WriteFile(*out, []byte(ts), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*sdk, []byte(enterprise.SDK()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
