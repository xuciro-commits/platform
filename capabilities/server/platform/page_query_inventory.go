package platform

import "encoding/json"

// ActiveQueryPlans excludes only declarations with no reachable use. Reference
// discovery is conservative: any matching declaration ID activates that edge.
// Titles/literals that collide with IDs may retain reads, never suppress them.
func (d *PageDocument) ActiveQueryPlans(sections []Section) map[string]bool {
	active := map[string]bool{}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Query.InventoryUIProfile) || d.Root == "" {
		for id := range d.Queries {
			active[id] = true
		}
		return active
	}
	widgets := map[string]Section{}
	for _, s := range sections {
		widgets[s.ID] = s
	}
	seen := map[string]bool{}
	var scan func(any)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		if q, ok := d.Queries[id]; ok {
			active[id] = true
			scan(q)
		}
		if v, ok := d.Variables[id]; ok {
			scan(v)
		}
		if n, ok := d.Nodes[id]; ok {
			scan(n)
		}
		if s, ok := widgets[id]; ok {
			scan(s)
		}
	}
	var walkJSON func(any)
	walkJSON = func(value any) {
		switch v := value.(type) {
		case string:
			visit(v)
		case []any:
			for _, x := range v {
				walkJSON(x)
			}
		case map[string]any:
			for _, x := range v {
				walkJSON(x)
			}
		}
	}
	scan = func(value any) {
		raw, err := json.Marshal(value)
		if err != nil {
			for id := range d.Queries {
				active[id] = true
			}
			return
		}
		var decoded any
		if json.Unmarshal(raw, &decoded) != nil {
			for id := range d.Queries {
				active[id] = true
			}
			return
		}
		walkJSON(decoded)
	}
	visit(d.Root)
	for _, overlay := range d.Overlays {
		scan(overlay)
	}
	scan(d.Interface)
	scan(d.Events)
	main := active
	active = map[string]bool{}
	seen = map[string]bool{}
	for _, entry := range d.UnusedWidgets {
		visit(entry.Node)
	}
	for id := range d.Queries {
		if !active[id] {
			main[id] = true
		}
	}
	return main
}
