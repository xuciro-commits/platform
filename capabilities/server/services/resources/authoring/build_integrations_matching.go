package build

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const MatchType = "build.match"

func (b *Build) matchEntity() platform.Entity {
	return platform.Entity{Type: MatchType, Title: "Matching rule", Plural: "Matching rules", Model: Match{}, Display: "title",
		Description: "When rows from different systems are one party, material or site, and whose value wins per field.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			m := record.(*Match)
			if m.State == "published" {
				fixed, err := b.ExecutionMatching(*m)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				return b.checkMatch(c, fixed)
			}
			return nil
		},
		Scope:    integrationScope(),
		Standard: integrationStandard(),
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
	if err := b.checkMatch(c, *m); err != nil {
		return err
	}
	m.Publisher = c.ID
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetMatching, *m)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*m = fixed.(Match)
	}
	return nil
}

func (b *Build) checkMatch(c platform.Caller, m Match) *kernel.Error {
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
		if other.ID == m.ID || other.State != "published" {
			continue
		}
		fixed, err := b.ExecutionMatching(other)
		if err != nil {
			return refuse(err.Error())
		}
		if fixed.Object == m.Object {
			return refuse("{object} already has the published rule {rule}; edit that one", m.Object, other.Title)
		}
	}
	return nil
}

type Match struct {
	platform.Record
	Name              string       `json:"name" field:"required,search"`
	Title             string       `json:"title" field:"required,search"`
	Object            string       `json:"object" field:"required" title:"Object"`
	Keys              []MatchKey   `json:"keys" type:"json" title:"Keys"`
	Prefer            []Preference `json:"prefer,omitempty" type:"json" title:"Preferred producers"`
	State             string       `json:"state" field:"readonly"`
	Publisher         string       `json:"publisher,omitempty" field:"readonly"`
	DefinitionVersion int          `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string       `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string     `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

type MatchKey struct {
	Fields    []string `json:"fields"`
	Normalize string   `json:"normalize,omitempty"`
}

type Preference struct {
	Field    string `json:"field"`
	Producer string `json:"producer"`
}

func (k MatchKey) KeyOf(fields map[string]any) string {
	parts := make([]string, 0, len(k.Fields))
	for _, f := range k.Fields {
		v := platform.IntegrationScalar(fields[f])
		if v == "" {
			return ""
		}
		parts = append(parts, platform.IntegrationNormalize(v, k.Normalize))
	}
	return strings.Join(parts, "\x1f")
}

type Resolver struct {
	Rule     Match
	Producer string
	index    map[string]string
	fields   map[string]map[string]any
	Merged   int
}

func NewResolver(rule Match, producer string, records []map[string]any) *Resolver {
	r := &Resolver{Rule: rule, Producer: producer, index: map[string]string{}, fields: map[string]map[string]any{}}
	for _, rec := range records {
		id := platform.IntegrationScalar(rec["id"])
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
			if p.Producer != r.Producer && platform.IntegrationScalar(existing[p.Field]) != "" {
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
