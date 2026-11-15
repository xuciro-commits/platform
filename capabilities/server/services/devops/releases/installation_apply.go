package releases

import (
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func ApplyInstallations(registry *publication.AssetRegistry, installations []platform.ReleaseInstallation) error {
	for _, installation := range installations {
		plan, err := InstallationPlan(registry, installation)
		if err != nil {
			return err
		}
		if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{}); err != nil {
			return err
		}
	}
	return nil
}
