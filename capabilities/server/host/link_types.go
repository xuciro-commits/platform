package host

import (
	"platformserver/platform"
	"platformserver/services/objects"
)

func (h hostView) ValidateInstallLinkType(l platform.LinkType) error {
	return h.definitionInstall().ValidateLinkType(l)
}

func (h hostView) InstallLinkType(c platform.Caller, l platform.LinkType, version int) error {
	return h.definitionInstall().InstallLinkType(c, l, version)
}

func (t *Tenant) visibleLink(m platform.Member, binding platform.AssetBinding) *platform.LinkType {
	if selected := objects.QuerySourceDefinition(t.Definitions(m), &binding); selected != nil {
		return selected.LinkType
	}
	return nil
}
