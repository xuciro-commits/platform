package ontology

import "platformserver/platform"

func DeclaredAction(owner platform.App, definitions []platform.Definition, schema string) (string, platform.Action, bool) {
	if owner == nil {
		return "", platform.Action{}, false
	}
	declared, ok := owner.Manifest().Actions.Action(schema)
	if !ok {
		for _, definition := range definitions {
			if definition.Ref == (platform.AssetRef{App: owner.Manifest().ID, Kind: platform.AssetAction, Name: schema}) && definition.Action != nil && definition.Action.Schema == schema {
				return owner.Manifest().ID, *definition.Action, true
			}
		}
	}
	return owner.Manifest().ID, declared, ok
}

func EntitySource(definitions []platform.Definition, typ string) (platform.AssetBinding, bool) {
	for _, definition := range definitions {
		if definition.Ref.Kind == platform.AssetObject && definition.Ref.Name == typ && definition.Version != "" {
			return platform.AssetBinding{Ref: definition.Ref, SourceVersion: definition.Version}, true
		}
	}
	return platform.AssetBinding{}, false
}

func ModelQueryBinding(definitions []platform.Definition, ref platform.AssetRef) (platform.AssetBinding, bool) {
	for _, d := range definitions {
		if d.Ref == ref && d.Query != nil {
			return platform.AssetBinding{Ref: ref, SourceVersion: d.Version}, true
		}
	}
	return platform.AssetBinding{}, false
}

func RuleQuery(owner platform.App, name string, version int, named func(string) (platform.NamedQuery, bool)) (platform.NamedQuery, int, bool) {
	if owner == nil {
		return platform.NamedQuery{}, 0, false
	}
	if source, ok := owner.(interface {
		QueryDefinition(string, int) (platform.NamedQuery, int, bool)
	}); ok {
		return source.QueryDefinition(name, version)
	}
	if version == 0 {
		q, ok := named(name)
		return q, 0, ok
	}
	return platform.NamedQuery{}, 0, false
}
