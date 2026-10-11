package buildapi

import (
	"encoding/json"

	"strings"

	"unicode"

	"platformserver/platform"
)

type Match struct {
	platform.Record
	Name   string `json:"name" field:"required,search"`
	Title  string `json:"title" field:"required,search"`
	Object string `json:"object" field:"required" title:"Object"`
	// Keys: a row that agrees with an existing record on any key is that record.
	Keys []MatchKey `json:"keys" type:"json" title:"Keys"`
	// Prefer: for a field, the pipeline or source (by name) whose value wins; others fill it only while empty.
	Prefer    []Preference `json:"prefer,omitempty" type:"json" title:"Preferred producers"`
	State     string       `json:"state" field:"readonly"`
	Publisher string       `json:"publisher,omitempty" field:"readonly"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// MatchKey is one way to say "the same": the fields compared, and how they are
// normalised first - exact; folded (trim, case, inner spaces); digits (only
// the digits, without leading zeros, so 000123 = 123 = M-123).
type MatchKey struct {
	Fields    []string `json:"fields"`
	Normalize string   `json:"normalize,omitempty"`
}

type Preference struct {
	Field    string `json:"field"`
	Producer string `json:"producer"`
}

// KeyOf is the normalised value of key k over the fields of a row, or "" when
// any field is missing - a half-known key never matches.
func (k MatchKey) KeyOf(fields map[string]any) string {
	parts := make([]string, 0, len(k.Fields))
	for _, f := range k.Fields {
		v := ValueScalar(fields[f])
		if v == "" {
			return ""
		}
		parts = append(parts, ValueNormalize(v, k.Normalize))
	}
	return strings.Join(parts, "\x1f")
}

func ValueNormalize(v, how string) string {
	switch how {
	case "folded":
		return strings.Join(strings.Fields(strings.ToLower(v)), " ")
	case "digits":
		var b strings.Builder
		for _, r := range v {
			if unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		return strings.TrimLeft(b.String(), "0")
	}
	return v
}

// Resolver applies one published rule to rows landing from producer: rows
// that match an existing record take its id (an edit instead of a second
// record), rows that match each other converge on the first, and fields a
// different producer owns are dropped while the record already has a value.
type Resolver struct {
	Rule     Match
	Producer string
	index    map[string]string         // key → record id
	fields   map[string]map[string]any // record id → fields
	Merged   int
}

func NewResolver(rule Match, producer string, records []map[string]any) *Resolver {
	r := &Resolver{Rule: rule, Producer: producer, index: map[string]string{}, fields: map[string]map[string]any{}}
	for _, rec := range records {
		id := ValueScalar(rec["id"])
		if id == "" {
			continue
		}
		r.fields[id] = rec
		r.Learn(id, rec)
	}
	return r
}

func (r *Resolver) Learn(id string, fields map[string]any) {
	for i, k := range r.Rule.Keys {
		if v := k.KeyOf(fields); v != "" {
			key := k.Normalize + ":" + ValueItoa(i) + ":" + v
			if _, taken := r.index[key]; !taken {
				r.index[key] = id
			}
		}
	}
}

// Resolve rewrites the row in place and tells whether it was merged into another id.
func (r *Resolver) Resolve(row *SourceRow) bool {
	if row.Error != "" || row.ID == "" {
		return false
	}
	var fields map[string]any
	if json.Unmarshal(row.Payload, &fields) != nil {
		return false
	}
	merged := false
	if _, known := r.fields[row.ID]; !known {
		for i, k := range r.Rule.Keys {
			if v := k.KeyOf(fields); v != "" {
				if id, ok := r.index[k.Normalize+":"+ValueItoa(i)+":"+v]; ok && id != row.ID {
					row.Key, row.ID, merged = row.Key+":as:"+id, id, true
					break
				}
			}
		}
	}
	if existing := r.fields[row.ID]; existing != nil {
		for _, p := range r.Rule.Prefer {
			if p.Producer != r.Producer && ValueScalar(existing[p.Field]) != "" {
				delete(fields, p.Field)
			}
		}
		for k, v := range fields {
			existing[k] = v
		}
	} else {
		r.fields[row.ID] = fields
	}
	r.Learn(row.ID, fields)
	row.Payload, _ = json.Marshal(fields)
	if merged {
		r.Merged++
	}
	return merged
}

func ValueItoa(i int) string { return string(rune('a' + i)) }
