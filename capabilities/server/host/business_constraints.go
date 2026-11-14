package host

import (
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
	return objects.LocalizedFieldIssues(issues, language, func(message string) string {
		return t.i18n.Said(&kernel.Error{Message: message}, language).Message
	})
}
