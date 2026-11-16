package platform

import (
	"fmt"
	"slices"
)

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
			if !ok || !PageNodeID.MatchString(id) || path[id] || depth > QuerySetMaxDepth || nodes > QuerySetMaxNodes || q.Object != plan.Object || q.Owner != plan.Owner || q.ItemOwner != plan.ItemOwner {
				return fmt.Errorf("query set %s has a missing, cyclic, incompatible or oversized source", root)
			}
			if q.Set == nil {
				return nil
			}
			if !slices.Contains(PageWidgets.Runtime.Query.Set.Operations, q.Set.Op) || len(q.Set.Inputs) != 2 {
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
