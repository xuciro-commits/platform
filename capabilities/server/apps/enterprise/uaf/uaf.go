package uaf

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

//go:embed spec/uaf-1.3.json
var specJSON []byte

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
	current  *Metamodel
	loadErr  error
)

func Releases() ([]*Metamodel, error) {
	loadOnce.Do(func() {
		var m Metamodel
		if err := json.Unmarshal(specJSON, &m); err != nil {
			loadErr = err
			return
		}
		current = &m
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return []*Metamodel{current}, nil
}

func Current() *Metamodel {
	rs, err := Releases()
	if err != nil || len(rs) == 0 {
		panic(fmt.Sprintf("uaf: no embedded release: %v", err))
	}
	return rs[0]
}

func Release(version string) (*Metamodel, bool) {
	if _, err := Releases(); err != nil || current == nil || current.Version != version {
		return nil, false
	}
	return current, true
}
