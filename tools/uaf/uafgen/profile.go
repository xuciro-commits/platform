// Package uafgen compiles original OMG XMI into runtime metamodel data.
// Only the UAF generation command imports this package.
package uafgen

import (
	"encoding/xml"
	"fmt"
	"strings"

	"platformserver/tools/uaf"
)

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
func Parse(raw []byte) (*uaf.Metamodel, error) {
	var root xmiNode
	if err := xml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	m := &uaf.Metamodel{Stereotypes: map[string]*uaf.Stereotype{}, Enumerations: map[string]*uaf.Enumeration{}}
	byID := map[string]string{}
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
				byID[n.ID] = n.Name
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
			e := &uaf.Enumeration{Name: n.Name}
			for _, c := range n.Children {
				if c.Type == "uml:EnumerationLiteral" {
					e.Literals = append(e.Literals, c.Name)
				}
			}
			m.Enumerations[n.Name] = e
			return
		case "uml:Stereotype":
			s := &uaf.Stereotype{Name: n.Name, ID: n.ID, Abstract: n.Abstract == "true"}
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
					if g := byID[c.General]; g != "" {
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
					p := uaf.Property{Name: c.Name}
					for _, x := range c.Children {
						switch {
						case x.XMLName.Local == "type" && x.Href != "":
							p.Type = x.Href[strings.LastIndex(x.Href, "#")+1:]
						case x.XMLName.Local == "type" && x.IDRef != "":
							p.Type = byID[x.IDRef]
						case x.XMLName.Local == "upperValue":
							p.Many = x.Value == "*" || strings.TrimSpace(x.Text) == "*"
						case x.Type == "uml:Comment":
							p.Description = strings.TrimSpace(x.Body)
						}
					}
					if p.Type == "" && c.TypeRef != "" {
						p.Type = byID[c.TypeRef]
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
