package records

import (
	"slices"
)

const changesKept = 10000

type RecentChanges struct {
	Entries Index[[]string]
	order   []string
}

func (r RecentChanges) Fork() RecentChanges {
	return RecentChanges{Entries: r.Entries, order: slices.Clone(r.order)}
}

func (r *RecentChanges) Remember(change, ref string) {
	if r.Entries.Get(change) == nil {
		r.order = append(r.order, change)
		if len(r.order) > changesKept {
			r.Entries.Remove(r.order[0])
			r.order = r.order[1:]
		}
	}
	if refs := r.Entries.Get(change); !slices.Contains(refs, ref) {
		r.Entries.Set(change, append(slices.Clone(refs), ref))
	}
}
