package build

import (
	"fmt"
	"platformserver/platform"
)

func propertyBindings(fields []Field) map[string]platform.AssetBinding {
	out := map[string]platform.AssetBinding{}
	for _, f := range fields {
		if f.Property != nil {
			out[f.Name] = *f.Property
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
func (b *Build) checkPropertyFields(fields []Field) error {
	for _, f := range fields {
		if f.Property == nil {
			continue
		}
		if b.host == nil || f.Property.Ref.App != ID {
			return fmt.Errorf("shared property needs its canonical tenant owner")
		}
		p, ok := b.host.ResolvePropertyType(*f.Property)
		if !ok {
			return fmt.Errorf("shared property version is unavailable")
		}
		if f.Choices != "" || f.Ref != "" || f.Inverse != "" {
			return fmt.Errorf("shared scalar property cannot declare another field shape")
		}
		if err := p.CheckField(platform.FieldInfo{Name: f.Name, Type: f.Type, Title: f.Title, Required: f.Required}); err != nil {
			return err
		}
	}
	return nil
}
