package host

import "platformserver/platform"

func (h hostView) ValidateInstallQuery(q platform.NamedQuery) error {
	return h.definitionInstall().ValidateQuery(q)
}

func (h hostView) InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.definitionInstall().NamedQuery(q, version)
}
