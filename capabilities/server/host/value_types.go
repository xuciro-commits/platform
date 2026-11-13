package host

import (
	"platformserver/kernel/records"
	"platformserver/platform"
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
	return h.definitionInstall().ValueType(v, version)
}
