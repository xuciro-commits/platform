package platform

import (
	"fmt"
	"slices"
	"strings"
)

// PropertyType owns reusable scalar meaning and presentation. Local field
// identity, required values and access remain the consuming object's rules.
type PropertyType struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

func (p PropertyType) Check() error {
	if !pageNodeID.MatchString(p.Name) || p.Title == "" || len(p.Title) > 1024 || strings.ContainsAny(p.Title, "\"\\") || p.Description == "" || len(p.Description) > 16384 || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "date", "datetime", "boolean"}, p.Type) {
		return fmt.Errorf("property type needs an identity, scalar type and bounded meaning")
	}
	return nil
}
func (p PropertyType) CheckField(f FieldInfo) error {
	if err := p.Check(); err != nil {
		return err
	}
	if f.Type != p.Type || f.Title != p.Title || f.Ref != "" || len(f.Choices) != 0 {
		return fmt.Errorf("field differs from its bound shared property")
	}
	return nil
}
func (d Definition) PropertyVersion(version string) *Definition {
	if d.Version == version && d.PropertyType != nil {
		return &d
	}
	p, ok := d.PropertyVersions[version]
	if !ok {
		return nil
	}
	d.Version = version
	d.PropertyType = &p
	d.PropertyVersions = nil
	return &d
}
