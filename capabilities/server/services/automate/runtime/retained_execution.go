package automation

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
)

type RetainedSource func() (shape string, value json.RawMessage, ok bool)

func RetainedFields(current, info platform.EntityInfo, raw []byte, source RetainedSource) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, fmt.Errorf("retained record cannot be decoded")
	}
	var prior map[string]json.RawMessage
	for _, old := range info.Fields {
		next, found := current.Field(old.Name)
		_, _, hasSource := source()
		if found && next.AliasOf != "" {
			fields[old.Name] = fields[next.AliasOf]
		} else if !found || next.Type != old.Type || next.Ref != old.Ref || next.ReadOnly && !old.ReadOnly && hasSource {
			if prior == nil {
				shape, value, ok := source()
				digest, _ := platform.CanonicalDigest(info.Fields)
				if !ok || shape != digest || json.Unmarshal(value, &prior) != nil {
					return nil, fmt.Errorf("retained field %s.%s has no saved source image", info.Type, old.Name)
				}
			}
			fields[old.Name] = prior[old.Name]
		}
	}
	return json.Marshal(fields)
}

func RetainedWrite(shape, old, current platform.EntityInfo, before, input, stored map[string]json.RawMessage, hasStored bool) (map[string]json.RawMessage, []string, error) {
	before, input = maps.Clone(before), maps.Clone(input)
	for _, target := range current.Fields {
		if string(before[target.Name]) == string(input[target.Name]) || target.AliasOf != "" {
			continue
		}
		field, found := shape.Field(target.Name)
		if !found || field.Type != target.Type || field.Ref != target.Ref {
			return nil, nil, fmt.Errorf("retained write %s lacks its declared field conversion", target.Name)
		}
	}
	result := stored
	if !hasStored {
		result = maps.Clone(input)
	}
	fields := []string{}
	for _, target := range current.Fields {
		if string(input[target.Name]) == string(before[target.Name]) {
			continue
		}
		name := target.Name
		if target.AliasOf != "" {
			target, _ = current.Field(target.AliasOf)
		}
		lifecycle := current.Lifecycle != nil && current.Lifecycle.Field == target.Name
		original, declared := old.Field(name)
		if target.ReadOnly && !lifecycle && (!declared || !original.ReadOnly) {
			return nil, nil, fmt.Errorf("retained action writes a retired field %s", target.Name)
		}
		result[target.Name] = input[name]
		fields = append(fields, target.Name)
	}
	return result, fields, nil
}

func ReadableFields(raw []byte, info *platform.EntityInfo, c platform.Caller) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if info != nil {
		for name := range fields {
			field, declared := info.Field(name)
			if declared && field.AliasOf != "" {
				field, declared = info.Field(field.AliasOf)
			}
			if declared && !field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
				delete(fields, name)
			}
		}
	}
	return json.Marshal(fields)
}

func RetainedCompatible(selected, current []platform.ReleaseAsset, native func(platform.ReleaseAsset) (platform.ReleaseAsset, bool)) error {
	for _, asset := range selected {
		a := slices.IndexFunc(current, func(x platform.ReleaseAsset) bool { return x.Ref == asset.Ref })
		before, _ := platform.CanonicalDigest(asset)
		var after string
		if a >= 0 {
			chosen := current[a]
			if asset.Ref.Kind == platform.AssetFlow {
				if candidate, ok := native(asset); ok {
					chosen = candidate
				}
			}
			after, _ = platform.CanonicalDigest(chosen)
		}
		if before != after {
			return fmt.Errorf("retained dependency %s has no compatible execution owner", asset.Ref)
		}
	}
	return nil
}

func RetainedDefinitions(definitions []platform.Definition, assets []platform.ReleaseAsset) ([]platform.Definition, error) {
	definitions = slices.Clone(definitions)
	for i, definition := range definitions {
		for _, asset := range assets {
			if asset.Ref != definition.Ref {
				continue
			}
			switch asset.Ref.Kind {
			case platform.AssetQuery:
				var query platform.NamedQuery
				if err := json.Unmarshal(asset.Body, &query); err != nil {
					return nil, err
				}
				definition.Query = &query
			case platform.AssetFunction:
				var function platform.AIFunction
				if err := json.Unmarshal(asset.Body, &function); err != nil {
					return nil, err
				}
				definition.Function = &function
			}
			definitions[i] = definition
		}
	}
	return definitions, nil
}
