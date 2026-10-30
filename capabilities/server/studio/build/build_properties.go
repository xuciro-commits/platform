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

const SchemaPropertyType = PropertyTypeType + ".publish"

type PropertyType struct {
	platform.Record
	Name        string   `json:"name" field:"required,search"`
	Title       string   `json:"title" field:"required,search"`
	Description string   `json:"description" field:"required" type:"longtext"`
	Type        string   `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean"`
	State       string   `json:"state" field:"readonly" choices:"draft,published"`
	Version     int      `json:"version,omitempty" field:"readonly"`
	Published   string   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string `json:"versions,omitempty" field:"readonly"`
}

func (f PropertyType) Definition() platform.PropertyType {
	return platform.PropertyType{Name: f.Name, Title: f.Title, Description: f.Description, Type: f.Type}
}

var propertyKind = resourceKind[PropertyType, platform.PropertyType]{
	asset: platform.AssetPropertyType, typ: PropertyTypeType, prefix: "property", noun: "propertyType", title: "Property type", plural: "Property types", capability: "propertytypes",
	description: "Reusable scalar meaning; object fields keep their own identity and access.",
	transition:  "Install this shared scalar property as its next retained version.",
	immutable:   "A published property keeps its identity and scalar type",
	records:     func(b *Build) map[string]PropertyType { return b.propertyTypes },
	validate: func(_ *Build, f PropertyType) (PropertyType, error) {
		if !Named(f.Name) {
			return f, fmt.Errorf("Property name must use lower-case letters and digits")
		}
		return f, f.Definition().Check()
	},
	identity: func(a, b PropertyType) bool { return a.Name == b.Name && a.Type == b.Type },
	fromDeclaration: func(p platform.PropertyType) PropertyType {
		return PropertyType{Name: p.Name, Title: p.Title, Description: p.Description, Type: p.Type}
	},
	install: func(h platform.BuildHost, c platform.Caller, d platform.PropertyType, version int) error {
		return h.InstallPropertyType(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.PropertyType) error { return h.ValidateInstallPropertyType(d) },
}

const PropertyTypeType = "build.propertytype"
