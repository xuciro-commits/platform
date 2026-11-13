package monitoring

import (
	"net/url"

	"platformkernel/kernel"
	"platformserver/platform"
)

func (o ObservationReads) AuditPage(values url.Values) (platform.AuditPage, *kernel.Error) {
	query, refusal := AuditQuery(values, o.Tenant, o.Now)
	if refusal != nil {
		return platform.AuditPage{}, refusal
	}
	page, err := o.Audit(query)
	if err != nil {
		return platform.AuditPage{}, QueryFailure(err, query.Cursor, "invalid audit cursor", "Audit records are unavailable")
	}
	o.Remember("platform.audit.read", AuditFilterSummary(query))
	return page, nil
}

func (o ObservationReads) PersonalReadPage(values url.Values) (platform.PersonalReadPage, *kernel.Error) {
	query, refusal := PersonalReadQuery(values, o.Tenant, o.Now)
	if refusal != nil {
		return platform.PersonalReadPage{}, refusal
	}
	page, err := o.PersonalReads(query)
	if err != nil {
		return platform.PersonalReadPage{}, QueryFailure(err, query.Cursor, "invalid audit cursor", "Personal-read audit is unavailable")
	}
	o.Remember("platform.personal-reads.read", PersonalReadFilterSummary(query))
	return page, nil
}
