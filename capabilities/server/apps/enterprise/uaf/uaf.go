// Package uaf loads the OMG Unified Architecture Framework metamodel (compiled
// runtime JSON index or official OMG XMI profile) and answers what the
// enterprise model may contain: stereotypes, their domain and aspect in the UAF
// grid, their generalisations, attributes and enumerations (ADR-0067 D1).
package uaf

import (
	"embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed spec/*
var spec embed.FS

type Stereotype struct {
	Name        string     `json:"name"`
	ID          string     `json:"id"`
	Domain      string     `json:"domain"`
	Aspect      string     `json:"aspect,omitempty"`
	Description string     `json:"description,omitempty"`
	Abstract    bool       `json:"abstract,omitempty"`
	Generals    []string   `json:"generals,omitempty"`
	Bases       []string   `json:"bases,omitempty"`
	Properties  []Property `json:"properties,omitempty"`
	Constraints []string   `json:"constraints,omitempty"`
	Client      []string   `json:"client,omitempty"`
	Supplier    []string   `json:"supplier,omitempty"`
}

type Property struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Many        bool   `json:"many,omitempty"`
	Description string `json:"description,omitempty"`
}

type Enumeration struct {
	Name     string   `json:"name"`
	Literals []string `json:"literals"`
}

type Metamodel struct {
	Version      string                  `json:"version"`
	URI          string                  `json:"uri"`
	Stereotypes  map[string]*Stereotype  `json:"stereotypes"`
	Enumerations map[string]*Enumeration `json:"enumerations"`
	Domains      []string                `json:"domains"`
	byID         map[string]string
}

var relationshipBases = map[string]bool{"Association": true, "Dependency": true, "Abstraction": true, "Realization": true, "Connector": true, "InformationFlow": true, "Generalization": true, "Usage": true}

func (s *Stereotype) Relationship() bool {
	for _, b := range s.Bases {
		if relationshipBases[b] {
			return true
		}
	}
	return false
}

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

func (m *Metamodel) Relationship(s string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(n string) bool {
		st := m.Stereotypes[n]
		if st == nil || seen[n] {
			return false
		}
		seen[n] = true
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

func Releases() ([]*Metamodel, error) {
	loadOnce.Do(func() {
		loaded = map[string]*Metamodel{}
		entries, _ := fs.ReadDir(spec, "spec")
		for _, e := range entries {
			name := e.Name()
			switch {
			case strings.HasPrefix(name, "uaf-") && strings.HasSuffix(name, ".json"):
				version := strings.TrimSuffix(strings.TrimPrefix(name, "uaf-"), ".json")
				raw, err := spec.ReadFile(path.Join("spec", name))
				if err != nil {
					loadErr = err
					return
				}
				var m Metamodel
				if err := json.Unmarshal(raw, &m); err != nil {
					loadErr = fmt.Errorf("%s: %w", name, err)
					return
				}
				m.Version = version
				loaded[version] = &m
			case strings.HasPrefix(name, "uaf-") && strings.HasSuffix(name, "-profile.xmi"):
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

func Current() *Metamodel {
	rs, err := Releases()
	if err != nil || len(rs) == 0 {
		panic(fmt.Sprintf("uaf: no embedded release: %v", err))
	}
	return rs[0]
}

func Release(version string) (*Metamodel, bool) {
	if _, err := Releases(); err != nil {
		return nil, false
	}
	m, ok := loaded[version]
	return m, ok
}

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

// Parse reads an official OMG UAF profile XMI so new UAF releases or audits
// can compile or verify spec/uaf-<version>.json directly from OMG XMI.
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
