package monitoring

import (
	"net/url"
	"platformserver/kernel/protocol"

	"platformserver/platform"
)

func (o ObservationReads) AuditPage(values url.Values) (protocol.AuditPage, *platform.Error) {
	query, refusal := AuditQuery(values, o.Tenant, o.Now)
	if refusal != nil {
		return protocol.AuditPage{}, refusal
	}
	page, err := o.Audit(query)
	if err != nil {
		return protocol.AuditPage{}, QueryFailure(err, query.Cursor, "invalid audit cursor", "Audit records are unavailable")
	}
	o.Remember("platform.audit.read", AuditFilterSummary(query))
	return page, nil
}

func (o ObservationReads) PersonalReadPage(values url.Values) (protocol.PersonalReadPage, *platform.Error) {
	query, refusal := PersonalReadQuery(values, o.Tenant, o.Now)
	if refusal != nil {
		return protocol.PersonalReadPage{}, refusal
	}
	page, err := o.PersonalReads(query)
	if err != nil {
		return protocol.PersonalReadPage{}, QueryFailure(err, query.Cursor, "invalid audit cursor", "Personal-read audit is unavailable")
	}
	o.Remember("platform.personal-reads.read", PersonalReadFilterSummary(query))
	return page, nil
}
