package construction

import (
	"fmt"
	enterprisemodel "platformserver/internal/enterprise"
	"regexp"
	"strings"
)

// EnterpriseTarget (ADR-0022) lands a pipeline's rows in the enterprise model
// as elements of one stereotype owned on an external system's behalf: SAP
// HR/OM organisational units become ActualOrganizations under the org tree,
// AD departments posts, MES equipment ActualResources at locations. Columns
// name what each row gives; a value written "=literal" is a constant instead.
type EnterpriseTarget struct {
	Source     string `json:"source"`     // the system's name, e.g. sap-hr: element ids are <source>:<id>
	Stereotype string `json:"stereotype"` // element stereotype, e.g. ActualOrganization
	ID         string `json:"id"`         // the column with the system's stable id
	Name       string `json:"name"`
	ShortName  string `json:"shortName,omitempty"`
	Kind       string `json:"kind,omitempty"` // column or =literal, e.g. =plant
	// Parent is the column with the parent's id in the same system; Root the element the top ones sit
	// under (the tenant's own, e.g. the company); In the relationship kind the placement is made in.
	Parent   string `json:"parent,omitempty"`
	Root     string `json:"root,omitempty"`
	In       string `json:"in,omitempty"`
	Relation string `json:"relation,omitempty"` // e.g. part of
	// At places a resource or post at an element given by column (ids of the same system or =literal).
	At         string   `json:"at,omitempty"`
	AtKind     string   `json:"atKind,omitempty"` // the relationship stereotype towards At; default ResponsibleFor (owner → resource)
	From       string   `json:"from,omitempty"`
	Until      string   `json:"until,omitempty"`
	Properties []string `json:"properties,omitempty"` // columns copied as tagged values
}

var ValueSourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (e EnterpriseTarget) Check() error {
	switch {
	case !ValueSourceName.MatchString(e.Source):
		return fmt.Errorf("the source system is lower-case letters, digits and dashes, such as sap-hr")
	case e.Stereotype == "" || e.ID == "" || e.Name == "":
		return fmt.Errorf("an enterprise target names the stereotype, the id column and the name column")
	case e.Parent != "" && e.In == "":
		return fmt.Errorf("a parent column needs the relationship kind the placement is made in")
	}
	return nil
}

func (e EnterpriseTarget) Value(row map[string]any, column string) string {
	if strings.HasPrefix(column, "=") {
		return column[1:]
	}
	if column == "" {
		return ""
	}
	return ValueScalar(row[column])
}

func (e EnterpriseTarget) ElementID(raw string) string {
	if strings.HasPrefix(raw, "=") {
		return raw[1:]
	}
	return e.Source + ":" + raw
}

// Slice turns the rows into the elements and relationships one sync lands.
// Rows without an id or a name are skipped; the counts tell.
func (e EnterpriseTarget) Slice(rows []map[string]any) ([]enterprisemodel.Element, []enterprisemodel.Relationship, int) {
	var elements []enterprisemodel.Element
	var rels []enterprisemodel.Relationship
	skipped := 0
	ids := map[string]bool{}
	for _, row := range rows {
		raw, name := e.Value(row, e.ID), e.Value(row, e.Name)
		if raw == "" || name == "" || ids[raw] {
			skipped++
			continue
		}
		ids[raw] = true
		el := enterprisemodel.Element{ID: e.ElementID(raw), Stereotype: e.Stereotype, Name: name, ShortName: e.Value(row, e.ShortName), Kind: e.Value(row, e.Kind), From: e.Value(row, e.From), Until: e.Value(row, e.Until)}
		bag := map[string]any{}
		for _, c := range e.Properties {
			if v, ok := row[c]; ok && v != nil {
				bag[c] = v
			}
		}
		if len(bag) > 0 { // the system's own attributes live under its name; UAF tagged values stay the model's
			el.Properties = map[string]any{"source:" + e.Source: bag}
		}
		elements = append(elements, el)
		if parent := e.Value(row, e.Parent); e.Parent != "" && parent != "" && parent != raw {
			rels = append(rels, enterprisemodel.Relationship{ID: "pl:" + raw, Stereotype: enterprisemodel.Placement, Kind: e.In, Source: el.ID, Target: e.ElementID(parent), Relation: e.Relation, From: el.From, Until: el.Until})
		} else if e.Root != "" && e.In != "" {
			rels = append(rels, enterprisemodel.Relationship{ID: "pl:" + raw, Stereotype: enterprisemodel.Placement, Kind: e.In, Source: el.ID, Target: e.Root, Relation: e.Relation, From: el.From, Until: el.Until})
		}
		if at := e.Value(row, e.At); e.At != "" && at != "" {
			kind := e.AtKind
			if kind == "" {
				kind = enterprisemodel.Owns
			}
			src, dst := e.ElementID(at), el.ID
			if kind == enterprisemodel.FillsPost || kind == enterprisemodel.Membership || kind == enterprisemodel.Performs {
				src, dst = el.ID, e.ElementID(at) // the row's element is the one that fills, belongs or performs
			}
			rels = append(rels, enterprisemodel.Relationship{ID: "at:" + raw, Stereotype: kind, Source: src, Target: dst, From: el.From, Until: el.Until})
		}
	}
	return elements, rels, skipped
}
