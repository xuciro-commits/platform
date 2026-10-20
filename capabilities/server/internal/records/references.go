package records

import "iter"

// Reference is one source record's derived reference and archive status.
// Field extraction and the meaning of the reference remain with the owner.
type Reference struct {
	ID, Target string
	Active     bool
}

type Bucket struct {
	IDs    Index[bool]
	Active int
}

// References keeps immutable target buckets. Copying it retains an independent
// snapshot; replacement copies only the affected buckets and index paths.
type References struct{ targets Index[Bucket] }

// ReferencesFrom builds a complete image without retaining insertion roots.
// The caller owns the temporary input, just as with From.
func ReferencesFrom(entries []Reference) (References, error) {
	groups := map[string]*struct {
		ids    []Entry[bool]
		active int
	}{}
	for _, entry := range entries {
		if entry.Target == "" {
			continue
		}
		group := groups[entry.Target]
		if group == nil {
			group = &struct {
				ids    []Entry[bool]
				active int
			}{}
			groups[entry.Target] = group
		}
		group.ids = append(group.ids, Entry[bool]{Key: entry.ID, Value: entry.Active})
		if entry.Active {
			group.active++
		}
	}
	targets := make([]Entry[Bucket], 0, len(groups))
	for target, group := range groups {
		ids, err := From(group.ids)
		if err != nil {
			return References{}, err
		}
		targets = append(targets, Entry[Bucket]{Key: target, Value: Bucket{IDs: ids, Active: group.active}})
	}
	index, err := From(targets)
	return References{targets: index}, err
}

func (r References) Get(target string) Bucket { return r.targets.Get(target) }

func (r References) All() iter.Seq2[string, Bucket] { return r.targets.All() }

// Replace receives the owner's previous and new targets. Buckets already own
// the old active status, so no second source-to-target index is maintained.
func (r *References) Replace(id, before, after string, active bool) {
	if before == after && (before == "" || r.targets.Get(before).IDs.Get(id) == active) {
		return
	}
	if before != "" {
		bucket := r.targets.Get(before)
		if bucket.IDs.Get(id) {
			bucket.Active--
		}
		bucket.IDs.Remove(id)
		if bucket.IDs.Len() == 0 {
			r.targets.Remove(before)
		} else {
			r.targets.Set(before, bucket)
		}
	}
	if after != "" {
		bucket := r.targets.Get(after)
		bucket.IDs.Set(id, active)
		if active {
			bucket.Active++
		}
		r.targets.Set(after, bucket)
	}
}
