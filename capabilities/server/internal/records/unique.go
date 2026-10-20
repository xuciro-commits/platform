package records

import "platformserver/platform"

// A rule index uses the same persistent AVL tree as records and references.
// Buckets retain every occupant, including old duplicate/archived rows; a
// derived index must not silently repair historical business data.
type UniqueIndex struct {
	Rule platform.Constraint
	Keys Index[Index[bool]]
	Rows Index[string]
}

func (index *UniqueIndex) Replace(id, key string) {
	before := index.Rows.Get(id)
	if before == key {
		return
	}
	if before != "" {
		bucket := index.Keys.Get(before)
		bucket.Remove(id)
		if bucket.Len() == 0 {
			index.Keys.Remove(before)
		} else {
			index.Keys.Set(before, bucket)
		}
		index.Rows.Remove(id)
	}
	if key != "" {
		bucket := index.Keys.Get(key)
		bucket.Set(id, true)
		index.Keys.Set(key, bucket)
		index.Rows.Set(id, key)
	}
}
