package build

import (
	"reflect"

	"platformserver/platform"
	dataowner "platformserver/services/data"
)

type buildDataHost struct{ b *Build }

func (b *Build) DataOwner() *dataowner.Owner { return b.data }

func (h buildDataHost) NameTaken(name, id string) (string, bool, error) {
	return h.b.taken(name, id)
}

func (h buildDataHost) CompartmentExists(id string) bool {
	return h.b.host != nil && h.b.host.CompartmentExists(id)
}

func (h buildDataHost) Entity(typ string) (platform.EntityInfo, bool) {
	return h.b.lookupEntity(typ)
}

func (h buildDataHost) DraftEntity(typ string) (platform.EntityInfo, bool) {
	if h.b.host == nil {
		return platform.EntityInfo{}, false
	}
	objects, err := readDefinitionInventory[Object](h.b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return platform.EntityInfo{}, false
	}
	for _, object := range objects {
		if object.Archived || TypeOf(object.Name) != typ {
			continue
		}
		info, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		return info, err == nil
	}
	return platform.EntityInfo{}, false
}

func (h buildDataHost) Action(c platform.Caller, schema string) (string, platform.Action, bool) {
	if h.b.host == nil {
		return "", platform.Action{}, false
	}
	return h.b.host.Action(schema)
}

func (h buildDataHost) ObjectActions(c platform.Caller, typ string) ([]platform.Action, bool) {
	if h.b.host == nil {
		return nil, false
	}
	objects, err := readDefinitionInventory[Object](h.b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, false
	}
	for _, object := range objects {
		if object.Archived || TypeOf(object.Name) != typ {
			continue
		}
		return objectActions(object, Entity(object)), true
	}
	return nil, false
}

func (h buildDataHost) RoleDeclared(role string) bool {
	return h.b.declaresRole(role)
}

func (h buildDataHost) Automation(parent platform.Caller, app string) platform.Caller {
	return h.b.host.Automation(parent, app)
}
