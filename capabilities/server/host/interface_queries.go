package host

import (
	"platformserver/platform"
	"platformserver/services/objects"
)

func (t *Tenant) bindInterfaceQuery(q platform.NamedQuery, drafts ...platform.EntityInfo) (platform.NamedQuery, error) {
	var types []platform.EntityInfo
	if q.InterfaceShape == nil {
		for _, et := range t.Projection.SortedTypes() {
			types = append(types, et.Info())
		}
	}
	return objects.BindInterfaceQuery(q, (hostView{t: t}).Interfaces(), types, t.Entity, t.services.Rules, drafts...)
}

func (t *Tenant) checkInterfaceQueryFields(q platform.NamedQuery) error {
	return objects.CheckInterfaceQueryFields(q, t.services.Rules)
}

func (h hostView) ValidateInstallInterface(i platform.Interface) error {
	return h.definitionInstall().ValidateInterface(i)
}

func (h hostView) InstallInterface(c platform.Caller, i platform.Interface, version int) error {
	return h.definitionInstall().InstallInterface(c, i, version)
}
