package descriptor

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"

	"platformserver/kernel/protocol"
	"platformserver/platform"
)

var contractID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*/[1-9][0-9]{0,2}$`)

func ProvidedContracts(p platform.PackageDescriptor) ([]string, error) {
	if p.Blueprint == nil {
		return nil, nil
	}
	ids := map[string]bool{}
	for _, asset := range p.Blueprint.Assets {
		switch asset.Ref.Kind {
		case platform.AssetObject:
			var body struct {
				Protocols []protocol.ProtocolBinding `json:"protocols"`
			}
			if err := json.Unmarshal(asset.Body, &body); err != nil {
				return nil, err
			}
			for _, binding := range body.Protocols {
				key := platform.Protocol{Name: binding.Name, Version: binding.Version}.ID()
				ids[key] = true
			}
		case protocol.AssetImplementation:
			var body protocol.ProtocolImplementation
			if err := json.Unmarshal(asset.Body, &body); err != nil {
				return nil, err
			}
			ids[body.Contract] = true
		}
	}
	return slices.Sorted(maps.Keys(ids)), nil
}

func CheckContracts(p platform.PackageDescriptor) error {
	if p.Contracts == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, group := range [][]string{p.Contracts.Requires, p.Contracts.Optional} {
		for _, id := range group {
			if !contractID.MatchString(id) || seen[id] {
				return fmt.Errorf("package %s has an invalid or repeated contract %q; use name/major", p.ID, id)
			}
			seen[id] = true
		}
	}
	derived, err := ProvidedContracts(p)
	if err != nil {
		return err
	}
	if !slices.Equal(derived, p.Contracts.Provides) {
		return fmt.Errorf("package %s contract providers must match its implementations: expected %v", p.ID, derived)
	}
	return nil
}
