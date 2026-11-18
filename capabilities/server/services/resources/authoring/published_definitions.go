package build

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
)

var publishedKinds = map[string]platform.AssetKind{
	PropertyTypeType: platform.AssetPropertyType, ValueTypeType: platform.AssetValueType, ObjectTypeGroupType: platform.AssetObjectTypeGroup,
	LinkTypeType: platform.AssetLinkType, QueryType: platform.AssetQuery, InterfaceType: platform.AssetInterface, ObjectType: platform.AssetObject,
	protocol.PageType: platform.AssetPage, protocol.AppType: platform.AssetApp, ProcessType: platform.AssetFlow, functionauthoring.FunctionType: platform.AssetFunction,
	functionauthoring.CodeType: platform.AssetCompute, functionauthoring.TableType: platform.AssetCompute,
}

func PublishedDefinitionsMissing(saved map[string][]records.StoredState, installed func(platform.AssetRef) bool, flow func(name string, version int) bool) bool {
	for typ, kind := range publishedKinds {
		for _, row := range saved[typ] {
			var meta struct {
				Published string `json:"published"`
			}
			if json.Unmarshal(row.Value, &meta) != nil || meta.Published == "" {
				continue
			}
			var published struct {
				Name    string `json:"name"`
				Version int    `json:"version"`
			}
			if json.Unmarshal([]byte(meta.Published), &published) != nil {
				continue
			}
			if kind == platform.AssetFlow {
				if !flow(published.Name, published.Version) {
					return true
				}
				continue
			}
			name := published.Name
			if kind == platform.AssetObject {
				name = TypeOf(name)
			}
			if !installed(platform.AssetRef{App: ID, Kind: kind, Name: name}) {
				return true
			}
		}
	}
	return false
}
