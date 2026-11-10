package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

func (p Page) CheckQuerySetConditions(named map[AssetBinding]NamedQuery) error {
	schema := p.InputSchema
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		q := schema.Queries[id]
		count := 0
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
	for id, q := range schema.Queries {
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

func (d PageInputSchema) CheckQuerySets() error {
	for root, plan := range d.Queries {
		if plan.Set == nil {
			continue
		}
		if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.18") {
			return fmt.Errorf("query set %s requires v2.18", root)
		}
		nodes := 0
		var visit func(string, int, map[string]bool) error
		visit = func(id string, depth int, path map[string]bool) error {
			nodes++
			q, ok := d.Queries[id]
			if q.Query != nil && q.Query.Ref.Kind == AssetLinkType {
				return fmt.Errorf("link traversal is not a set predicate source")
			}
			if !ok || !pageNodeID.MatchString(id) || path[id] || depth > QuerySetMaxDepth || nodes > QuerySetMaxNodes || q.Object != plan.Object || q.Owner != plan.Owner || q.ItemOwner != plan.ItemOwner {
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
