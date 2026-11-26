package store

import "fmt"

// FrozenEpoch is the format epoch frozen before the first customer tenant was loaded.
// Entries older than it are never read; entries from FrozenEpoch up to FormatEpoch are
// read only through the registered migration of each step.
const FrozenEpoch = 1

// Migration rewrites one accepted entry from epoch From to From+1. Each one lives in
// migration_<from>_<to>.go with a test in migration_<from>_<to>_test.go; make check refuses
// a FormatEpoch change without both.
type Migration struct {
	From  int
	Name  string
	Apply func(Entry) (Entry, error)
}

var Migrations = map[int]Migration{}

func Upgrade(e Entry) (Entry, error) {
	return upgrade(e, FrozenEpoch, FormatEpoch, Migrations)
}

func upgrade(e Entry, frozen, target int, migrations map[int]Migration) (Entry, error) {
	if e.Epoch < frozen || e.Epoch > target {
		return e, fmt.Errorf("journal is format epoch %d and this host reads epoch %d", e.Epoch, target)
	}
	for e.Epoch < target {
		m, ok := migrations[e.Epoch]
		if !ok || m.Apply == nil {
			return e, fmt.Errorf("journal is format epoch %d and this host has no migration to epoch %d", e.Epoch, e.Epoch+1)
		}
		next, err := m.Apply(e)
		if err != nil {
			return e, fmt.Errorf("migration %s from format epoch %d: %w", m.Name, e.Epoch, err)
		}
		next.Epoch = e.Epoch + 1
		e = next
	}
	return e, nil
}
