package host

import "platformserver/platform"

func (h hostView) ValidateInstallObjectTypeGroup(g platform.ObjectTypeGroup) error {
	return h.definitionInstall().ValidateObjectTypeGroup(g)
}

func (h hostView) InstallObjectTypeGroup(c platform.Caller, g platform.ObjectTypeGroup, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.definitionInstall().ObjectTypeGroup(g, version)
}
