package platformserver

import (
	"fmt"
	"platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/studio/releases"
	"slices"
	"strconv"
)

func (h hostView) ResolveValueType(binding platform.AssetBinding) (platform.ValueType, bool) {
	if binding.Ref.Kind != platform.AssetValueType || binding.SourceVersion == "" {
		return platform.ValueType{}, false
	}
	for _, d := range h.t.definitions {
		if d.Ref == binding.Ref {
			if selected := d.ValueVersion(binding.SourceVersion); selected != nil {
				return *selected.ValueType, true
			}
			break
		}
	}
	return platform.ValueType{}, false
}

func (r runtime) ResolveValueType(binding platform.AssetBinding) (platform.ValueType, bool) {
	return (hostView{t: r.t}).ResolveValueType(binding)
}

func (d *stagedDecision) ResolveValueType(binding platform.AssetBinding) (platform.ValueType, bool) {
	return (hostView{t: d.tenant}).ResolveValueType(binding)
}

func (h hostView) ValidateInstallValueType(v platform.ValueType) error { return v.Check() }

func (h hostView) ValidateObjectPresentation(info platform.EntityInfo) error {
	_, err := records.CompileFormats(info)
	return err
}

func (h hostView) InstallValueType(c platform.Caller, v platform.ValueType, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if err := v.Check(); err != nil {
		return err
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("value type needs a retained version")
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetValueType, Name: v.Name}
	d := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".value-" + strconv.Itoa(version), ContractVersion: 1, ValueType: &v, ValueVersions: map[string]platform.ValueType{}}
	d, err := h.t.describeDefinition(d)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		old := h.t.definitions[i]
		if old.Source != "tenant" {
			return fmt.Errorf("value type is owned by code")
		}
		var ok bool
		d.ValueVersions, ok = releaseowner.RetainedDefinitionVersions(old.ValueVersions, old.Version, old.ValueType, d.Version, v)
		if !ok {
			return fmt.Errorf("value type version bytes cannot change")
		}
	}
	d.ValueVersions[d.Version] = v
	h.t.storeDefinition(i, d)
	return nil
}

func (t *Tenant) checkObjectValueTypes(info platform.EntityInfo) error {
	for _, f := range info.Fields {
		if f.ValueType == nil {
			continue
		}
		v, ok := (hostView{t: t}).ResolveValueType(*f.ValueType)
		if !ok {
			return fmt.Errorf("field %s has an unavailable value type version", f.Name)
		}
		if err := v.CheckField(f); err != nil {
			return err
		}
	}
	return nil
}
