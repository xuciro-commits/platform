package platformserver

import (
	"encoding/json"
	"platformserver/kernel/records"
	"slices"
	"strings"

	"platformserver/platform"
	"platformserver/studio/build"
)

type enterpriseReferenceDeclaration struct {
	Type   string                           `json:"type"`
	Name   string                           `json:"name"`
	Ref    string                           `json:"ref"`
	Fields []enterpriseReferenceDeclaration `json:"fields"`
	Entity *enterpriseReferenceDeclaration  `json:"entity"`
}

func (t *Tenant) enterpriseHistoricalReferencePaths(store *records.Store) map[string][][]string {
	paths := map[string][][]string{}
	seen := map[string]bool{}
	add := func(typ string, path []string) {
		key := typ + "/" + strings.Join(path, ".")
		if typ == "" || len(path) == 0 || seen[key] {
			return
		}
		seen[key] = true
		paths[typ] = append(paths[typ], slices.Clone(path))
	}
	var fields func(string, []platform.FieldInfo, []string)
	fields = func(typ string, items []platform.FieldInfo, prefix []string) {
		for _, field := range items {
			path := append(slices.Clone(prefix), field.Name)
			if field.Ref == "enterprise.element" {
				add(typ, path)
			}
			if len(field.Fields) > 0 {
				fields(typ, field.Fields, path)
			}
		}
	}
	type retainedBody struct {
		typ string
		raw json.RawMessage
	}
	var bodies []retainedBody
	func() {
		store.Lock()
		defer store.Unlock()
		for _, et := range store.Types() {
			fields(et.Info().Type, et.Info().Fields, nil)
		}
		if objects := store.Type(build.ObjectType); objects != nil {
			for _, row := range objects.Rows().All() {
				typ := build.TypeOf(records.BusinessField(objects.Info(), row.Value, "name"))
				if published := records.BusinessField(objects.Info(), row.Value, "published"); published != "" {
					bodies = append(bodies, retainedBody{typ, json.RawMessage(published)})
				}
				for _, change := range row.RecordHistory() {
					for _, field := range change.Fields {
						if field.Field != "published" {
							continue
						}
						for _, raw := range []json.RawMessage{field.Before, field.After} {
							var published string
							if json.Unmarshal(raw, &published) == nil && published != "" {
								bodies = append(bodies, retainedBody{typ, json.RawMessage(published)})
							}
						}
					}
				}
			}
		}
	}()
	for _, raw := range t.releases.Candidates() {
		var release struct {
			Assets []platform.ReleaseAsset `json:"assets"`
		}
		if json.Unmarshal(raw, &release) != nil {
			continue
		}
		for _, asset := range release.Assets {
			if asset.Ref.Kind == platform.AssetObject {
				bodies = append(bodies, retainedBody{asset.Ref.Name, asset.Body})
			}
		}
	}
	for _, body := range bodies {
		var declaration enterpriseReferenceDeclaration
		if json.Unmarshal(body.raw, &declaration) != nil {
			continue
		}
		typ := body.typ
		if declaration.Type != "" {
			typ = declaration.Type
		} else if declaration.Name != "" && strings.HasPrefix(typ, build.ID+".") {
			typ = build.TypeOf(declaration.Name)
		}
		if declaration.Entity != nil {
			declaration = *declaration.Entity
		}
		var walk func([]enterpriseReferenceDeclaration, []string)
		walk = func(items []enterpriseReferenceDeclaration, prefix []string) {
			for _, field := range items {
				path := append(slices.Clone(prefix), field.Name)
				if field.Ref == "enterprise.element" {
					add(typ, path)
				}
				walk(field.Fields, path)
			}
		}
		walk(declaration.Fields, nil)
	}
	return paths
}

func historyNamesEnterprisePaths(paths [][]string, changes []records.RecordChange, id string) bool {
	for _, change := range changes {
		for _, field := range change.Fields {
			for _, path := range paths {
				if len(path) == 0 || path[0] != field.Field {
					continue
				}
				for _, raw := range []json.RawMessage{field.Before, field.After} {
					if enterpriseHistoryJSONNames(raw, path[1:], id) {
						return true
					}
				}
			}
		}
	}
	return false
}

func enterpriseHistoryJSONNames(raw json.RawMessage, path []string, id string) bool {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return false
	}
	var names func(any, []string) bool
	names = func(value any, path []string) bool {
		if rows, ok := value.([]any); ok {
			return slices.ContainsFunc(rows, func(row any) bool { return names(row, path) })
		}
		if len(path) == 0 {
			reference, ok := value.(string)
			return ok && reference == id
		}
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		return names(object[path[0]], path[1:])
	}
	return names(value, path)
}
