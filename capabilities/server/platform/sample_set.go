package platform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

type SampleRecords struct {
	Type    string            `json:"type"`
	Records []json.RawMessage `json:"records"`
}

// SampleSet is synthetic, versioned application input. It contains no request
// to copy live rows and has no execution or authorization privileges.
type SampleSet struct {
	Name    string          `json:"name"`
	Title   string          `json:"title"`
	Samples []SampleRecords `json:"samples"`
}

var sampleSetName = regexp.MustCompile(`^[a-z][a-z0-9]{0,63}$`)

func (s SampleSet) Check() error {
	if !sampleSetName.MatchString(s.Name) || s.Title == "" || len(s.Title) > 256 || len(s.Samples) > 8 {
		return fmt.Errorf("Sample set needs a name, title and at most eight model types")
	}
	seen := map[string]bool{}
	size := 0
	for _, group := range s.Samples {
		if ModelObject(group.Type).Check() != nil || seen[group.Type] || len(group.Records) > 100 {
			return fmt.Errorf("Sample model types must be unique, with at most 100 records each")
		}
		seen[group.Type] = true
		ids := map[string]bool{}
		for _, raw := range group.Records {
			size += len(raw)
			var row struct {
				ID string `json:"id"`
			}
			if size > 256<<10 || len(raw) > 32<<10 || invalidSampleRecord(raw) || json.Unmarshal(raw, &row) != nil || row.ID == "" || len(row.ID) > 128 || ids[row.ID] {
				return fmt.Errorf("Sample records need unique bounded IDs and at most 256 KiB in total")
			}
			ids[row.ID] = true
		}
	}
	return nil
}
func (s SampleSet) Dependencies() []AssetRef {
	refs := []AssetRef{}
	for _, group := range s.Samples {
		refs = append(refs, ModelObject(group.Type))
	}
	slices.SortFunc(refs, compareRef)
	return refs
}

func invalidSampleRecord(raw json.RawMessage) bool {
	_, err := DecodeValue(raw, 32<<10)
	return err != nil
}
