package host

import (
	"fmt"
	"slices"

	"platformserver/platform"
	"platformserver/services/devops/packages"
)

func (t *Tenant) OwnerActive(id string) bool { return !t.disabledOwners[id] }
func (t *Tenant) LiveApps() []platform.App {
	apps := make([]platform.App, 0, len(t.apps))
	for _, app := range t.apps {
		if t.OwnerActive(app.Manifest().ID) {
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
	return packages.CheckOwnerRemoval(disabled, func(id string) bool { return t.App(id) != nil }, t.baseModules(), func(removed map[string]bool) error {
		for _, task := range t.work.Tasks(t.apps) {
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
	})
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
		if !t.OwnerActive(owner) {
			delete(t.owner, name)
		}
	}
	roles, err := ownerRolesOf(t.ownerSet, t.OwnerActive)
	if err != nil {
		return err
	}
	t.ownerRoles = roles
	return nil
}

func (t *Tenant) businessStopped() bool { return t.quarantined() || t.console.Suspended() }
