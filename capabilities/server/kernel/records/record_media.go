package records

import (
	"platformserver/platform"
	"slices"
)

func (s *Store) HasMediaItem(id, hash string) bool {
	entity := s.types[platform.MediaSetType]
	if entity == nil {
		return false
	}
	row := entity.rows.Get(id)
	if row == nil {
		return false
	}
	set, ok := row.Value.Interface().(platform.MediaSet)
	if !ok {
		return false
	}
	if slices.ContainsFunc(set.Items, func(item platform.MediaItem) bool { return item.Hash == hash }) {
		return true
	}

	versions := s.types[platform.MediaSetVersionType]
	if versions == nil {
		return false
	}
	for _, row := range versions.rows.All() {
		version := row.Value.Interface().(platform.MediaSetVersion)
		if version.MediaSet == id && slices.ContainsFunc(version.Items, func(item platform.MediaItem) bool { return item.Hash == hash }) {
			return true
		}
	}
	return false
}
