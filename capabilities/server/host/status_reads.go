package host

import (
	"encoding/json"
	dataowner "platformserver/services/data"

	"platformserver/platform"
)

func (t *Tenant) statusReadStamp(revision uint32, applicant string, sourceRaw, connectionRaw json.RawMessage, localRecords bool) (string, bool) {
	member, found := t.Member(applicant)
	if !found {
		return "", false
	}
	var connection dataowner.Connection
	_ = json.Unmarshal(connectionRaw, &connection)
	var credential []byte
	if connection.Secret != "" {
		credential, _ = t.credentialValue(connection.Secret, "connection/"+connection.ID, connection.Address)
	}
	var source dataowner.Source
	_ = json.Unmarshal(sourceRaw, &source)
	publisher, _ := t.sourcePublisher(source)
	marking := platform.MarkingSet{}
	if connection.ID != "" {
		marking = t.connectionMarkings(connection.ID)
	}
	var generation uint64
	if localRecords {
		t.Projection.Lock()
		generation = t.Projection.Generation()
		t.Projection.Unlock()
	}
	stamp, err := platform.CanonicalDigest([]any{revision, member, publisher, marking, credential, generation})
	return stamp, err == nil
}

func (t *Tenant) sourcePublisher(source dataowner.Source) (platform.Member, bool) {
	member, known := t.Member(source.Publisher)
	if known && source.PublisherScopes != "" && json.Unmarshal([]byte(source.PublisherScopes), &member.Scopes) != nil {
		return platform.Member{}, false
	}
	return member, known
}
