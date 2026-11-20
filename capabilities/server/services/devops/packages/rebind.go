package packages

import (
	"bytes"
	"encoding/json"
	"strings"

	"platformserver/platform"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

func Space(p platform.PackageDescriptor) string {
	space := ""
	if p.Blueprint == nil {
		return ""
	}
	for _, asset := range p.Blueprint.Assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		prefix, _, typed := strings.Cut(asset.Ref.Name, ".")
		if !typed || prefix == "build" || space != "" && prefix != space {
			return ""
		}
		space = prefix
	}
	return space
}

func Rebind(p platform.PackageDescriptor, namespace string) (platform.PackageDescriptor, error) {
	from := Space(p)
	if from == namespace || from == "" {
		return p, nil
	}
	owned := map[string]bool{}
	for _, asset := range p.Blueprint.Assets {
		if _, name, typed := strings.Cut(asset.Ref.Name, "."); typed && asset.Ref.Kind == platform.AssetObject {
			owned[name] = true
		}
	}
	r := rebinding{from: from, to: namespace, owned: owned}
	raw, err := json.Marshal(p.Blueprint)
	if err != nil {
		return p, err
	}
	tree, err := decodeTree(raw)
	if err != nil {
		return p, err
	}
	if raw, err = json.Marshal(r.value("", tree)); err != nil {
		return p, err
	}
	var blueprint platform.PackageBlueprint
	if err := json.Unmarshal(raw, &blueprint); err != nil {
		return p, err
	}
	var versions []string
	for _, asset := range blueprint.Assets {
		var object ontology.Object
		if asset.Ref.Kind != platform.AssetObject || json.Unmarshal(asset.Body, &object) != nil || len(object.SchemaVersions) == 0 {
			continue
		}
		body, err := ontology.ObjectSchemaBody(object)
		if err != nil {
			return p, err
		}
		version, err := publication.SchemaContentVersion(body)
		if err != nil {
			return p, err
		}
		versions = append(versions, object.SchemaVersions[len(object.SchemaVersions)-1].Version, version)
	}
	raw = []byte(strings.NewReplacer(versions...).Replace(string(raw)))
	blueprint = platform.PackageBlueprint{}
	if err := json.Unmarshal(raw, &blueprint); err != nil {
		return p, err
	}
	p.Blueprint = &blueprint
	return p, nil
}

type rebinding struct {
	from, to string
	owned    map[string]bool
}

func decodeTree(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var tree any
	return tree, decoder.Decode(&tree)
}

func (r rebinding) value(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = r.value(k, e)
		}
	case []any:
		for i, e := range x {
			x[i] = r.value(key, e)
		}
	case string:
		if x == r.from && (key == "namespace" || key == "name" || key == "app") {
			return r.to
		}
		if trimmed := strings.TrimSpace(x); strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			if tree, err := decodeTree([]byte(x)); err == nil {
				if raw, err := json.Marshal(r.value(key, tree)); err == nil {
					return string(raw)
				}
			}
		}
		return r.text(x)
	}
	return v
}

func (r rebinding) text(s string) string {
	prefix := r.from + "."
	var out strings.Builder
	for i := 0; i < len(s); {
		at := strings.Index(s[i:], prefix)
		if at < 0 {
			out.WriteString(s[i:])
			break
		}
		at += i
		end := at + len(prefix)
		for end < len(s) && (s[end] >= 'a' && s[end] <= 'z' || s[end] >= '0' && s[end] <= '9') {
			end++
		}
		out.WriteString(s[i:at])
		if (at == 0 || !word(s[at-1])) && r.owned[s[at+len(prefix):end]] {
			out.WriteString(r.to + ".")
		} else {
			out.WriteString(prefix)
		}
		out.WriteString(s[at+len(prefix) : end])
		i = end
	}
	return out.String()
}

func word(b byte) bool {
	return b == '.' || b == '_' || b == '-' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
