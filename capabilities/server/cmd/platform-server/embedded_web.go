//go:build embedweb

package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	platformserver "platformserver/host"
	"platformserver/kernel/transport"
	"platformserver/services/security/idp"
)

//go:embed all:webdist
var embeddedDist embed.FS

func init() {
	if sub, err := fs.Sub(embeddedDist, "webdist"); err == nil {
		transport.RegisterEmbeddedWeb(sub)
	}
}

func flagProvided(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func standaloneDataDir() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "platform-data")
		if os.MkdirAll(candidate, 0o700) == nil {
			return candidate
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".platform-data")
		_ = os.MkdirAll(candidate, 0o700)
		return candidate
	}
	return "platform-data"
}

func configureStandalone(deployment *platformserver.Deployment, admin *string) {
	if deployment.Web == "" {
		deployment.Web = "embedded"
	}
	if !flagProvided("profile") && deployment.Database == "" && deployment.Issuer == "" {
		deployment.Profile = "lightweight"
	}
	if deployment.Profile == "lightweight" && deployment.Data == "" {
		deployment.Data = standaloneDataDir()
	}
	if deployment.Profile == "lightweight" && !deployment.NewKey {
		keyPath := deployment.Startup().KeyPath()
		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			if err := idp.MakeKeyFile(keyPath); err != nil {
				log.Printf("init signing key: %v", err)
			}
		}
	}
	if deployment.TenantsFile == "" && *admin == "" {
		*admin = "root"
		if deployment.HostAdmins == "" {
			deployment.HostAdmins = "root"
		}
	}
	if !deployment.NewKey && deployment.Mint == "" {
		log.Printf("Platform standalone ready: open http://%s (data: %s)", deployment.Addr, deployment.Data)
	}
}
