package records

import (
	"fmt"
	"slices"
	"testing"
)

func TestIndexSnapshotsAndBulkImages(t *testing.T) {
	var index Index[int]
	for i := range 3000 {
		key := (i * 137) % 3000
		index.Set(fmt.Sprintf("%04d", key), key+1)
	}
	saved := index
	for i := range 3000 {
		key := fmt.Sprintf("%04d", i)
		if i%3 == 0 {
			index.Remove(key)
		} else {
			index.Set(key, -i-1)
		}
		if saved.Get(key) != i+1 {
			t.Fatal("write changed a retained index snapshot")
		}
	}
	if saved.Len() != 3000 || index.Len() != 2000 {
		t.Fatal("snapshot counts changed")
	}
	var check func(*indexNode[int]) (int, int)
	check = func(n *indexNode[int]) (int, int) {
		if n == nil {
			return 0, 0
		}
		lh, ls := check(n.left)
		rh, rs := check(n.right)
		if lh-rh > 1 || rh-lh > 1 || n.height != 1+max(lh, rh) || n.size != 1+ls+rs ||
			n.left != nil && n.left.key >= n.key || n.right != nil && n.right.key <= n.key {
			t.Fatal("record index lost its bounded ordered structure")
		}
		return n.height, n.size
	}
	check(saved.root)
	check(index.root)
	entries := make([]Entry[int], 0, saved.Len())
	for key, value := range saved.All() {
		entries = append(entries, Entry[int]{Key: key, Value: value})
	}
	slices.Reverse(entries)
	bulk, err := From(entries)
	if err != nil || bulk.Len() != saved.Len() {
		t.Fatal("bulk image did not recover an unordered record set", err)
	}
	check(bulk.root)
	for key, value := range saved.All() {
		if bulk.Get(key) != value {
			t.Fatal("bulk image changed a retained record")
		}
	}
	if _, err := From([]Entry[int]{{Key: "same", Value: 1}, {Key: "same", Value: 2}}); err == nil {
		t.Fatal("bulk image silently overwrote a duplicate identity")
	}
	last, seen := "", 0
	for key, value := range index.All() {
		if key <= last || value >= 0 || saved.Get(key) != -value {
			t.Fatal("iteration lost or changed a retained record")
		}
		last, seen = key, seen+1
	}
	if seen != 2000 {
		t.Fatal("iteration count differs")
	}
	if _, err := From([]Entry[int]{{Key: "", Value: 1}}); err == nil {
		t.Fatal("bulk image silently accepted an empty identity")
	}
}
