package platform

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Snapshotter is an app whose state the host can save at a journal position
// and restore instead of replaying the entries before it (ADR-0019 D6). Every
// app of a tenant must be one for the tenant to use snapshots; CheckReplay
// checks, for every app, that a snapshot plus the entries after it equals a
// full replay. Restore is given a new app, composed as at start-up.
type Snapshotter interface {
	Snapshot() (json.RawMessage, error)
	Restore(json.RawMessage) error
}

// Snapshot saves the ledger's change log: an app whose other state lives in
// the host's records (ADR-0016) snapshots with this alone.
func (l *Ledger) Snapshot() (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Protos(l.Changes.Records(l.tenant))
}

func (l *Ledger) Restore(raw json.RawMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	records, err := Unprotos[*pb.ChangeRecord](raw)
	if err == nil {
		l.Changes.Restore(l.tenant, records)
	}
	return err
}

// SnapshotFacts and RestoreFacts save and restore a tenant's fact log.
func SnapshotFacts(log *kernel.FactLog, tenant string) (json.RawMessage, error) {
	return Protos(log.Records(tenant))
}

func RestoreFacts(log *kernel.FactLog, tenant string, raw json.RawMessage) error {
	records, err := Unprotos[*pb.FactRecord](raw)
	if err == nil {
		log.Restore(tenant, records)
	}
	return err
}

// Protos encodes kernel messages in their binary form, as a list (a JSON
// array of base64): several times faster and smaller than their JSON form.
func Protos[T proto.Message](list []T) (json.RawMessage, error) {
	out := make([][]byte, len(list))
	for i, m := range list {
		raw, err := proto.Marshal(m)
		if err != nil {
			return nil, err
		}
		out[i] = raw
	}
	return json.Marshal(out)
}

func Unprotos[T interface {
	proto.Message
	*M
}, M any](raw json.RawMessage) ([]T, error) {
	var list [][]byte
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]T, len(list))
	for i, r := range list {
		m := T(new(M))
		if err := proto.Unmarshal(r, m); err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

type withOwn struct {
	Changes json.RawMessage `json:"changes"`
	Own     json.RawMessage `json:"own,omitempty"`
}

// SnapshotWith saves the ledger with the app's own state, any JSON value; the
// app holds its lock around it.
func (l *Ledger) SnapshotWith(own any) (json.RawMessage, error) {
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

// RestoreWith restores the ledger and decodes the app's own state into own.
func (l *Ledger) RestoreWith(raw json.RawMessage, own any) error {
	var w withOwn
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	if err := l.Restore(w.Changes); err != nil {
		return err
	}
	return json.Unmarshal(w.Own, own)
}
