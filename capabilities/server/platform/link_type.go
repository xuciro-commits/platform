package platform

import (
	"fmt"
	"slices"
)

type LinkTraversal struct {
	Binding   AssetBinding `json:"binding"`
	Direction string       `json:"direction"`
	ID        string       `json:"id"`
}

// LinkType names a reference-backed relationship. Its instances remain in the
// child object's scalar field; this declaration grants no extra write rights.
type LinkType struct {
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Parent       AssetRef `json:"parent"`
	Child        AssetRef `json:"child"`
	Via          string   `json:"via"`
	Forward      string   `json:"forward"`
	Reverse      string   `json:"reverse"`
	Storage      string   `json:"storage"`
	Cardinality  string   `json:"cardinality"`
	Required     bool     `json:"required"`
	DeletePolicy string   `json:"deletePolicy"`
}

func (l LinkType) Check() error {
	if !pageNodeID.MatchString(l.Name) || l.Title == "" || l.Description == "" || len(l.Title) > 1024 || len(l.Description) > 16384 || !pageNodeID.MatchString(l.Via) || !pageNodeID.MatchString(l.Forward) || !pageNodeID.MatchString(l.Reverse) || l.Parent.Check() != nil || l.Child.Check() != nil || l.Parent.Kind != AssetObject || l.Child.Kind != AssetObject || l.Storage != "reference" || !slices.Contains([]string{"one-to-many", "one-to-one"}, l.Cardinality) || !slices.Contains([]string{"owner", "restrict-active"}, l.DeletePolicy) || l.DeletePolicy == "restrict-active" && l.Parent.App != l.Child.App {
		return fmt.Errorf("link type needs typed objects, two names and a supported reference cardinality profile")
	}
	return nil
}
func (l LinkType) Contract() int {
	if l.DeletePolicy == "restrict-active" {
		return 3
	}
	if l.Cardinality == "one-to-one" {
		return 2
	}
	return 1
}

func (l LinkType) CheckSchema(parent, child EntityInfo) error {
	if err := l.Check(); err != nil {
		return err
	}
	f, ok := child.Field(l.Via)
	if l.Parent.Name != parent.Type || l.Parent.App != parent.App || l.Child.Name != child.Type || l.Child.App != child.App || !ok || f.Type != "reference" || f.Ref != parent.Type || f.Required != l.Required {
		return fmt.Errorf("link type requires its original typed reference and field requirement")
	}
	return nil
}
func (d Definition) LinkVersion(version string) *Definition {
	if d.Version == version && d.LinkType != nil {
		return &d
	}
	l, ok := d.LinkVersions[version]
	if !ok {
		return nil
	}
	d.Version = version
	d.LinkType = &l
	d.ContractVersion = l.Contract()
	d.LinkVersions = nil
	return &d
}

func (d Definition) QuerySourceVersion(version string) *Definition {
	if d.Ref.Kind == AssetLinkType {
		return d.LinkVersion(version)
	}
	return d.QueryVersion(version)
}
