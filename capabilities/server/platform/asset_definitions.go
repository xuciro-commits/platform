package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type LinkTraversal struct {
	Binding   AssetBinding `json:"binding"`
	Direction string       `json:"direction"`
	ID        string       `json:"id"`
}

type LinkType struct {
	Roles        []string `json:"roles,omitempty"`
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
	if l.Storage == "edge" {
		if !pageNodeID.MatchString(l.Name) || l.Title == "" || l.Description == "" || len(l.Title) > 1024 || len(l.Description) > 16384 || l.Parent.Check() != nil || l.Child.Check() != nil || l.Parent.Kind != AssetObject || l.Child.Kind != AssetObject || l.Parent.App != l.Child.App || l.Via != "" || !pageNodeID.MatchString(l.Forward) || !pageNodeID.MatchString(l.Reverse) || !slices.Contains([]string{"one-to-many", "one-to-one", "many-to-many"}, l.Cardinality) || !slices.Contains([]string{"owner", "restrict-active"}, l.DeletePolicy) || len(l.Roles) == 0 || len(l.Roles) > 16 {
			return fmt.Errorf("owned edge needs same-owner objects, explicit roles and supported cardinality")
		}
		seen := map[string]bool{}
		for _, role := range l.Roles {
			if role == "" || len(role) > 64 || strings.TrimSpace(role) != role || seen[role] {
				return fmt.Errorf("owned edge roles must be unique nonempty names")
			}
			seen[role] = true
		}
		return nil
	}
	if !pageNodeID.MatchString(l.Name) || l.Title == "" || l.Description == "" || len(l.Title) > 1024 || len(l.Description) > 16384 || !pageNodeID.MatchString(l.Via) || !pageNodeID.MatchString(l.Forward) || !pageNodeID.MatchString(l.Reverse) || l.Parent.Check() != nil || l.Child.Check() != nil || l.Parent.Kind != AssetObject || l.Child.Kind != AssetObject || l.Storage != "reference" || !slices.Contains([]string{"one-to-many", "one-to-one"}, l.Cardinality) || !slices.Contains([]string{"owner", "restrict-active"}, l.DeletePolicy) || l.DeletePolicy == "restrict-active" && l.Parent.App != l.Child.App {
		return fmt.Errorf("link type needs typed objects, two names and a supported reference cardinality profile")
	}
	return nil
}

func (l LinkType) Contract() int {
	if l.Storage == "edge" {
		return 4
	}
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
	if l.Storage == "edge" {
		if l.Parent.Name != parent.Type || l.Child.Name != child.Type || l.Parent.App != parent.App || l.Child.App != child.App {
			return fmt.Errorf("owned edge endpoint types differ")
		}
		return nil
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

func (q NamedQuery) Check() error {
	if q.Name == "" || q.Title == "" || (q.Object == "") == (q.Interface == "") || len(q.Name) > 80 || len(q.Title) > 1024 || len(q.Description) > 4096 || q.Limit < 0 || q.Limit > 200 || len(q.Sort) > 4 || len(q.Domain) > 16384 {
		return fmt.Errorf("query has an invalid identity or budget")
	}
	if q.Interface != "" {
		owner, _, _ := strings.Cut(q.Interface, ".")
		if q.By != "" || q.InterfaceShape == nil || q.InterfaceShape.Name != q.Interface || q.InterfaceShape.Check(owner) != nil || len(q.Implementations) > 256 {
			return fmt.Errorf("interface query needs its frozen shape and implementation list, without an untyped parent input")
		}
		for i, typ := range q.Implementations {
			if typ == "" || slices.Contains(q.Implementations[:i], typ) {
				return fmt.Errorf("interface query implementations must be distinct object types")
			}
		}
	} else if q.InterfaceShape != nil || len(q.Implementations) != 0 {
		return fmt.Errorf("object query cannot carry an interface binding")
	}
	var terms []json.RawMessage
	if len(q.Domain) > 0 && (json.Unmarshal(q.Domain, &terms) != nil || len(terms) > 32) {
		return fmt.Errorf("query domain needs a bounded list of conditions")
	}
	return nil
}

func (q NamedQuery) Dependencies() []AssetRef {
	types := q.Implementations
	if q.Object != "" {
		types = []string{q.Object}
	}
	refs := make([]AssetRef, 0, len(types))
	for _, typ := range types {
		owner, _, _ := strings.Cut(typ, ".")
		refs = append(refs, AssetRef{App: owner, Kind: AssetObject, Name: typ})
	}
	slices.SortFunc(refs, func(a, b AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return refs
}

func (d Definition) QueryVersion(version string) *Definition {
	if version == d.Version && d.Query != nil {
		return &d
	}
	q, ok := d.QueryVersions[version]
	if !ok {
		return nil
	}
	body, err := json.Marshal(q)
	if err != nil {
		return nil
	}
	description, err := DescribeAsset(ReleaseAsset{Ref: d.Ref, SourceVersion: version, Body: body})
	if err != nil {
		return nil
	}
	d.Query, d.Version, d.QueryVersions = &q, version, nil
	d.Requires, d.Provides = description.Requires, description.Provides
	return &d
}

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

type SampleRecords struct {
	Type    string            `json:"type"`
	Records []json.RawMessage `json:"records"`
}

type SampleSet struct {
	Name    string          `json:"name"`
	Title   string          `json:"title"`
	Samples []SampleRecords `json:"samples"`
}

var sampleSetName = regexp.MustCompile(`^[a-z][a-z0-9]{0,63}$`)

func (s SampleSet) Check() error {
	if !sampleSetName.MatchString(s.Name) || s.Title == "" || len(s.Title) > 256 || len(s.Samples) > 8 {
		return fmt.Errorf("Sample set needs a name, title and at most eight model types")
	}
	seen := map[string]bool{}
	size := 0
	for _, group := range s.Samples {
		if ModelObject(group.Type).Check() != nil || seen[group.Type] || len(group.Records) > 100 {
			return fmt.Errorf("Sample model types must be unique, with at most 100 records each")
		}
		seen[group.Type] = true
		ids := map[string]bool{}
		for _, raw := range group.Records {
			size += len(raw)
			var row struct {
				ID string `json:"id"`
			}
			if size > 256<<10 || len(raw) > 32<<10 || invalidSampleRecord(raw) || json.Unmarshal(raw, &row) != nil || row.ID == "" || len(row.ID) > 128 || ids[row.ID] {
				return fmt.Errorf("Sample records need unique bounded IDs and at most 256 KiB in total")
			}
			ids[row.ID] = true
		}
	}
	return nil
}

func (s SampleSet) Dependencies() []AssetRef {
	refs := []AssetRef{}
	for _, group := range s.Samples {
		refs = append(refs, ModelObject(group.Type))
	}
	slices.SortFunc(refs, compareRef)
	return refs
}

func invalidSampleRecord(raw json.RawMessage) bool {
	_, err := DecodeValue(raw, 32<<10)
	return err != nil
}

type SchemaVersion struct {
	Version string `json:"version"`
	Family  uint32 `json:"family"`
}

func (v SchemaVersion) Check() error {
	raw, ok := strings.CutPrefix(v.Version, "schema.sha256.")
	decoded, err := hex.DecodeString(raw)
	if !ok || err != nil || len(decoded) != sha256.Size || strings.ToLower(raw) != raw || v.Family == 0 || v.Family > 64 {
		return fmt.Errorf("schema version needs its content identity and family")
	}
	return nil
}

func SchemaContentVersion(body json.RawMessage) (string, error) {
	canonical, err := canonicalBody(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "schema.sha256." + hex.EncodeToString(sum[:]), nil
}
