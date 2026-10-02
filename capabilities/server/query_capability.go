package platformserver

import (
	"platformserver/platform"
	"strconv"
)

func queryInputSchema(q platform.NamedQuery) platform.ValueSchema {
	input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	if q.By != "" {
		input.Properties["for"] = platform.ValueSchema{Type: "string", Description: "The record this query is run for"}
		input.Required = []string{"for"}
	}
	return input
}
func queryCapabilitySchemas(q platform.NamedQuery, info platform.EntityInfo) (*platform.ValueSchema, *platform.ValueSchema) {
	input := queryInputSchema(q)
	if info.Type == "" {
		return &input, nil
	}
	record, known := entityValueSchema(info)
	rows := platform.ValueSchema{Type: "array", Items: &record, MaxItems: 200}
	text := platform.ValueSchema{Type: "string"}
	result := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"records": rows, "total": {Type: "integer"}, "sources": {Type: "array", Items: &text}}, Required: []string{"records", "total", "sources"}}
	if !known || result.Check() != nil {
		return &input, nil
	}
	return &input, &result
}

// Resolve only member-filtered owner bytes. A hidden latest declaration does
// not hide a separately authorized retained version, and never substitutes it.
func (t *Tenant) memberQueryVersion(m platform.Member, ref platform.AssetRef, version int) (platform.NamedQuery, string, bool) {
	app := t.app(ref.App)
	if app == nil || ref.Kind != platform.AssetQuery || version < 0 || version > 64 {
		return platform.NamedQuery{}, "", false
	}
	for _, d := range t.Definitions(m) {
		if d.Ref != ref {
			continue
		}
		if version == 0 {
			if d.Source == "code" && d.Query != nil {
				return *d.Query, d.Version, true
			}
			break
		}
		if d.Source != "tenant" {
			break
		}
		source := app.Manifest().Version + ".query-" + strconv.Itoa(version)
		if selected := d.QueryVersion(source); selected != nil {
			return *selected.Query, source, true
		}
		break
	}
	return platform.NamedQuery{}, "", false
}
