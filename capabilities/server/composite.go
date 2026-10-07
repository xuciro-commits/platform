package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Composite edits (ADR-0047 §11, ordered by the owner on 2026-10-05): one
// command that edits several of an application's assets as a unit.
//
// Every edit is staged on a private fork of the tenant's records and
// definitions and probed with the same rules, permissions and attribute checks
// an ordinary submission meets — including a project's asset edit delegation.
// Only when every edit is accepted is the composite itself committed as one
// accepted result: the fork is promoted in the same commit, so a refused edit
// refuses the whole command and nothing half-applied becomes visible. The
// committed result is journaled like a release result, and replay re-runs the
// same edits against the replayed state.

// CompositeEdit is one asset edit of a composite command.
type CompositeEdit struct {
	Schema  string          `json:"schema"`
	Target  *pb.EntityRef   `json:"target"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// CompositeAnswer says what the tenant accepted.
type CompositeAnswer struct {
	Key     string `json:"key"`
	Applied int    `json:"applied"`
	Digest  string `json:"digest,omitempty"`
}

// acceptedComposite is the journaled identity of one composite command.
type acceptedComposite struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	Tenant      string          `json:"tenant"`
	App         string          `json:"app"`
	Member      string          `json:"member"`
	Key         string          `json:"key"`
	At          time.Time       `json:"at"`
	Edits       []CompositeEdit `json:"edits"`
	RequestHash string          `json:"requestHash"`
	Digest      string          `json:"digest"`
}

func compositeRequestHash(tenant, member, key string, edits []CompositeEdit) (string, error) {
	return canonicalDigest(struct {
		Tenant, Member, Key string
		Edits               []CompositeEdit
	}{tenant, member, key, edits})
}

func encodeAcceptedComposite(saved acceptedComposite) ([]byte, error) {
	var err error
	if saved.Digest, err = compositeDigest(saved); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return nil, err
	}
	_, err = decodeAcceptedComposite(raw)
	return raw, err
}

func compositeDigest(saved acceptedComposite) (string, error) {
	saved.Digest = ""
	return canonicalDigest(saved)
}

func decodeAcceptedComposite(raw []byte) (acceptedComposite, error) {
	var saved acceptedComposite
	if len(raw) == 0 || len(raw) > 4<<20 {
		return saved, fmt.Errorf("composite result is empty or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saved); err != nil {
		return saved, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return saved, fmt.Errorf("composite result has trailing data")
	}
	hash, err := compositeRequestHash(saved.Tenant, saved.Member, saved.Key, saved.Edits)
	if err != nil || saved.Version != 1 || saved.Kind != "composite-result" || saved.App != build.ID ||
		saved.Tenant == "" || saved.Member == "" || saved.Key == "" || saved.At.IsZero() ||
		len(saved.Edits) == 0 || len(saved.Edits) > 64 || saved.RequestHash != hash {
		return saved, fmt.Errorf("invalid composite result identity")
	}
	digest, err := compositeDigest(saved)
	if err != nil || saved.Digest != digest {
		return saved, fmt.Errorf("composite result digest differs")
	}
	for i, edit := range saved.Edits {
		if edit.Schema == "" || edit.Target == nil || edit.Target.GetType() == "" {
			return saved, fmt.Errorf("composite edit %d has no schema or target", i+1)
		}
	}
	return saved, nil
}

// Composite stages, commits and answers one composite command. Edits are
// builder actions; a member with a project's edit delegation may edit exactly
// the assets the project names, and the delegation is resolved per edit.
func (t *Tenant) Composite(m platform.Member, key string, edits []CompositeEdit, now time.Time) (CompositeAnswer, error) {
	if err := t.admits(m); err != nil {
		return CompositeAnswer{}, err
	}
	if key == "" || len(key) > 200 || len(edits) == 0 || len(edits) > 64 || now.IsZero() {
		return CompositeAnswer{}, fmt.Errorf("a composite needs an idempotency key and one to 64 edits")
	}
	if t.app(build.ID) == nil {
		return CompositeAnswer{}, fmt.Errorf("this tenant runs no builder")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return CompositeAnswer{}, fmt.Errorf("tenant is quarantined")
	}
	saved := acceptedComposite{Version: 1, Kind: "composite-result", Tenant: t.ID, App: build.ID, Member: m.ID,
		Key: "composite:" + key, At: now.UTC(), Edits: edits}
	var err error
	if saved.RequestHash, err = compositeRequestHash(t.ID, m.ID, saved.Key, edits); err != nil {
		return CompositeAnswer{}, err
	}
	raw, err := encodeAcceptedComposite(saved)
	if err != nil {
		return CompositeAnswer{}, err
	}
	// Stage every edit before anything is journaled: a refusal here leaves no trace.
	if _, err := t.stageCompositeLocked(m, edits, key, now); err != nil {
		return CompositeAnswer{}, err
	}
	if t.AcceptResult == nil {
		applied, err := t.applyAcceptedComposite(raw)
		if err != nil {
			return CompositeAnswer{}, err
		}
		if t.Record != nil {
			principal, _ := json.Marshal(m)
			t.Record(Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: now})
		}
		t.changed()
		return CompositeAnswer{Key: key, Applied: len(applied.Edits), Digest: applied.Digest}, nil
	}
	principal, _ := json.Marshal(m)
	entry := Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: now}
	committed, err := t.AcceptResult(entry, saved.Key, saved.RequestHash)
	if err != nil {
		return CompositeAnswer{}, err
	}
	applied, err := t.applyAcceptedComposite(committed)
	if err != nil || applied.Key != saved.Key || applied.RequestHash != saved.RequestHash || applied.Member != m.ID {
		t.quarantine(fmt.Errorf("committed composite result differs: %v", err))
		return CompositeAnswer{}, fmt.Errorf("committed composite result differs: %v", err)
	}
	t.changed()
	return CompositeAnswer{Key: key, Applied: len(applied.Edits), Digest: applied.Digest}, nil
}

// stageCompositeLocked probes every edit on a private fork. The draft shares
// the apps (their ledgers are the apps' own business) but owns its records and
// definitions, so a refusal cannot change what the tenant sees.
func (t *Tenant) stageCompositeLocked(m platform.Member, edits []CompositeEdit, key string, now time.Time) (*Tenant, error) {
	owner, ok := t.app(build.ID).(*build.Build)
	if !ok {
		return nil, fmt.Errorf("this tenant runs no builder")
	}
	draft := (hostView{t: t, app: owner}).installationDraft()
	for i, edit := range edits {
		app := t.owner["action:"+edit.Schema]
		if app == nil || app.Manifest().ID != build.ID {
			return nil, fmt.Errorf("edit %d: %s is not a builder action", i+1, edit.Schema)
		}
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID,
			IdempotencyKey: key + "#" + strconv.Itoa(i), Target: edit.Target,
			Schema: &pb.SchemaRef{Name: edit.Schema, Version: 1}, Payload: edit.Payload}
		if err := t.delegatedBound(m, sub); err != nil {
			return nil, fmt.Errorf("edit %d: %s refused (%s)", i+1, edit.Schema, err.Error())
		}
		if _, err := draft.Submit(m, sub, now); err != nil {
			return nil, fmt.Errorf("edit %d: %s refused (%s)", i+1, edit.Schema, err.Error())
		}
	}
	return draft, nil
}

// applyAcceptedComposite applies a committed composite result: it re-stages the
// edits on the current state and promotes the fork, so applying is the same
// work the submission did and replay reproduces the committed records.
func (t *Tenant) applyAcceptedComposite(raw []byte) (acceptedComposite, error) {
	saved, err := decodeAcceptedComposite(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID {
		return saved, fmt.Errorf("composite result belongs to another tenant")
	}
	if prior, ok := t.committed.composites[saved.Key]; ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("composite idempotency key changed")
		}
		return saved, nil
	}
	draft, err := t.stageCompositeLocked(platform.Member{ID: saved.Member, Tenant: t.ID, Roles: t.rolesOf(saved.Member)}, saved.Edits, saved.Key, saved.At)
	if err != nil {
		return saved, fmt.Errorf("committed composite does not apply: %w", err)
	}
	if err := t.records.promoteRecords(draft.records); err != nil {
		return saved, err
	}
	t.definitions = draft.definitions
	t.committed.saveComposite(saved.Key, saved.Digest)
	t.audit.remember(AuditEntry{At: saved.At, Member: saved.Member, App: build.ID, Action: "composite", Target: saved.Key})
	return saved, nil
}

// rolesOf restores a member's roles from the console for replay, where only the
// member ID is journaled.
func (t *Tenant) rolesOf(member string) map[string]string {
	if m, ok := t.Member(member); ok {
		return m.Roles
	}
	return map[string]string{}
}
