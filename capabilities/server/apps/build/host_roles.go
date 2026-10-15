package build

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/host"
	buildapi "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

func (b *Build) NewSimulationApp(tenant string) platform.App { return New(tenant) }
func (b *Build) CandidateActions(asset platform.ReleaseAsset) ([]platform.Action, error) {
	code, err := b.retainedCodeObject(asset)
	if err != nil {
		return nil, err
	}
	if code {
		var actions []platform.Action
		for _, action := range b.Manifest().Actions.All() {
			if action.Target == asset.Ref.Name {
				actions = append(actions, action)
			}
		}
		return actions, nil
	}
	var object Object
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject || json.Unmarshal(asset.Body, &object) != nil || buildapi.TypeOf(object.Name) != asset.Ref.Name {
		return nil, fmt.Errorf("invalid construction object descriptor")
	}
	return objectActions(object, Entity(object)), nil
}

var _ host.ConstructionDrafts = (*Build)(nil)
var _ host.IntegrationDefinitions = (*Build)(nil)
var _ host.ConstructionSandbox = (*Build)(nil)
