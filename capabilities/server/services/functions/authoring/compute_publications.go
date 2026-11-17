package authoring

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/services/resources/publication"
)

func DecodeCodePublication(image []byte) (Code, error) {
	var code Code
	if json.Unmarshal(image, &code) != nil || !slices.Contains([]string{"draft", "published"}, code.State) || code.Version < 1 || code.Version > 64 || len(code.Versions) != code.Version || !slices.Contains(code.Versions, code.Published) {
		return code, fmt.Errorf("invalid compute publication family")
	}
	var selected Code
	for i, raw := range code.Versions {
		prior, ok := publication.Read[Code](raw)
		if !ok || prior.ID != code.ID || prior.Name != code.Name || prior.Version != i+1 || prior.Definition().Check() != nil || prior.SourceHash != prior.SourceDigest() || prior.Published != "" || len(prior.Versions) > 0 {
			return code, fmt.Errorf("invalid retained compute version")
		}
		if raw == code.Published {
			selected = prior
		}
	}
	selected.Versions = code.Versions
	return selected, nil
}
func DecodeTablePublication(image []byte) (Table, error) {
	var record Table
	if err := json.Unmarshal(image, &record); err != nil {
		return record, err
	}
	frozen, ok := publication.Read[Table](record.Published)
	if !ok || record.State != "published" || frozen.ID != record.ID || frozen.Name != record.Name || frozen.Version < 1 || frozen.Version > record.Version || record.Version > 64 {
		return frozen, fmt.Errorf("accepted table has no matching immutable publication")
	}
	return frozen, nil
}
