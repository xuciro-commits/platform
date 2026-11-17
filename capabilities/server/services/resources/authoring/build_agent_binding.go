package build

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

const AgentType = "build.agent"
const SchemaAgentPublish = AgentType + ".publish"

func (b *Build) agentEnvironment() platform.AgentAuthoringEnvironment {
	return platform.AgentAuthoringEnvironment{
		App: ID, Type: AgentType, Builder: Builder,
		Authority:         func() platform.Authority { return b.ledger },
		JointEntities:     func() map[string]platform.EntityInfo { return b.jointEntities },
		JointAssets:       func() map[platform.AssetRef]bool { return b.jointAssets },
		JointDeclarations: func() map[platform.AssetRef]platform.ReleaseAsset { return b.jointDeclarations },
		QueryExists:       func(name string) bool { _, ok := b.queries[name]; return ok },
		InstalledAction:   b.installedActionOf,
		DeclaresRole:      b.declaresRole,
		DraftTools:        b.agentDraftTools,
		SourceVersion:     func() string { return b.Manifest().Version },
		WithDependencies:  b.resolver.WithDependencies,
		CheckDependencies: b.resolver.CheckAssetDependencies,
		Inventory: func(typ reflect.Type) ([]any, error) {
			return readDefinitionInventoryOf(b.host.Automation(platform.Caller{}, ID), typ)
		},
		Read: publication.ReadInto, ValidName: publication.ValidName, EqualAsset: publication.EqualAsset,
	}
}

func (b *Build) agentDraftTools(c platform.Caller) (map[string]bool, []string, error) {
	objects, err := readDefinitionInventory[ontology.Object](c)
	if err != nil {
		return nil, nil, err
	}
	queries, err := readDefinitionInventory[Query](c)
	if err != nil {
		return nil, nil, err
	}
	tools := map[string]bool{}
	var roles []string
	for _, object := range objects {
		if object.Archived {
			continue
		}
		model := Entity(object)
		for _, action := range objectActions(object, model) {
			tools[action.Schema] = true
		}
		for role := range model.Scope.Levels {
			roles = append(roles, role)
		}
	}
	for _, query := range queries {
		if !query.Archived {
			tools["query:"+query.Name] = true
		}
	}
	return tools, roles, nil
}

func (b *Build) installedActionOf(schema string) string {
	for typ := range b.installed {
		if strings.HasPrefix(schema, typ+".") {
			return typ
		}
	}
	return ""
}

func (b *Build) AgentStamp() string {
	names := b.agent.Stamps()
	for name, query := range b.queries {
		names = append(names, fmt.Sprintf("query:%s@%d", name, query.Version))
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func (b *Build) AgentVersion(name string) int { return b.agent.Version(name) }
func (b *Build) InstallAgentAsset(asset platform.ReleaseAsset) error {
	return b.agent.InstallAsset(asset)
}
