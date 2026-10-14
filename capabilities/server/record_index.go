package platformserver

import (
	"fmt"
	"iter"
	"slices"
	"strings"
)

// recordIndex is the record store's immutable AVL index. Copying its root is
// a snapshot; a write replaces only the search path. It has no parent/delta
// chain to lengthen reads as decisions accumulate, and is never persisted.
// Values are immutable once published in the index.
type recordIndex[V any] struct{ root *recordNode[V] }

type recordEntry[V any] struct {
	key   string
	value V
}

// Complete images own this temporary slice. Sorting by record ID also accepts
// historical snapshots whose JSON image order differs from their ID order.
// Each node is allocated once, without retaining intermediate insertion roots.
func recordIndexFrom[V any](entries []recordEntry[V]) (recordIndex[V], error) {
	slices.SortFunc(entries, func(a, b recordEntry[V]) int { return strings.Compare(a.key, b.key) })
	for i, entry := range entries {
		if entry.key == "" || i > 0 && entries[i-1].key == entry.key {
			return recordIndex[V]{}, fmt.Errorf("record index needs unique nonempty keys")
		}
	}
	var build func(int, int) *recordNode[V]
	build = func(start, end int) *recordNode[V] {
		if start == end {
			return nil
		}
		middle := start + (end-start)/2
		entry := entries[middle]
		return recordBranch(entry.key, entry.value, build(start, middle), build(middle+1, end))
	}
	return recordIndex[V]{root: build(0, len(entries))}, nil
}

type recordNode[V any] struct {
	key          string
	value        V
	left, right  *recordNode[V]
	height, size int
}

func (s recordIndex[V]) get(key string) V {
	if n := s.find(key); n != nil {
		return n.value
	}
	var absent V
	return absent
}

func (s recordIndex[V]) has(key string) bool { return s.find(key) != nil }

func (s recordIndex[V]) find(key string) *recordNode[V] {
	for n := s.root; n != nil; {
		switch {
		case key < n.key:
			n = n.left
		case key > n.key:
			n = n.right
		default:
			return n
		}
	}
	return nil
}

func (s recordIndex[V]) len() int { return recordSize(s.root) }

func recordSize[V any](n *recordNode[V]) int {
	if n == nil {
		return 0
	}
	return n.size
}

func recordHeight[V any](n *recordNode[V]) int {
	if n == nil {
		return 0
	}
	return n.height
}

func recordBranch[V any](key string, value V, left, right *recordNode[V]) *recordNode[V] {
	return &recordNode[V]{key: key, value: value, left: left, right: right,
		height: 1 + max(recordHeight(left), recordHeight(right)), size: 1 + recordSize(left) + recordSize(right)}
}

func recordRotateLeft[V any](n *recordNode[V]) *recordNode[V] {
	r := n.right
	return recordBranch(r.key, r.value, recordBranch(n.key, n.value, n.left, r.left), r.right)
}

func recordRotateRight[V any](n *recordNode[V]) *recordNode[V] {
	l := n.left
	return recordBranch(l.key, l.value, l.left, recordBranch(n.key, n.value, l.right, n.right))
}

func recordBalance[V any](n *recordNode[V]) *recordNode[V] {
	switch delta := recordHeight(n.left) - recordHeight(n.right); {
	case delta > 1:
		if recordHeight(n.left.left) < recordHeight(n.left.right) {
			n = recordBranch(n.key, n.value, recordRotateLeft(n.left), n.right)
		}
		return recordRotateRight(n)
	case delta < -1:
		if recordHeight(n.right.right) < recordHeight(n.right.left) {
			n = recordBranch(n.key, n.value, n.left, recordRotateRight(n.right))
		}
		return recordRotateLeft(n)
	}
	return n
}

func (s *recordIndex[V]) set(key string, value V) { s.root = recordSet(s.root, key, value) }

func recordSet[V any](n *recordNode[V], key string, value V) *recordNode[V] {
	if n == nil {
		return recordBranch(key, value, nil, nil)
	}
	switch {
	case key < n.key:
		return recordBalance(recordBranch(n.key, n.value, recordSet(n.left, key, value), n.right))
	case key > n.key:
		return recordBalance(recordBranch(n.key, n.value, n.left, recordSet(n.right, key, value)))
	default:
		return recordBranch(key, value, n.left, n.right)
	}
}

func (s *recordIndex[V]) remove(key string) { s.root = recordRemove(s.root, key) }

func recordRemove[V any](n *recordNode[V], key string) *recordNode[V] {
	if n == nil {
		return nil
	}
	switch {
	case key < n.key:
		return recordBalance(recordBranch(n.key, n.value, recordRemove(n.left, key), n.right))
	case key > n.key:
		return recordBalance(recordBranch(n.key, n.value, n.left, recordRemove(n.right, key)))
	case n.left == nil:
		return n.right
	case n.right == nil:
		return n.left
	default:
		next := n.right
		for next.left != nil {
			next = next.left
		}
		return recordBalance(recordBranch(next.key, next.value, n.left, recordRemove(n.right, next.key)))
	}
}

func (s recordIndex[V]) all() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		var walk func(*recordNode[V]) bool
		walk = func(n *recordNode[V]) bool {
			return n == nil || walk(n.left) && yield(n.key, n.value) && walk(n.right)
		}
		walk(s.root)
	}
}
