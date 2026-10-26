package platformserver

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/internal/host"
	"platformserver/platform"
)

func (t *Tenant) ownerActive(id string) bool { return !t.disabledOwners[id] }
func (t *Tenant) liveApps() []platform.App {
	apps := make([]platform.App, 0, len(t.apps))
	for _, app := range t.apps {
		if t.ownerActive(app.Manifest().ID) {
			apps = append(apps, app)
		}
	}
	return apps
}
func (t *Tenant) currentDisabledOwners() []string {
	ids := []string{}
	for id := range t.disabledOwners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
func (t *Tenant) desiredDisabledOwners() []string {
	t.console.mu.Lock()
	defer t.console.mu.Unlock()
	ids := append([]string{}, t.console.disabledOwners...)
	slices.Sort(ids)
	return ids
}

func (t *Tenant) checkOwnerRemoval(disabled []string) error {
	removed := map[string]bool{}
	for _, id := range disabled {
		if removed[id] || t.app(id) == nil {
			return fmt.Errorf("Unknown or repeated platform module %s", id)
		}
		removed[id] = true
	}
	var modules []platform.CapabilityModule
	for _, module := range t.baseModules() {
		if id, owner := strings.CutPrefix(module.ID, "platform.owner."); owner && removed[id] {
			continue
		}
		modules = append(modules, module)
	}
	if _, err := platform.ResolveComposition(modules, nil); err != nil {
		return err
	}
	for _, task := range t.Tasks() {
		if removed[task.App] && task.Kind != "job" && !slices.Contains([]string{"done", "cancelled"}, task.State) {
			return fmt.Errorf("Finish or resolve work %s before removing %s", task.ID, task.App)
		}
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, effect := range t.outbound {
		if removed[effect.App] && !settled(effect.State) {
			return fmt.Errorf("Resolve external attempt %s before removing %s", effect.ID, effect.App)
		}
	}
	return nil
}

// Recovery keeps the original owner codecs and ledgers. At the cold startup
// boundary, live routes and role bindings are withdrawn without deleting data.
func (t *Tenant) deactivateOwners(disabled []string) error {
	if len(disabled) == 0 {
		return nil
	}
	if err := t.checkOwnerRemoval(disabled); err != nil {
		return err
	}
	t.disabledOwners = map[string]bool{}
	for _, id := range disabled {
		t.disabledOwners[id] = true
	}
	for name, owner := range t.owner {
		if !t.ownerActive(owner.Manifest().ID) {
			delete(t.owner, name)
		}
	}
	t.listeners = slices.DeleteFunc(t.listeners, func(id string) bool { return !t.ownerActive(id) })
	t.observers = slices.DeleteFunc(t.observers, func(observer host.Observer) bool {
		app, ok := observer.(platform.App)
		return ok && !t.ownerActive(app.Manifest().ID)
	})
	for _, app := range t.apps {
		if t.ownerActive(app.Manifest().ID) {
			continue
		}
		if _, ok := app.(host.Linker); ok {
			t.linker = nil
		}
		if _, ok := app.(host.Directory); ok {
			t.directory = nil
		}
		if _, ok := app.(host.IdentityDirectory); ok {
			t.identity = nil
		}
		if _, ok := app.(models); ok {
			t.ai = nil
		}
		if _, ok := app.(host.Tasks); ok {
			t.tasks = nil
		}
		if _, ok := app.(AgentRuntime); ok {
			t.agents = nil
		}
		if _, ok := app.(host.Glossary); ok {
			t.knowledge = nil
		}
	}
	return nil
}
