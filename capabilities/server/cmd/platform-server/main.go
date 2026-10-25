// Command platform-server serves tenant applications on the common platform.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	_ "time/tzdata"

	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/core"
	"platformserver/apps/system"
	"platformserver/apps/work"
	"platformserver/composition"
	"platformserver/internal/construction"
	"platformserver/internal/systembuild"
	"platformserver/internal/systemscope"
	"platformserver/platform"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8495")
	tenant := flag.String("tenant", "workspace", "default tenant when -tenants is not configured")
	admin := flag.String("admin", "", "explicit administrator subject for the default tenant")
	sourceRoot := flag.String("source-root", "", "running source directory or sealed source snapshot")
	flag.Parse()
	source, err := runningSource(*sourceRoot, deployment.Release)
	if err != nil {
		log.Fatal(err)
	}
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		return composition.NewTenantWithSource(id, source, deployment.SeatsFor(id)...)
	}
	development := platformserver.TenantSpec{ID: *tenant}
	if *admin != "" {
		roles := map[string]string{
			platformserver.PlatformApp: platformserver.Admin,
			construction.ID:            construction.Builder, core.ID: core.Steward, ai.ID: ai.Admin,
			work.ID: work.Admin, platformserver.AgentApp: platformserver.AgentAdmin,
		}
		// The development seat of the system tenant is its publisher: without
		// it nobody could approve a change in a local run.
		if *tenant == systemscope.Tenant {
			roles[system.ID] = system.Publisher
		}
		development.Seats = []platformserver.Seat{{Subjects: []string{*admin}, Member: platform.Member{
			ID: "builder", Roles: roles,
		}}}
	}
	if err := deployment.Run(development); err != nil {
		log.Fatal(err)
	}
}

// Development discovers its repository once. Deployed releases name their
// sealed snapshot explicitly; neither path reads a changing tree per proposal.
func runningSource(root string, version int) (*platform.SystemSource, error) {
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		for {
			if _, err := os.Stat(filepath.Join(current, "capabilities", "server", "go.mod")); err == nil {
				root = current
				break
			}
			parent := filepath.Dir(current)
			if parent == current {
				return nil, nil
			}
			current = parent
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var source systembuild.Source
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err == nil {
		source, err = systembuild.OpenSource(root)
		if err != nil {
			return nil, err
		}
	} else {
		source, err = systembuild.CaptureSource(root, filepath.Join(root, ".build", "system", "sources"))
		if err != nil {
			return nil, err
		}
	}
	return source.Runtime(version), nil
}
