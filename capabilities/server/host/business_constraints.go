package host

import (
	"slices"
	"time"

	"platformkernel/kernel"

	"platformserver/platform"
	"platformserver/services/objects"
)

func (t *Tenant) readableFieldIssues(member platform.Member, issues []platform.FieldIssue, at time.Time) []platform.FieldIssue {
	return objects.ReadableFieldIssues(t.Projection, member, issues,
		func(ref string) bool { return t.readableIn(t.Projection, member, ref, at) },
		func() *objects.Narrower { return t.narrowerIn(t.Projection, member, at, true) })
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
