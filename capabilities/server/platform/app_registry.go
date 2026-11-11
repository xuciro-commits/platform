package platform

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

//go:embed app_registry.json
var appRegistryJSON []byte

type applicationRegistry struct {
	Format    int                         `json:"format"`
	Manifests []registeredManifest        `json:"manifests"`
	Apps      []registeredApplicationInfo `json:"applications"`
}

type registeredManifest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type registeredApplicationInfo struct {
	ID       string                 `json:"id"`
	Title    string                 `json:"title"`
	Manifest string                 `json:"manifest"`
	Serves   []string               `json:"serves"`
	Access   []registeredAccessRule `json:"access"`
	Home     []registeredHomeInfo   `json:"home"`
	Opens    []registeredOpenInfo   `json:"opens"`
}

type registeredAccessRule struct {
	App      string   `json:"app"`
	Roles    []string `json:"roles"`
	Schema   string   `json:"schema"`
	Entities bool     `json:"entities"`
	All      bool     `json:"all"`
}

type registeredHomeInfo struct {
	When []registeredAccessRule `json:"when"`
}

type registeredOpenInfo struct {
	When []registeredAccessRule `json:"when"`
}

type BuiltinApplicationAccess struct {
	Application string
	App         string
	Roles       []string
}

type applicationRegistryContents struct {
	titles map[string]string
	access []BuiltinApplicationAccess
}

var builtinRegistry = loadApplicationRegistry()
var builtinManifests = builtinRegistry.titles

func loadApplicationRegistry() applicationRegistryContents {
	var registry applicationRegistry
	if err := json.Unmarshal(appRegistryJSON, &registry); err != nil {
		panic(fmt.Sprintf("application registry: %v", err))
	}
	if registry.Format != 1 {
		panic(fmt.Sprintf("application registry format: %d", registry.Format))
	}
	out := applicationRegistryContents{titles: make(map[string]string, len(registry.Manifests))}
	for _, manifest := range registry.Manifests {
		if manifest.ID == "" || manifest.Title == "" {
			panic("application registry has a manifest without an ID or title")
		}
		if _, exists := out.titles[manifest.ID]; exists {
			panic(fmt.Sprintf("application registry repeats manifest %q", manifest.ID))
		}
		out.titles[manifest.ID] = manifest.Title
	}
	add := func(app string, rules []registeredAccessRule) {
		for _, rule := range rules {
			if rule.App != "" {
				out.access = append(out.access, BuiltinApplicationAccess{Application: app, App: rule.App, Roles: slices.Clone(rule.Roles)})
			}
		}
	}
	for _, app := range registry.Apps {
		add(app.ID, app.Access)
		for _, home := range app.Home {
			add(app.ID, home.When)
		}
		for _, open := range app.Opens {
			add(app.ID, open.When)
		}
	}
	return out
}

func BuiltinManifestTitle(id string) string {
	return builtinManifests[id]
}
