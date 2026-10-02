package platformserver

import (
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
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
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		old := h.t.definitions[i]
		if old.Source != "tenant" {
			return fmt.Errorf("property type is owned by code")
		}
		d.PropertyVersions = maps.Clone(old.PropertyVersions)
		if d.PropertyVersions == nil {
			d.PropertyVersions = map[string]platform.PropertyType{}
		}
		if old.PropertyType != nil {
			d.PropertyVersions[old.Version] = *old.PropertyType
		}
		if prior, ok := d.PropertyVersions[d.Version]; ok && string(platform.Raw(prior)) != string(platform.Raw(p)) {
			return fmt.Errorf("property version bytes cannot change")
		}
	}
	d.PropertyVersions[d.Version] = p
	if i >= 0 {
		h.t.definitions[i] = d
	} else {
		h.t.definitions = append(h.t.definitions, d)
	}
	slices.SortFunc(h.t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
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
	return nil
}
