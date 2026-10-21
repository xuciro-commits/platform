package records

import (
	"iter"
	"slices"
)

// RowSource belongs to one consistent read view. Rows retain only accepted
// private replacements; loading a committed row never installs a shared cache.
type RowSource interface {
	Get(string) *Row
	IDs(after string, limit int) []string
}

// Rows is either the original immutable in-memory index or a source with an
// immutable private overlay. Copying it gives nested decisions independent writes.
type Rows struct {
	index  Index[*Row]
	source RowSource
}

func MemoryRows(index Index[*Row]) Rows     { return Rows{index: index} }
func SourceRows(source RowSource) Rows      { return Rows{source: source} }
func (r Rows) Stored() int                  { return r.index.Len() }
func (r Rows) Sourced() bool                { return r.source != nil }
func (r Rows) Rebind(source RowSource) Rows { r.source = source; return r }
func (r Rows) Get(id string) *Row {
	if row := r.index.Get(id); row != nil || r.index.Has(id) {
		return row
	}
	if r.source != nil {
		return r.source.Get(id)
	}
	return nil
}
func (r *Rows) Set(id string, row *Row) { r.index.Set(id, row) }
func (r *Rows) Remove(id string) {
	if r.source == nil {
		r.index.Remove(id)
	} else {
		r.index.Set(id, nil)
	}
}
func (r Rows) Has(id string) bool { return r.Get(id) != nil }
func (r Rows) All() iter.Seq2[string, *Row] {
	if r.source == nil {
		return r.index.All()
	}
	return func(yield func(string, *Row) bool) {
		next, stop := iter.Pull2(r.index.All())
		defer stop()
		overlayID, overlayRow, hasOverlay := next()
		emitOverlay := func() bool {
			if overlayRow != nil && !yield(overlayID, overlayRow) {
				return false
			}
			overlayID, overlayRow, hasOverlay = next()
			return true
		}
		after := ""
		for {
			ids := r.source.IDs(after, 256)
			for _, id := range ids {
				for hasOverlay && overlayID < id {
					if !emitOverlay() {
						return
					}
				}
				if hasOverlay && overlayID == id {
					if !emitOverlay() {
						return
					}
					continue
				}
				if row := r.source.Get(id); row != nil && !yield(id, row) {
					return
				}
			}
			if len(ids) < 256 {
				break
			}
			after = ids[len(ids)-1]
		}
		for hasOverlay {
			if !emitOverlay() {
				return
			}
		}
	}
}

func (r Rows) Len() int {
	if r.source == nil {
		return r.index.Len()
	}
	n := 0
	after := ""
	for {
		ids := r.source.IDs(after, 256)
		for _, id := range ids {
			if !r.index.Has(id) {
				n++
			}
		}
		if len(ids) < 256 {
			break
		}
		after = ids[len(ids)-1]
	}
	for _, row := range r.index.All() {
		if row != nil {
			n++
		}
	}
	return n
}

// Overlay is the immutable set of replacements in this private decision.
func (r Rows) Overlay() Index[*Row] { return r.index }

// IDs enumerates the same visible IDs as All without loading record values.
// It is used by retained-model projections, which transform a row only when
// the caller actually reads it.
func (r Rows) IDs(after string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	if r.source == nil {
		var ids []string
		for id, row := range r.index.after(after) {
			if row != nil {
				ids = append(ids, id)
				if len(ids) == limit {
					break
				}
			}
		}
		return ids
	}
	ids := r.source.IDs(after, limit+r.index.Len())
	for id, row := range r.index.after(after) {
		if row != nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	out := ids[:0]
	for _, id := range ids {
		if r.index.Has(id) && r.index.Get(id) == nil {
			continue
		}
		out = append(out, id)
		if len(out) == limit {
			break
		}
	}
	return out
}
