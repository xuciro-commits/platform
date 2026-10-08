// Package uaf reads the OMG Unified Architecture Framework profile as OMG
// publishes it (XMI) and answers what the enterprise model may contain: the
// stereotypes, their domain and aspect in the UAF grid, their generalisations,
// attributes and enumerations (ADR-0067 D1). The platform never writes its own
// copy of the metamodel: a new UAF release is a new XMI beside the old one.
package uaf

import (
	"embed"
	"encoding/xml"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed spec/*.xmi
var spec embed.FS

// Stereotype is one UAF construct: «ActualOrganization», «Capability», «FillsPost».
type Stereotype struct {
	Name        string   `json:"name"`
	ID          string   `json:"id"`
	Domain      string   `json:"domain"`           // Strategic, Operational, Personnel, Resources, Projects …
	Aspect      string   `json:"aspect,omitempty"` // Taxonomy, Structure, Connectivity, Processes, Roadmap …
	Description string   `json:"description,omitempty"`
	Abstract    bool     `json:"abstract,omitempty"`
	Generals    []string `json:"generals,omitempty"` // direct supertypes, by name
	// Bases are the UML metaclasses it extends: Class, InstanceSpecification,
	// Association, Dependency, Activity … A stereotype on an Association,
	// Dependency, Abstraction, Realization, Connector or InformationFlow is a
	// relationship; the rest are elements.
	Bases       []string   `json:"bases,omitempty"`
	Properties  []Property `json:"properties,omitempty"`
	Constraints []string   `json:"constraints,omitempty"`
	// Client and Supplier are the ends a plain "must be stereotyped" rule
	// states: "Value for the client metaproperty must be stereotyped «A»,
	// «B»". A compound rule (IsCapableToPerform's conditional pairs) is left in
	// Constraints as text and read by nobody but a person.
	Client   []string `json:"client,omitempty"`
	Supplier []string `json:"supplier,omitempty"`
}

// Property is a tagged value of a stereotype.
type Property struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // String, Boolean, Integer, Real, an enumeration name, or a stereotype name
	Many        bool   `json:"many,omitempty"`
	Description string `json:"description,omitempty"`
}

// Enumeration is a kind list: CapabilityKind, ProjectKind, LocationKind …
type Enumeration struct {
	Name     string   `json:"name"`
	Literals []string `json:"literals"`
}

// Metamodel is one UAF release as loaded.
type Metamodel struct {
	Version      string                  `json:"version"` // "1.3"
	URI          string                  `json:"uri"`     // http://www.omg.org/spec/UAF/20241101/UAF
	Stereotypes  map[string]*Stereotype  `json:"stereotypes"`
	Enumerations map[string]*Enumeration `json:"enumerations"`
	Domains      []string                `json:"domains"`
	byID         map[string]string
}

var relationshipBases = map[string]bool{"Association": true, "Dependency": true, "Abstraction": true, "Realization": true, "Connector": true, "InformationFlow": true, "Generalization": true, "Usage": true}

// Relationship reports whether the stereotype decorates a relationship rather
// than an element.
func (s *Stereotype) Relationship() bool {
	for _, b := range s.Bases {
		if relationshipBases[b] {
			return true
		}
	}
	return false
}

// Is reports whether s is name or specialises it.
func (m *Metamodel) Is(s, name string) bool {
	return m.is(s, name, map[string]bool{})
}

func (m *Metamodel) is(s, name string, seen map[string]bool) bool {
	if s == name {
		return true
	}
	if seen[s] {
		return false
	}
	seen[s] = true
	st := m.Stereotypes[s]
	if st == nil {
		return false
	}
	for _, g := range st.Generals {
		if m.is(g, name, seen) {
			return true
		}
	}
	return false
}

// Relationship reports whether the stereotype, or one it specialises, extends
// a UML relationship metaclass.
func (m *Metamodel) Relationship(s string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(n string) bool {
		st := m.Stereotypes[n]
		if st == nil || seen[n] {
			return false
		}
		seen[n] = true
		// The ends are the proof: a stereotype whose rules name a client and a
		// supplier joins two elements, whatever metaclass it extends (UAF
		// models MapsToGoal on Element, with the ends as properties).
		if st.Relationship() || len(st.Client) > 0 || len(st.Supplier) > 0 {
			return true
		}
		for _, g := range st.Generals {
			if walk(g) {
				return true
			}
		}
		return false
	}
	return walk(s)
}

// ends reads a plain endpoint rule: which end it constrains, and the
// stereotypes that end accepts. Only a body that names one end is read; a
// compound rule names both and stays text. Names keep their first-seen order
// and are deduplicated.
func ends(body string) (string, []string) {
	lower := strings.ToLower(body)
	client, supplier := strings.Contains(lower, "client metaproperty"), strings.Contains(lower, "supplier metaproperty")
	if client == supplier {
		return "", nil
	}
	end := "supplier"
	if client {
		end = "client"
	}
	var names []string
	seen := map[string]bool{}
	for rest := body; ; {
		i := strings.Index(rest, "\u00ab")
		if i < 0 {
			break
		}
		rest = rest[i+len("\u00ab"):]
		j := strings.Index(rest, "\u00bb")
		if j < 0 {
			break
		}
		name := strings.TrimSpace(rest[:j])
		rest = rest[j+len("\u00bb"):]
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return end, names
}

// Properties are the stereotype's own and inherited tagged values.
func (m *Metamodel) Properties(name string) []Property {
	var out []Property
	seen := map[string]bool{}
	var walk func(string, int)
	walk = func(n string, depth int) {
		st := m.Stereotypes[n]
		if st == nil || depth > 20 {
			return
		}
		for _, p := range st.Properties {
			if !seen[p.Name] {
				seen[p.Name] = true
				out = append(out, p)
			}
		}
		for _, g := range st.Generals {
			walk(g, depth+1)
		}
	}
	walk(name, 0)
	return out
}

// Names are the stereotypes in a stable order.
func (m *Metamodel) Names() []string {
	out := make([]string, 0, len(m.Stereotypes))
	for n := range m.Stereotypes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

var (
	loadOnce sync.Once
	loaded   map[string]*Metamodel
	loadErr  error
)

// Releases are the UAF releases embedded in this build, newest first.
func Releases() ([]*Metamodel, error) {
	loadOnce.Do(func() {
		loaded = map[string]*Metamodel{}
		entries, _ := fs.ReadDir(spec, "spec")
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, "-profile.xmi") {
				continue
			}
			version := strings.TrimSuffix(strings.TrimPrefix(name, "uaf-"), "-profile.xmi")
			raw, err := spec.ReadFile(path.Join("spec", name))
			if err != nil {
				loadErr = err
				return
			}
			m, err := Parse(raw)
			if err != nil {
				loadErr = fmt.Errorf("%s: %w", name, err)
				return
			}
			m.Version = version
			loaded[version] = m
		}
	})
	if loadErr != nil {
		return nil, loadErr
	}
	out := make([]*Metamodel, 0, len(loaded))
	for _, m := range loaded {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// Current is the newest embedded release.
func Current() *Metamodel {
	rs, err := Releases()
	if err != nil || len(rs) == 0 {
		panic(fmt.Sprintf("uaf: no embedded release: %v", err))
	}
	return rs[0]
}

// Release is one embedded release by version ("1.3").
func Release(version string) (*Metamodel, bool) {
	if _, err := Releases(); err != nil {
		return nil, false
	}
	m, ok := loaded[version]
	return m, ok
}

// --- XMI ---

type xmiNode struct {
	XMLName  xml.Name
	Type     string    `xml:"http://www.omg.org/spec/XMI/20131001 type,attr"`
	ID       string    `xml:"http://www.omg.org/spec/XMI/20131001 id,attr"`
	IDRef    string    `xml:"http://www.omg.org/spec/XMI/20131001 idref,attr"`
	Name     string    `xml:"name,attr"`
	Body     string    `xml:"body,attr"`
	General  string    `xml:"general,attr"`
	Href     string    `xml:"href,attr"`
	TypeRef  string    `xml:"type,attr"`
	Abstract string    `xml:"isAbstract,attr"`
	Value    string    `xml:"value,attr"`
	Text     string    `xml:",chardata"`
	Children []xmiNode `xml:",any"`
}

// Parse reads a UAF profile XMI.
func Parse(raw []byte) (*Metamodel, error) {
	var root xmiNode
	if err := xml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	m := &Metamodel{Stereotypes: map[string]*Stereotype{}, Enumerations: map[string]*Enumeration{}, byID: map[string]string{}}
	var profile *xmiNode
	for i := range root.Children {
		if root.Children[i].Type == "uml:Profile" {
			profile = &root.Children[i]
		}
	}
	if profile == nil {
		return nil, fmt.Errorf("no uml:Profile in the XMI")
	}
	for _, c := range profile.Children {
		if c.XMLName.Local == "URI" {
			m.URI = strings.TrimSpace(c.Text)
		}
	}
	// First pass: names by id (enumerations, stereotypes, data types), so
	// property types and generalisations resolve.
	var index func(n *xmiNode)
	index = func(n *xmiNode) {
		switch n.Type {
		case "uml:Stereotype", "uml:Enumeration", "uml:DataType", "uml:Class", "uml:PrimitiveType":
			if n.Name != "" {
				m.byID[n.ID] = n.Name
			}
		}
		for i := range n.Children {
			index(&n.Children[i])
		}
	}
	index(&root)
	seenDomain := map[string]bool{}
	var walk func(n *xmiNode, pkgs []string)
	walk = func(n *xmiNode, pkgs []string) {
		switch n.Type {
		case "uml:Package", "uml:Profile":
			if n.Name != "" && n.Type == "uml:Package" {
				pkgs = append(append([]string{}, pkgs...), n.Name)
			}
		case "uml:Enumeration":
			e := &Enumeration{Name: n.Name}
			for _, c := range n.Children {
				if c.Type == "uml:EnumerationLiteral" {
					e.Literals = append(e.Literals, c.Name)
				}
			}
			m.Enumerations[n.Name] = e
			return
		case "uml:Stereotype":
			s := &Stereotype{Name: n.Name, ID: n.ID, Abstract: n.Abstract == "true"}
			if len(pkgs) > 0 {
				s.Domain = pkgs[0]
				if !seenDomain[s.Domain] {
					seenDomain[s.Domain] = true
					m.Domains = append(m.Domains, s.Domain)
				}
			}
			if len(pkgs) > 1 {
				s.Aspect = pkgs[1]
			}
			for _, c := range n.Children {
				switch c.Type {
				case "uml:Comment":
					if s.Description == "" {
						s.Description = strings.TrimSpace(c.Body)
					}
				case "uml:Generalization":
					if g := m.byID[c.General]; g != "" {
						s.Generals = append(s.Generals, g)
					}
				case "uml:Constraint":
					for _, sp := range c.Children {
						for _, b := range sp.Children {
							if b.XMLName.Local == "body" && strings.TrimSpace(b.Text) != "" {
								body := strings.TrimSpace(b.Text)
								s.Constraints = append(s.Constraints, body)
								switch end, names := ends(body); {
								case end == "client":
									s.Client = append(s.Client, names...)
								case end == "supplier":
									s.Supplier = append(s.Supplier, names...)
								}
							}
						}
					}
				case "uml:Property":
					if base, ok := strings.CutPrefix(c.Name, "base_"); ok {
						s.Bases = append(s.Bases, base)
						continue
					}
					if c.Name == "" {
						continue
					}
					p := Property{Name: c.Name}
					for _, x := range c.Children {
						switch {
						case x.XMLName.Local == "type" && x.Href != "":
							p.Type = x.Href[strings.LastIndex(x.Href, "#")+1:]
						case x.XMLName.Local == "type" && x.IDRef != "":
							p.Type = m.byID[x.IDRef]
						case x.XMLName.Local == "upperValue":
							p.Many = x.Value == "*" || strings.TrimSpace(x.Text) == "*"
						case x.Type == "uml:Comment":
							p.Description = strings.TrimSpace(x.Body)
						}
					}
					if p.Type == "" && c.TypeRef != "" {
						p.Type = m.byID[c.TypeRef]
					}
					if p.Type == "" {
						p.Type = "String"
					}
					s.Properties = append(s.Properties, p)
				}
			}
			m.Stereotypes[n.Name] = s
			return
		}
		for i := range n.Children {
			walk(&n.Children[i], pkgs)
		}
	}
	walk(profile, nil)
	if len(m.Stereotypes) == 0 {
		return nil, fmt.Errorf("no stereotypes in the profile")
	}
	return m, nil
}
