package host

import (
	"fmt"
	"slices"
	"strings"

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
	ids := append([]string{}, t.console.DisabledOwners()...)
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
	for _, effect := range t.outbound.Select(nil) {
		if removed[effect.App] && !platform.EffectSettled(effect.State) {
			return fmt.Errorf("Resolve external attempt %s before removing %s", effect.ID, effect.App)
		}
	}
	return nil
}

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
	roles, err := ownerRolesOf(t.apps, t.ownerActive)
	if err != nil {
		return err
	}
	t.ownerRoles = roles
	return nil
}

func (t *Tenant) businessStopped() bool { return t.quarantined() || t.console.Suspended() }
