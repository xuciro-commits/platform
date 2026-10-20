package platformserver

import (
	"encoding/json"
	"platformserver/journal"
	"platformserver/platform"
)

// records.CompileSet has already checked the complete expression. Only plain
// conjunctions of physical text/reference equality are reduced here; the
// original evaluator remains the authority for every supported expression.
func sqlRecordFilters(info platform.EntityInfo, domain json.RawMessage) []journal.StateFilter {
	var terms []json.RawMessage
	if len(domain) == 0 || json.Unmarshal(domain, &terms) != nil {
		return nil
	}
	for _, term := range terms {
		if len(term) == 0 || term[0] != '[' {
			return nil
		}
	}
	var out []journal.StateFilter
	for _, raw := range terms {
		var term []json.RawMessage
		var name, op string
		if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &name) != nil || json.Unmarshal(term[1], &op) != nil {
			continue
		}
		f, ok := info.Field(name)
		if name == "id" {
			ok = true
			f = platform.FieldInfo{Name: "id", Type: "text", Index: []int{0}}
		}
		if !ok || len(f.Index) == 0 || f.AliasOf != "" || f.Property != nil {
			continue
		}
		list := f.Type == "references" || f.Type == "tags"
		if !list && f.Type != "text" && f.Type != "longtext" && f.Type != "reference" && f.Type != "choice" && f.Type != "date" {
			continue
		}
		filter := journal.StateFilter{Path: []string{"value", name}, List: list}
		switch op {
		case "=":
			var value string
			if json.Unmarshal(term[2], &value) != nil {
				continue
			}
			filter.Values = []string{value}
		case "in":
			if json.Unmarshal(term[2], &filter.Values) != nil {
				continue
			}
		default:
			continue
		}
		out = append(out, filter)
	}
	return out
}
