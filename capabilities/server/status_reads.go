package platformserver

import (
	"encoding/json"

	"platformserver/platform"
	"time"

	"platformserver/studio/build"
)

const statusReadLimit = 64

type statusReadSchedule map[string]statusReadAttempt

type statusReadAttempt struct {
	Due  time.Time
	Busy bool
}

func (s statusReadSchedule) retain(active map[string]bool) {
	for key, attempt := range s {
		if !active[key] && !attempt.Busy {
			delete(s, key)
		}
	}
}

func (s statusReadSchedule) next(key string, saved time.Time) time.Time {
	if cached := s[key].Due; cached.After(saved) {
		return cached
	}
	return saved
}

func (s *statusReadSchedule) start(key string, saved, at time.Time, reads *int) bool {
	if *reads >= statusReadLimit || (*s)[key].Busy || s.next(key, saved).After(at) {
		return false
	}
	if *s == nil {
		*s = statusReadSchedule{}
	}
	(*s)[key] = statusReadAttempt{Busy: true}
	*reads++
	return true
}

func (s statusReadSchedule) finished(key string, next time.Time) {
	s[key] = statusReadAttempt{Due: next}
}

func (t *Tenant) statusReadStamp(revision uint32, applicant string, sourceRaw, connectionRaw json.RawMessage, localRecords bool) (string, bool) {
	member, found := t.Member(applicant)
	if !found {
		return "", false
	}
	var connection build.Connection
	_ = json.Unmarshal(connectionRaw, &connection)
	var credential []byte
	if connection.Secret != "" {
		credential, _ = t.credentialValue(connection.Secret, "connection/"+connection.ID, connection.Address)
	}
	var source build.Source
	_ = json.Unmarshal(sourceRaw, &source)
	publisher, _ := t.sourcePublisher(source)
	marking := ""
	if connection.ID != "" {
		marking = t.connectionMarking(connection.ID)
	}
	var generation uint64
	if localRecords {
		t.records.Lock()
		generation = t.records.Generation()
		t.records.Unlock()
	}
	stamp, err := platform.CanonicalDigest([]any{revision, member, publisher, marking, credential, generation})
	return stamp, err == nil
}

func (t *Tenant) sourcePublisher(source build.Source) (platform.Member, bool) {
	member, known := t.Member(source.Publisher)
	if known && source.PublisherScopes != "" && json.Unmarshal([]byte(source.PublisherScopes), &member.Scopes) != nil {
		return platform.Member{}, false
	}
	return member, known
}
