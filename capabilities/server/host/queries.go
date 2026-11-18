package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	records "platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
	"time"
)

func (t *Tenant) namedQuery(app, name string) (platform.NamedQuery, bool) {
	if q := objects.DeclaredQuery(t.definitions, app, name, "", PublicationResolver()); q != nil {
		return *q, true
	}
	return platform.NamedQuery{}, false
}

func (t *Tenant) queryWindowRead(m platform.Member, app, name, of, version string, window platform.Query, now time.Time) (RecordPage, *platform.Error) {
	declared := objects.DeclaredQuery(t.Definitions(m), app, name, version, PublicationResolver())
	if declared == nil {
		return RecordPage{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return t.runDeclaredQueryWindowFrom(t.Projection, m, *declared, of, window, now)
}

func (t *Tenant) runDeclaredQueryFrom(store *records.Store, m platform.Member, q platform.NamedQuery, of string, now time.Time) (RecordPage, *platform.Error) {
	return t.runDeclaredQueryWindowFrom(store, m, q, of, platform.Query{}, now)
}

func (t *Tenant) runDeclaredQueryWindowFrom(store *records.Store, m platform.Member, q platform.NamedQuery, of string, window platform.Query, now time.Time) (RecordPage, *platform.Error) {
	set, bounds, refusal := objects.DeclaredWindow(q, of, window)
	if refusal != nil {
		return RecordPage{}, refusal
	}
	page, _, err := t.loadObjectSet(store, platform.NewCaller(runtime{t: t, scoped: true}, m, "", false, false), set, bounds, now, q)
	return page, err
}

func (h hostView) ValidateInstallQuery(q platform.NamedQuery) error {
	return h.definitionInstall().ValidateQuery(q)
}

func (h hostView) InstallQuery(c platform.Caller, q platform.NamedQuery, version int) error {
	return h.definitionInstall().InstallQuery(c, q, version)
}
