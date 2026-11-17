package ontology

import (
	"strings"

	"platformserver/platform"
)

func ModelObject(typ string) platform.AssetRef {
	app, _, _ := strings.Cut(typ, ".")
	return platform.AssetRef{App: app, Kind: platform.AssetObject, Name: typ}
}
