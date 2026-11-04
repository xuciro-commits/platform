package host_test

import (
	"slices"
	"testing"

	"platformserver/host/composition"
	"platformserver/kernel/systemscope"
	"platformserver/platform"
)

func TestInvariantApplicationRegistryMatchesBuiltinManifests(t *testing.T) {
	actual := map[string]string{}
	manifests := map[string]platform.Manifest{}
	for _, app := range composition.BuiltinApps(systemscope.Tenant, nil) {
		manifest := app.Manifest()
		if manifest.ID == "" || manifest.Title == "" {
			t.Fatalf("built-in app %T has an empty manifest identity", app)
		}
		if other, exists := actual[manifest.ID]; exists {
			t.Fatalf("manifest %q is declared as %q and %q", manifest.ID, other, manifest.Title)
		}
		actual[manifest.ID] = manifest.Title
		manifests[manifest.ID] = manifest
	}

	ids := platform.BuiltinManifestIDs()
	if len(actual) != len(ids) {
		t.Fatalf("built-in manifests = %d, registry manifests = %d", len(actual), len(ids))
	}
	for _, id := range ids {
		if want := platform.BuiltinManifestTitle(id); actual[id] != want {
			t.Errorf("manifest %q title = %q, registry title = %q", id, actual[id], want)
		}
	}
	for _, access := range platform.BuiltinApplicationAccessRules() {
		manifest, exists := manifests[access.App]
		if !exists {
			t.Errorf("application %q names unknown access owner %q", access.Application, access.App)
			continue
		}
		for _, role := range access.Roles {
			if !slices.Contains(manifest.AllRoles(), role) {
				t.Errorf("application %q grants %s:%s, which the manifest does not declare", access.Application, access.App, role)
			}
		}
	}
}
