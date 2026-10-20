package platformserver

import (
	"fmt"
	"maps"
	"platformserver/journal"
	"slices"
	"strings"
)

func uniqueReplacements(changes map[string]map[string]string) []journal.StateUnique {
	out := make([]journal.StateUnique, 0, len(changes))
	for _, ref := range slices.Sorted(maps.Keys(changes)) {
		typ, id, _ := strings.Cut(ref, "/")
		out = append(out, journal.StateUnique{Type: typ, ID: id, Keys: changes[ref]})
	}
	return out
}

func (d *stagedDecision) savedUniqueChanges() []journal.StateUnique {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	return uniqueReplacements(d.records.uniqueChanges())
}

func validateUniqueReplacements(store *recordStore, saved []journal.StateUnique) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	expected := store.uniqueChanges()
	if len(saved) != len(expected) {
		return fmt.Errorf("record unique-key replacements differ")
	}
	seen := map[string]bool{}
	for _, row := range saved {
		ref := row.Type + "/" + row.ID
		keys, ok := expected[ref]
		if !ok || seen[ref] || !maps.Equal(keys, row.Keys) {
			return fmt.Errorf("record unique-key replacement differs for %s", ref)
		}
		seen[ref] = true
	}
	return nil
}
