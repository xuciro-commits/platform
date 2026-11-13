package ontology

import (
	"slices"
	"strings"

	"platformserver/platform"
)

func Interfaces(definitions []platform.Definition, manifests []platform.Manifest) []platform.Interface {
	seen := map[platform.AssetRef]bool{}
	out := []platform.Interface{}
	for _, definition := range definitions {
		if definition.Interface == nil || seen[definition.Ref] {
			continue
		}
		seen[definition.Ref] = true
		out = append(out, *definition.Interface)
	}
	for _, manifest := range manifests {
		for _, shape := range manifest.Interfaces {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetInterface, Name: shape.Name}
			if seen[ref] {
				continue
			}
			seen[ref] = true
			out = append(out, shape)
		}
	}
	slices.SortFunc(out, func(a, b platform.Interface) int { return strings.Compare(a.Name, b.Name) })
	return out
}
