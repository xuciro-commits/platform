package build

import (
	"platformserver/services/ontology"
	"reflect"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type appEnvironment struct {
	b *Build
}

func (e appEnvironment) Host() platform.BuildHost { return e.b.host }
func (e appEnvironment) AppID() string            { return ID }
func (e appEnvironment) Version() string          { return ValueDefinitionVersion }
func (e appEnvironment) Resolver() publication.Resolver {
	return e.b.resolver
}
func (e appEnvironment) Entity(typ string) (platform.EntityInfo, bool) {
	return e.b.lookupEntity(typ)
}

func (e appEnvironment) DraftObjectLookup(c platform.Caller) (func(string) (platform.EntityInfo, bool), error) {
	objects, err := readDefinitionInventory[ontology.Object](c)
	if err != nil {
		return nil, err
	}
	models := map[string]platform.EntityInfo{}
	for _, object := range objects {
		if object.Archived {
			continue
		}
		info, err := records.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		if err != nil {
			return nil, err
		}
		models[info.Type] = info
	}
	return func(typ string) (platform.EntityInfo, bool) {
		if info, saved := models[typ]; saved {
			return info, true
		}
		return e.b.conditionLookup(typ)
	}, nil
}

func (e appEnvironment) CheckName(name, id string) error {
	return e.b.checkName(name, id)
}

type ApplicationOwners struct {
	New            func(publication.ApplicationEnvironment) publication.ApplicationAuthoring
	ObjectPage     func(ontology.Object, string) platform.Page
	ListDetailPage func(string, string, string, platform.AssetRef, []string, []string, []platform.AssetRef) platform.Page
}
