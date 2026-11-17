package build

import (
	"fmt"
	"reflect"

	"platformserver/kernel/records"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/ontology"
)

type buildDataHost struct{ b *Build }

func (b *Build) DataOwner() *dataowner.Owner { return b.data }

func (b *Build) integrationRuntime() dataowner.IntegrationRuntime {
	return dataowner.IntegrationRuntime{
		App: b, Ledger: func() platform.Authority { return b.ledger },
		JointDeclarations: func() map[platform.AssetRef]platform.ReleaseAsset { return b.jointDeclarations },
		Lookup:            b.conditionLookup,
		Record: func(ref string) (any, bool) {
			if b.host == nil {
				return nil, false
			}
			return b.host.Record(ref)
		},
		FreezeProtocol: func(p platform.ConfirmationParty) (*platform.ConfirmationProtocol, error) {
			if b.host == nil {
				return nil, fmt.Errorf("confirmation protocol host is unavailable")
			}
			return b.host.FreezeConfirmationProtocol(p)
		},
		Holding: func(app, role string) []string {
			if b.host == nil {
				return nil
			}
			return b.host.Holding(app, role)
		},
	}
}

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
	objects, err := readDefinitionInventory[ontology.Object](h.b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return platform.EntityInfo{}, false
	}
	for _, object := range objects {
		if object.Archived || TypeOf(object.Name) != typ {
			continue
		}
		info, err := records.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
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
	objects, err := readDefinitionInventory[ontology.Object](h.b.host.Automation(platform.Caller{}, ID))
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
