package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"google.golang.org/protobuf/proto"
)

// SimulationFactory is trusted composition capability. It creates a fresh
// owner with its original declarations; no records, grants or side effects
// are copied from the active owner.
type SimulationFactory interface {
	NewSimulationApp(tenant string) App
}

// SimulationIdentityFactory constructs only the explicit test principals;
// active authentication subjects, sessions and tokens never enter a sandbox.
type SimulationIdentityFactory interface {
	NewSimulationIdentity(tenant string, members []Member) App
}

// SimulationOrganizationFactory installs only explicit synthetic organization
// input in a fresh owner. It never copies the active enterprise model.
type SimulationOrganizationFactory interface {
	NewSimulationOrganization(tenant string, seed OrgSeed) App
}

// Snapshotter is an app whose state the host can save at a journal position
// and restore instead of replaying the entries before it. Every
// app of a tenant must be one for the tenant to use snapshots; CheckReplay
// checks, for every app, that a snapshot plus the entries after it equals a
// full replay. Restore is given a new app, composed as at start-up.
type Snapshotter interface {
	Snapshot() (json.RawMessage, error)
	Restore(json.RawMessage) error
}

// SnapshotFormatOwner declares the persisted shape an owner's Restore reads.
// Keep the format stable across implementation changes; change it when restoring
// the previous shape would require a migration. Owners without this declaration
// can still use snapshots, but only with the exact binary that wrote them.
type SnapshotFormatOwner interface {
	SnapshotFormat() string
}

// Snapshot saves the ledger's change log: an app whose other state lives in
// the host's records snapshots with this alone.
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
