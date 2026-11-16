package platform

import (
	"slices"
	"strings"
)

func (q PageQuery) Variables() []string {
	ids := []string{}
	if q.Input != "" {
		ids = append(ids, q.Input)
	}
	for _, v := range q.Values() {
		if v.Variable != "" {
			ids = append(ids, v.Variable)
		}
	}
	if q.Filter != nil {
		for _, ref := range ConditionRefs(*q.Filter) {
			if name, ok := strings.CutPrefix(ref, "root."); ok {
				name, _, _ = strings.Cut(name, "#")
				if name != "" && !slices.Contains(ids, name) {
					ids = append(ids, name)
				}
			}
		}
	}
	return ids
}
