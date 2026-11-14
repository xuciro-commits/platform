package host

import (
	"encoding/json"
	dataowner "platformserver/services/data"

	"platformserver/platform"
)

func (t *Tenant) statusReadStamp(revision uint32, applicant string, sourceRaw, connectionRaw json.RawMessage, localRecords bool) (string, bool) {
	return dataowner.StatusStamp{Member: t.Member, Publisher: t.sourcePublisher, Credential: t.CredentialValue, Markings: t.connectionMarkings, Generation: func() uint64 {
		t.Projection.Lock()
		defer t.Projection.Unlock()
		return t.Projection.Generation()
	}}.Of(revision, applicant, sourceRaw, connectionRaw, localRecords)
}

func (t *Tenant) sourcePublisher(source dataowner.Source) (platform.Member, bool) {
	return source.PublisherAs(t.Member)
}
