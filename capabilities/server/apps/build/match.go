package build

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Matching rules (ADR-0074, ADR-0069 Ⅰ-G). Several systems describe the same
// party, material or site under different ids; a matching rule says when two
// rows are one entity, and whose value wins per field. It is applied wherever
// rows land in an object - a source's pull, a pipeline's run - so the object
// converges to one record per real thing without anyone writing code.
const MatchType = "build.match"

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

func (b *Build) matchEntity() platform.Entity {
	return platform.Entity{Type: MatchType, Title: "Matching rule", Plural: "Matching rules", Model: Match{}, Display: "title",
		Description: "When rows from different systems are one party, material or site, and whose value wins per field.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the rule and apply it to every row landing in the object from now on.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishMatch},
				{Name: "pause", Title: "Pause", Description: "Stop matching; records stay as they are.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func (b *Build) publishMatch(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	m, ok := record.(*Match)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(m.Name, m.ID); err != nil {
		return refuse(err.Error())
	}
	info, ok := b.lookupEntity(m.Object)
	if !ok {
		return refuse("The object {object} is not installed", m.Object)
	}
	if len(m.Keys) == 0 && len(m.Prefer) == 0 {
		return refuse("A matching rule has at least one key or one preferred producer")
	}
	for _, k := range m.Keys {
		if len(k.Fields) == 0 {
			return refuse("Every key names at least one field")
		}
		if !slices.Contains([]string{"", "exact", "folded", "digits"}, k.Normalize) {
			return refuse("A key is compared exact, folded or by digits")
		}
		for _, f := range k.Fields {
			if _, ok := info.Field(f); !ok {
				return refuse("{field} is not a field of {object}", f, m.Object)
			}
		}
	}
	for _, p := range m.Prefer {
		if p.Producer == "" {
			return refuse("A preference names the producer whose value wins")
		}
		if f, ok := info.Field(p.Field); !ok || f.ReadOnly {
			return refuse("{field} is not a writable field of {object}", p.Field, m.Object)
		}
	}
	others, _, _ := platform.Find[Match](c, platform.Query{Limit: 500})
	for _, other := range others {
		if other.ID != m.ID && other.Object == m.Object && other.State == "published" {
			return refuse("{object} already has the published rule {rule}; edit that one", m.Object, other.Title)
		}
	}
	m.Publisher = c.ID
	return nil
}

// KeyOf is the normalised value of key k over the fields of a row, or "" when
// any field is missing - a half-known key never matches.
func (k MatchKey) KeyOf(fields map[string]any) string {
	parts := make([]string, 0, len(k.Fields))
	for _, f := range k.Fields {
		v := scalar(fields[f])
		if v == "" {
			return ""
		}
		parts = append(parts, normalize(v, k.Normalize))
	}
	return strings.Join(parts, "\x1f")
}

func normalize(v, how string) string {
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
		id := scalar(rec["id"])
		if id == "" {
			continue
		}
		r.fields[id] = rec
		r.learn(id, rec)
	}
	return r
}

func (r *Resolver) learn(id string, fields map[string]any) {
	for i, k := range r.Rule.Keys {
		if v := k.KeyOf(fields); v != "" {
			key := k.Normalize + ":" + itoa(i) + ":" + v
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
				if id, ok := r.index[k.Normalize+":"+itoa(i)+":"+v]; ok && id != row.ID {
					row.Key, row.ID, merged = row.Key+":as:"+id, id, true
					break
				}
			}
		}
	}
	if existing := r.fields[row.ID]; existing != nil {
		for _, p := range r.Rule.Prefer {
			if p.Producer != r.Producer && scalar(existing[p.Field]) != "" {
				delete(fields, p.Field)
			}
		}
		for k, v := range fields {
			existing[k] = v
		}
	} else {
		r.fields[row.ID] = fields
	}
	r.learn(row.ID, fields)
	row.Payload, _ = json.Marshal(fields)
	if merged {
		r.Merged++
	}
	return merged
}

func itoa(i int) string { return string(rune('a' + i)) }
