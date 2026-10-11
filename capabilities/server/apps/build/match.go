package build

import (
	"encoding/json"
	"slices"

	"time"

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

func (b *Build) matchEntity() platform.Entity {
	return platform.Entity{Type: MatchType, Title: "Matching rule", Plural: "Matching rules", Model: Match{}, Display: "title",
		Description: "When rows from different systems are one party, material or site, and whose value wins per field.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			m := record.(*Match)
			if m.State == "published" {
				return b.checkMatch(c, *m)
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
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
		if other.ID != m.ID && other.Object == m.Object && other.State == "published" {
			return refuse("{object} already has the published rule {rule}; edit that one", m.Object, other.Title)
		}
	}
	return nil
}
