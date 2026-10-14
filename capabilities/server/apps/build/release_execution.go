package build

import (
	"encoding/json"
	"fmt"
	"reflect"

	"platformserver/internal/host"
	buildapi "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

type releaseExecution struct {
	*Build
	catalog *platform.Catalog
}

func (b *releaseExecution) Manifest() platform.Manifest {
	m := b.Build.Manifest()
	m.Actions = b.catalog
	return m
}

func (b *Build) ForkReleaseExecution(assets []platform.ReleaseAsset) (platform.App, error) {
	app, err := b.ForkDecision()
	if err != nil {
		return nil, err
	}
	bound := app.(*Build)
	bound.installed = map[string]platform.Entity{}
	bound.jointEntities = map[string]platform.EntityInfo{}
	bound.jointMode = true
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetAgent {
			agent, err := decodeAgentAsset(asset)
			if err != nil {
				return nil, err
			}
			bound.agents[agent.Name] = agent
		}
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object Object
		if asset.SourceVersion != definitionVersion || json.Unmarshal(asset.Body, &object) != nil || buildapi.TypeOf(object.Name) != asset.Ref.Name {
			return nil, fmt.Errorf("unsupported retained object descriptor %s", asset.Ref)
		}
		info, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		if problem := checkObjectSchemas(object); problem != nil {
			return nil, problem
		}
		if err != nil {
			return nil, err
		}
		bound.jointEntities[info.Type] = info
	}
	var actions []platform.Action
	for _, asset := range assets {
		if asset.Ref.App != ID {
			continue
		}
		if asset.Ref.Kind == platform.AssetObject {
			var object Object
			if err := json.Unmarshal(asset.Body, &object); err != nil {
				return nil, err
			}
			entity := bound.entity(object)
			bound.installed[entity.Type] = entity
			actions = append(actions, objectActions(object, entity)...)
		}
	}
	// The retained owner still receives on its original kernel ledger. Only
	// declaration selection changes; today's policy and grants remain in force.
	return &releaseExecution{Build: bound, catalog: b.ledger.Catalog.WithActions(actions)}, nil
}

var _ host.ReleaseExecutionOwner = (*Build)(nil)
