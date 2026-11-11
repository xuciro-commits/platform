package host

import (
	runtimekernel "platformserver/kernel"

	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformkernel/kernel"

	"platformserver/platform"
)

func (t *Tenant) readableFieldIssues(member platform.Member, issues []platform.FieldIssue, at time.Time) []platform.FieldIssue {
	issues = slices.Clone(issues)
	t.Projection.Lock()
	defer t.Projection.Unlock()
	for i, issue := range issues {
		issues[i].Values = maps.Clone(issue.Values)
		if issue.Code == "business_refusal" {
			if !t.readableIn(t.Projection, member, issue.Record, at) {
				issues[i].Values = nil
				issues[i].Translations = nil
				issues[i].Message = "The business record is no longer readable"
			}
			continue
		}
		if issue.Constraint == "" {
			continue
		}
		typ, id, _ := strings.Cut(issue.Record, "/")
		et := t.Projection.Type(typ)
		if et == nil {
			issues[i].Values = nil
			continue
		}
		info := platform.ConstraintEntity(et.Info())
		if et.Rows().Get(id) != nil && !t.readableIn(t.Projection, member, issue.Record, at) {
			issues[i].Values = nil
			continue
		}
		if current := et.Rows().Get(id); current != nil && et.Info().HasReadSources() {
			narrowed, _ := t.narrowerIn(t.Projection, member, at, true).Derive(et, current.Value)
			for _, derivation := range et.Info().Derived {
				paths := derivation.Fields
				if len(derivation.List) > 0 {
					paths = [][]int{derivation.List}
				}
				for _, indices := range paths {
					if reflect.DeepEqual(current.Value.FieldByIndex(indices).Interface(), narrowed.FieldByIndex(indices).Interface()) {
						continue
					}
					delete(issues[i].Values, fieldName(et.Info().Go, indices))
				}
			}
		}
		for name := range issues[i].Values {
			field, ok := info.Field(name)
			if !ok || !field.ReadsAny(member.RolesIn(info.App)) {
				delete(issues[i].Values, name)
			}
		}
	}
	return issues
}

func (t *Tenant) localizedFieldIssues(member platform.Member, issues []platform.FieldIssue, language string) []platform.FieldIssue {
	issues = slices.Clone(issues)
	for i := range issues {
		if translated := issues[i].Translations[language]; translated != "" {
			issues[i].Message = translated
		} else {
			issues[i].Message = t.i18n.Said(&kernel.Error{Message: issues[i].Message}, language).Message
		}
	}
	return issues
}

func (d *stagedDecision) constraints() runtimekernel.ConstraintCheck {
	k := runtimekernel.ConstraintCheck{Records: d.records, At: d.at, Writes: d.constraintWrites, Definitions: d.tenant.definitions,
		RuleQuery: hostView{t: d.tenant}.RuleQuery, Binding: d.tenant.recordSetBinding}
	if d.TraceEnabled() {
		k.Trace = d.RecordDecisionTrace
	}
	return k
}

func (d *stagedDecision) checkFinalConstraints() *kernel.Error {
	if d.Failure == nil {
		d.Failure = d.constraints().Final()
	}
	return d.Failure
}
