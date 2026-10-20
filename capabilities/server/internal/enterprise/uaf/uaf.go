// Package uaf describes the OMG Unified Architecture Framework from generated
// standard data. Original XMI remains the source; parsing is a generation step
// and never runs while an enterprise model is read or decided (ADR-0064).
package uaf

import "sort"

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
