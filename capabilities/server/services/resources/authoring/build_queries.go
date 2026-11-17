package build

import (
	"reflect"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

const (
	QueryType          = "build.query"
	SchemaQueryPublish = QueryType + ".publish"
)

func (b *Build) QueryDefinition(name string, version int) (platform.NamedQuery, int, bool) {
	return b.queries.Definition(name, version)
}

func (b *Build) queryEnvironment() publication.QueryAuthoringEnvironment {
	return publication.QueryAuthoringEnvironment{
		App: ID, Type: QueryType, Builder: Builder, Version: ValueDefinitionVersion,
		Host:              func() platform.BuildHost { return b.host },
		InstalledObject:   func(typ string) (platform.Entity, bool) { entity, ok := b.installed[typ]; return entity, ok },
		LookupEntity:      b.lookupEntity,
		JointEntities:     func() map[string]platform.EntityInfo { return b.jointEntities },
		JointDeclarations: func() map[platform.AssetRef]platform.ReleaseAsset { return b.jointDeclarations },
		JointMode:         func() bool { return b.jointMode },
		AssetVersion: func(asset platform.ReleaseAsset) int {
			return b.resolver.DefinitionAssetVersion(asset, ID, ValueDefinitionVersion, platform.AssetQuery, "query")
		},
		WithDependencies:  b.resolver.WithDependencies,
		CheckDependencies: b.resolver.CheckAssetDependencies,
	}
}

func (b *Build) checkRecordSets(object ontology.Object, engines ...platform.RuleEngine) error {
	if len(object.Sets) == 0 {
		return nil
	}
	info, err := records.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	return b.queries.CheckRecordSets(info, b.conditionLookup, engines...)
}
