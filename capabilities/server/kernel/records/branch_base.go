package records

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"

	"platformserver/platform"
)

func (s *Store) BranchBase(active string, draftType func(string) bool) string {
	sum := sha256.New()
	sum.Write([]byte(active))
	for _, et := range s.SortedTypes() {
		info := et.Info()
		if !draftType(info.Type) {
			continue
		}
		sum.Write([]byte("\x00" + info.Type))
		s.Lock()
		var entries []string
		for id, row := range et.Rows().All() {
			header := row.Value.FieldByName("Record").Interface().(platform.Record)
			entry := id + ":" + strconv.FormatUint(uint64(header.Revision), 10)
			if header.Archived {
				entry += ":archived"
			}
			entries = append(entries, entry)
		}
		s.Unlock()
		sort.Strings(entries)
		for _, entry := range entries {
			sum.Write([]byte("\x00" + entry))
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}
