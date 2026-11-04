package build

import (
	"reflect"

	"platformserver/platform"
	appsauthoring "platformserver/services/apps/authoring"
)

type appEnvironment struct {
	b *Build
}

func (e appEnvironment) Host() platform.BuildHost { return e.b.host }
func (e appEnvironment) AppID() string            { return ID }
func (e appEnvironment) Version() string          { return ValueDefinitionVersion }
func (e appEnvironment) Entity(typ string) (platform.EntityInfo, bool) {
	return e.b.lookupEntity(typ)
}

func (e appEnvironment) DraftObjectLookup(c platform.Caller) (func(string) (platform.EntityInfo, bool), error) {
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, err
	}
	models := map[string]platform.EntityInfo{}
	for _, object := range objects {
		if object.Archived {
			continue
		}
		info, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
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

var _ appsauthoring.Environment = appEnvironment{}
