package data

import (
	"encoding/json"

	"platformserver/platform"
)

type StatusStamp struct {
	Member     func(string) (platform.Member, bool)
	Publisher  func(Source) (platform.Member, bool)
	Credential func(name, consumer, address string) ([]byte, bool)
	Markings   func(connection string) platform.MarkingSet
	Generation func() uint64
}

func (s StatusStamp) Of(revision uint32, applicant string, sourceRaw, connectionRaw json.RawMessage, localRecords bool) (string, bool) {
	member, found := s.Member(applicant)
	if !found {
		return "", false
	}
	var connection Connection
	_ = json.Unmarshal(connectionRaw, &connection)
	var credential []byte
	if connection.Secret != "" {
		credential, _ = s.Credential(connection.Secret, "connection/"+connection.ID, connection.Address)
	}
	var source Source
	_ = json.Unmarshal(sourceRaw, &source)
	publisher, _ := s.Publisher(source)
	marking := platform.MarkingSet{}
	if connection.ID != "" {
		marking = s.Markings(connection.ID)
	}
	var generation uint64
	if localRecords {
		generation = s.Generation()
	}
	stamp, err := platform.CanonicalDigest([]any{revision, member, publisher, marking, credential, generation})
	return stamp, err == nil
}

func (s Source) PublisherAs(lookup func(string) (platform.Member, bool)) (platform.Member, bool) {
	member, known := lookup(s.Publisher)
	if known && s.PublisherScopes != "" && json.Unmarshal([]byte(s.PublisherScopes), &member.Scopes) != nil {
		return platform.Member{}, false
	}
	return member, known
}
