package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"platformkernel/kernel"
)

// Versioning describes business data, independently of record revisions and
// definition releases. A version is an ordinary retained record.
type Versioning struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	ValidFrom  string `json:"validFrom"`
	ValidUntil string `json:"validUntil,omitempty"`
	Pin        string `json:"pin"`
}

// VersionPinBinding names the consumer's reference and the field receiving the
// selected record's version. Selection remains a business decision.
type VersionPinBinding struct {
	Reference string `json:"reference"`
	Version   string `json:"version"`
}

type RecordVersionPin struct {
	Reference    string          `json:"reference"`
	VersionField string          `json:"versionField"`
	Source       string          `json:"source"`
	Version      json.RawMessage `json:"version"`
	At           time.Time       `json:"at"`
}

// BusinessTime selects effective data; nil means the caller's current clock.
// All deliberately disables effective-time filtering, without widening access.
type BusinessTime struct {
	At  string `json:"at,omitempty"`
	All bool   `json:"all,omitempty"`
}

func withinBusinessPeriod(startComparedToPoint, pointComparedToEnd int, bounded bool) bool {
	return startComparedToPoint <= 0 && (!bounded || pointComparedToEnd < 0)
}

func ActiveOn(from, until, day string) bool {
	return withinBusinessPeriod(strings.Compare(from, day), strings.Compare(day, until), until != "")
}

func EffectiveAt(kind, from, until, at string) (bool, error) {
	layout := time.DateOnly
	if kind == "datetime" {
		layout = time.RFC3339Nano
	}
	start, err := time.Parse(layout, from)
	if err != nil {
		return false, fmt.Errorf("business valid-from must be a %s", kind)
	}
	point, err := time.Parse(layout, at)
	if err != nil {
		return false, fmt.Errorf("business time must be a %s", kind)
	}
	var end time.Time
	if until != "" {
		end, err = time.Parse(layout, until)
		if err != nil || !end.After(start) {
			return false, fmt.Errorf("business valid-until must be after valid-from")
		}
	}
	return withinBusinessPeriod(start.Compare(point), point.Compare(end), until != ""), nil
}

func CheckVersioning(info EntityInfo, lookup func(string) (EntityInfo, bool)) error {
	if v := info.Versioning; v != nil {
		key, k := info.Field(v.Key)
		version, n := info.Field(v.Version)
		from, f := info.Field(v.ValidFrom)
		if !k || !n || !f || !key.Required || !version.Required || !from.Required || !slices.Contains([]string{"text", "choice"}, key.Type) || !slices.Contains([]string{"text", "integer"}, version.Type) || !slices.Contains([]string{"date", "datetime"}, from.Type) || v.Key == v.Version || v.Key == v.ValidFrom || v.Version == v.ValidFrom {
			return fmt.Errorf("business versioning needs distinct required key, integer/text version and date/datetime valid-from fields")
		}
		if v.ValidUntil != "" {
			until, ok := info.Field(v.ValidUntil)
			if !ok || until.Type != from.Type || slices.Contains([]string{v.Key, v.Version, v.ValidFrom}, v.ValidUntil) {
				return fmt.Errorf("business valid-until must be a distinct field with the valid-from type")
			}
		}
		if v.Pin != "none" && v.Pin != "on_create" && (!strings.HasPrefix(v.Pin, "on_state:") || strings.TrimPrefix(v.Pin, "on_state:") == "") {
			return fmt.Errorf("business version pin policy must be none, on_create or on_state:<state>")
		}
	}
	seen := map[string]bool{}
	for _, pin := range info.VersionPins {
		reference, ok := info.Field(pin.Reference)
		version, exists := info.Field(pin.Version)
		if !ok || reference.Type != "reference" || reference.Ref == "" || !exists || !version.ReadOnly || pin.Reference == pin.Version || seen[pin.Reference] || seen[pin.Version] {
			return fmt.Errorf("business version pin needs a distinct reference and read-only version field")
		}
		seen[pin.Reference], seen[pin.Version] = true, true
		source, ok := lookup(reference.Ref)
		if !ok || source.App != info.App || source.Versioning == nil || source.Versioning.Pin == "none" {
			return fmt.Errorf("business version pin source must declare a same-authority pin policy")
		}
		selected, ok := source.Field(source.Versioning.Version)
		if !ok || selected.Type != version.Type {
			return fmt.Errorf("business version pin field type differs from its source")
		}
		if policy := source.Versioning.Pin; strings.HasPrefix(policy, "on_state:") && (info.Lifecycle == nil || !slices.ContainsFunc(info.Lifecycle.States, func(s State) bool { return s.Name == strings.TrimPrefix(policy, "on_state:") })) {
			return fmt.Errorf("business version pin state is not declared by the consumer")
		}
	}
	return nil
}

func VersioningConstraints(v *Versioning) []Constraint {
	if v == nil {
		return nil
	}
	return []Constraint{
		{Name: "business-version-unique", Kind: "unique", Fields: []string{v.Key, v.Version}, Scope: "tenant", Message: "The business key and version must be unique", Translations: map[string]string{"zh-CN": "业务标识和版本必须唯一"}},
	}
}

type BusinessVersionRuntime interface {
	PrepareBusinessVersions(Caller, any, time.Time) *kernel.Error
}

func (c Caller) PrepareBusinessVersions(record any, at time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	if r, ok := c.rt.(BusinessVersionRuntime); ok {
		return r.PrepareBusinessVersions(c, record, at)
	}
	return nil
}
