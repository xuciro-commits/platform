package platformserver

import (
	"encoding/json"
	"platformserver/internal/records"
	"slices"
	"strings"

	"platformserver/internal/construction"
	"platformserver/platform"
)

type enterpriseReferenceDeclaration struct {
	Type   string                           `json:"type"`
	Name   string                           `json:"name"`
	Ref    string                           `json:"ref"`
	Fields []enterpriseReferenceDeclaration `json:"fields"`
	Entity *enterpriseReferenceDeclaration  `json:"entity"`
}

// enterpriseHistoricalReferencePaths reads retained owner declarations once
// per usage request. It indexes no new state and never replays the journal.
func (t *Tenant) enterpriseHistoricalReferencePaths(store *recordStore) map[string][][]string {
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
		store.mu.Lock()
		defer store.mu.Unlock()
		for _, et := range store.types {
			fields(et.info.Type, et.info.Fields, nil)
		}
		if objects := store.types[construction.ObjectType]; objects != nil {
			for _, row := range objects.rows.All() {
				typ := construction.TypeOf(businessField(objects.info, row.Value, "name"))
				if published := businessField(objects.info, row.Value, "published"); published != "" {
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
	for _, raw := range t.releases.candidates {
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
		// Ignore choice/Go storage representations; only retained name/ref and
		// child paths determine whether historical bytes name an enterprise ID.
		var declaration enterpriseReferenceDeclaration
		if json.Unmarshal(body.raw, &declaration) != nil {
			continue
		}
		typ := body.typ
		if declaration.Type != "" {
			typ = declaration.Type
		} else if declaration.Name != "" && strings.HasPrefix(typ, construction.ID+".") {
			typ = construction.TypeOf(declaration.Name)
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

// History stores field before/after JSON in the shape of that decision, not
// the current Go type. Descend declared paths through nested line arrays.
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
