package actions

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (l *authority) Snapshot() (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return platform.Protos(l.changes.Records(l.tenant))
}

func (l *authority) Restore(raw json.RawMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	records, err := platform.Unprotos[*pb.ChangeRecord](raw)
	if err == nil {
		l.changes.Restore(l.tenant, records)
	}
	return err
}

type withOwn struct {
	Changes json.RawMessage `json:"changes"`
	Own     json.RawMessage `json:"own,omitempty"`
}

func (l *authority) SnapshotWith(own any) (json.RawMessage, error) {
	changes, err := l.Snapshot()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(own)
	if err != nil {
		return nil, err
	}
	return json.Marshal(withOwn{Changes: changes, Own: raw})
}

func (l *authority) RestoreWith(raw json.RawMessage, own any) error {
	var w withOwn
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	if err := l.Restore(w.Changes); err != nil {
		return err
	}
	return json.Unmarshal(w.Own, own)
}
