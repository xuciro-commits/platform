package host

import (
	"fmt"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"strconv"
)

func (h hostView) ResolvePropertyType(binding platform.AssetBinding) (platform.PropertyType, bool) {
	if binding.Ref.Kind != platform.AssetPropertyType || binding.SourceVersion == "" {
		return platform.PropertyType{}, false
	}
	for _, d := range h.t.definitions {
		if d.Ref == binding.Ref {
			if selected := d.PropertyVersion(binding.SourceVersion); selected != nil {
				return *selected.PropertyType, true
			}
			break
		}
	}
	return platform.PropertyType{}, false
}
func (h hostView) ValidateInstallPropertyType(p platform.PropertyType) error { return p.Check() }
func (h hostView) InstallPropertyType(c platform.Caller, p platform.PropertyType, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if err := p.Check(); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("property type needs a retained version")
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetPropertyType, Name: p.Name}
	d := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".property-" + strconv.Itoa(version), ContractVersion: 1, PropertyType: &p, PropertyVersions: map[string]platform.PropertyType{}}
	d, err := h.t.describer().DescribeDefinition(d)
	if err != nil {
		return err
	}
	i, versions, err := releaseowner.RetainVersion(h.t.definitions, ref, "property type is owned by code", "property version bytes cannot change", d.Version, p, func(old platform.Definition) (map[string]platform.PropertyType, *platform.PropertyType) {
		return old.PropertyVersions, old.PropertyType
	})
	if err != nil {
		return err
	}
	d.PropertyVersions = versions
	h.t.storeDefinition(i, d)
	return nil
}

func (t *Tenant) checkObjectProperties(info platform.EntityInfo) error {
	for _, f := range info.Fields {
		if f.Property == nil {
			continue
		}
		p, ok := (hostView{t: t}).ResolvePropertyType(*f.Property)
		if !ok {
			return fmt.Errorf("field %s has an unavailable property version", f.Name)
		}
		if err := p.CheckField(f); err != nil {
			return err
		}
	}
	return t.checkObjectValueTypes(info)
}
