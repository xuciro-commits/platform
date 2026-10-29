package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"google.golang.org/protobuf/proto"
)

type SimulationFactory interface {
	NewSimulationApp(tenant string) App
}

type SimulationIdentityFactory interface {
	NewSimulationIdentity(tenant string, members []Member) App
}

type SimulationOrganizationFactory interface {
	NewSimulationOrganization(tenant string, seed OrgSeed) App
}

type Snapshotter interface {
	Snapshot() (json.RawMessage, error)
	Restore(json.RawMessage) error
}

type SnapshotFormatOwner interface {
	SnapshotFormat() string
}

func (l *Ledger) Snapshot() (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Protos(l.changes.Records(l.tenant))
}

func (l *Ledger) Restore(raw json.RawMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	records, err := Unprotos[*pb.ChangeRecord](raw)
	if err == nil {
		l.changes.Restore(l.tenant, records)
	}
	return err
}

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
