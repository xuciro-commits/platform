package enterprise

import (
	"fmt"
	"strings"
)

// ModelExample is a read-only preview of an owner-maintained, versioned seed.
// Applying it uses the same template builder as the empty-model wizard.
type ModelExample struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Model       Model  `json:"model"`
}

type exampleSpec struct {
	id, title, description string
	params                 SeedParams
}

var exampleSpecs = []exampleSpec{
	{"small-v1", "Small enterprise", "A company, teams, posts, a site and capabilities.", SeedParams{Scale: "S", Name: "Example company", Headcount: 50, Industry: "services"}},
	{"hotel-v1", "Hotel", "A hotel with service departments, posts, facilities and equipment.", SeedParams{Scale: "M", Name: "Example hotel", Headcount: 500, Industry: "hospitality"}},
	{"factory-v1", "Factory", "A factory with departments, production lines, stations and machines.", SeedParams{Scale: "M", Name: "Example factory", Headcount: 500, Industry: "manufacturing"}},
	{"group-v1", "Enterprise group", "A group with subsidiaries, sites, shared services and governance.", SeedParams{Scale: "XL", Name: "Example group", Headcount: 50000, Industry: "manufacturing"}},
}

func ModelExamples() ([]ModelExample, error) {
	out := make([]ModelExample, 0, len(exampleSpecs))
	for _, spec := range exampleSpecs {
		model, err := Template(spec.params)
		if err != nil {
			return nil, err
		}
		out = append(out, ModelExample{ID: spec.id, Title: spec.title, Description: spec.description, Model: model})
	}
	return out, nil
}

// appendExample never rewrites an existing element, relationship, view or
// calendar. Each application receives its own namespace and remains editable.
func (m Model) appendExample(example, prefix, name, parent string, day Date) (Model, error) {
	if prefix == "" || len(prefix) > 80 || strings.ContainsAny(prefix, "/\\ \t\r\n") {
		return Model{}, fmt.Errorf("the example application needs a bounded identifier")
	}
	var params *SeedParams
	for _, spec := range exampleSpecs {
		if spec.id == example {
			p := spec.params
			params = &p
			break
		}
	}
	if params == nil {
		return Model{}, fmt.Errorf("unknown enterprise example %q", example)
	}
	if strings.TrimSpace(name) == "" {
		return Model{}, fmt.Errorf("the enterprise needs a name")
	}
	if parent != "" {
		el := m.element(parent)
		if el == nil || el.Stereotype != Organization || el.Owner != "" || !activeOn(el.From, el.Until, day) {
			return Model{}, fmt.Errorf("the example's parent must be a live local organisation")
		}
	}
	params.Name, params.Day = name, day
	seed, err := Template(*params)
	if err != nil {
		return Model{}, err
	}
	ref := func(id string) string { return prefix + "-" + id }
	used := map[string]bool{}
	for _, x := range m.Elements {
		used[x.ID] = true
	}
	for _, x := range m.Relationships {
		used[x.ID] = true
	}
	for _, x := range m.Kinds {
		used[x.ID] = true
	}
	for _, x := range m.Views {
		used[x.ID] = true
	}
	reserve := func(id string) error {
		if used[id] {
			return fmt.Errorf("the example application identifier is already in use")
		}
		used[id] = true
		return nil
	}
	for i := range seed.Kinds {
		seed.Kinds[i].ID = ref(seed.Kinds[i].ID)
		if err := reserve(seed.Kinds[i].ID); err != nil {
			return Model{}, err
		}
	}
	for i := range seed.Elements {
		seed.Elements[i].ID = ref(seed.Elements[i].ID)
		if err := reserve(seed.Elements[i].ID); err != nil {
			return Model{}, err
		}
	}
	for i := range seed.Relationships {
		r := &seed.Relationships[i]
		r.ID, r.Source, r.Target = ref(r.ID), ref(r.Source), ref(r.Target)
		if r.Kind != "" {
			r.Kind = ref(r.Kind)
		}
		if err := reserve(r.ID); err != nil {
			return Model{}, err
		}
	}
	for i := range seed.Views {
		v := &seed.Views[i]
		v.ID = ref(v.ID)
		v.Name = name + " · " + v.Name
		if err := reserve(v.ID); err != nil {
			return Model{}, err
		}
		for j := range v.Elements {
			v.Elements[j] = ref(v.Elements[j])
		}
		if len(v.Layout) > 0 {
			layout := map[string][2]float64{}
			for id, point := range v.Layout {
				layout[ref(id)] = point
			}
			v.Layout = layout
		}
	}
	if parent != "" {
		kind := ""
		for _, k := range seed.Kinds {
			if k.Kind == "management" {
				kind = k.ID
				break
			}
		}
		seed.Relationships = append(seed.Relationships, Relationship{ID: prefix + "-parent", Stereotype: Placement, Kind: kind, Source: seed.Elements[0].ID, Target: parent, Relation: "part of", From: day})
		if err := reserve(prefix + "-parent"); err != nil {
			return Model{}, err
		}
		seed.Views[0].Elements = append(seed.Views[0].Elements, parent)
	}
	out := copyModel(m)
	if out.UAF == "" {
		out.UAF = seed.UAF
	}
	if out.Scale == "" {
		out.Scale = seed.Scale
	}
	out.Kinds = append(out.Kinds, seed.Kinds...)
	out.Elements = append(out.Elements, seed.Elements...)
	out.Relationships = append(out.Relationships, seed.Relationships...)
	out.Views = append(out.Views, seed.Views...)
	return out, nil
}
