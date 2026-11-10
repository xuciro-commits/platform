package host

import (
	"encoding/json"
	dataowner "platformserver/services/data"
	"sync"

	"platformserver/platform"
	"time"
)

const statusReadLimit = 64

type statusReadSchedule struct {
	mu       sync.Mutex
	attempts map[string]statusReadAttempt
}

type statusReadAttempt struct {
	Due  time.Time
	Busy bool
}

func (s *statusReadSchedule) retain(active map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, attempt := range s.attempts {
		if !active[key] && !attempt.Busy {
			delete(s.attempts, key)
		}
	}
}

func (s *statusReadSchedule) next(key string, saved time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextLocked(key, saved)
}

func (s *statusReadSchedule) nextLocked(key string, saved time.Time) time.Time {
	if cached := s.attempts[key].Due; cached.After(saved) {
		return cached
	}
	return saved
}

func (s *statusReadSchedule) start(key string, saved, at time.Time, reads *int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if *reads >= statusReadLimit || s.attempts[key].Busy || s.nextLocked(key, saved).After(at) {
		return false
	}
	if s.attempts == nil {
		s.attempts = map[string]statusReadAttempt{}
	}
	s.attempts[key] = statusReadAttempt{Busy: true}
	*reads++
	return true
}

func (s *statusReadSchedule) finished(key string, next time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[key] = statusReadAttempt{Due: next}
}

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
		t.records.Lock()
		generation = t.records.Generation()
		t.records.Unlock()
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
