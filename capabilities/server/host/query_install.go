package host

import "platformserver/platform"

func (h hostView) ValidateInstallQuery(q platform.NamedQuery) error {
	return h.definitionInstall().ValidateQuery(q)
}

func (h hostView) InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error {
	return h.definitionInstall().InstallQuery(c, q, version)
}
