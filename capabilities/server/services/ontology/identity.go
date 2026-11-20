package ontology

import (
	"platformserver/platform"
)

func ModelObject(typ string) platform.AssetRef {
	return platform.AssetRef{App: platform.OwnerOfType(typ), Kind: platform.AssetObject, Name: typ}
}
