package host

import "platformserver/platform"

func (h hostView) ValidateInstallInterface(i platform.Interface) error {
	return h.definitionInstall().ValidateInterface(i)
}

func (h hostView) InstallInterface(c platform.Caller, i platform.Interface, version int) error {
	return h.definitionInstall().InstallInterface(c, i, version)
}
