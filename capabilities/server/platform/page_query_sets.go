package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Fixed named conditions and the parent term count at each expanded source,
// while dynamic parameter values retain the record owner's runtime budget.
func (p Page) CheckQuerySetConditions(named map[AssetBinding]NamedQuery) error {
	if p.Document == nil {
		return nil
	}
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		q := p.Document.Queries[id]
		count := len(q.Conditions)
		if q.Query != nil {
			decl := named[*q.Query]
			var terms []json.RawMessage
			if json.Unmarshal(decl.Domain, &terms) == nil {
				count += len(terms)
			}
			if decl.By != "" {
				count++
			}
		}
		if count > QuerySetMaxConditions {
			return fmt.Errorf("query set source %s exceeds its condition budget", id)
		}
		if q.Set != nil {
			for _, input := range q.Set.Inputs {
				if err := visit(input); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for id, q := range p.Document.Queries {
		if q.Set != nil {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (q PageQuery) MissingSetInput(plans map[string]PageQuery) bool {
	if q.Set == nil {
		return false
	}
	for _, id := range q.Set.Inputs {
		if _, ok := plans[id]; !ok {
			return true
		}
	}
	return false
}

// Expand each root's explicit graph to the same bounded predicate tree the
// original record owner executes. A reused source counts on every occurrence.
func (d *PageDocument) CheckQuerySets() error {
	for root, plan := range d.Queries {
		if plan.Set == nil {
			continue
		}
		if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.18") {
			return fmt.Errorf("query set %s requires v2.18", root)
		}
		nodes := 0
		var visit func(string, int, map[string]bool) error
		visit = func(id string, depth int, path map[string]bool) error {
			nodes++
			q, ok := d.Queries[id]
			if !ok || !pageNodeID.MatchString(id) || path[id] || depth > QuerySetMaxDepth || nodes > QuerySetMaxNodes || q.Object != plan.Object || q.Owner != plan.Owner {
				return fmt.Errorf("query set %s has a missing, cyclic, incompatible or oversized source", root)
			}
			if q.Set == nil {
				return nil
			}
			if !slices.Contains(pageWidgets.Runtime.Query.Set.Operations, q.Set.Op) || len(q.Set.Inputs) != 2 {
				return fmt.Errorf("query set %s needs a binary supported operation", id)
			}
			path[id] = true
			defer delete(path, id)
			for _, input := range q.Set.Inputs {
				if err := visit(input, depth+1, path); err != nil {
					return err
				}
			}
			return nil
		}
		if err := visit(root, 0, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}
