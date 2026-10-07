package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"platformserver/platform"
)

// releaseStore is the component that owns what a tenant has released: the
// exact, validated candidate bytes by id, which one is active, which release
// commands were applied (by idempotency key → result digest), and the sealed
// artifacts the host console moved out. It is guarded by the tenant's lock
// like the rest of the tenant's committed state (ADR-0039 20b); it never
// derives anything from the mutable development definitions.
type releaseStore struct {
	candidates map[string]json.RawMessage
	applied    map[string]string
	active     string
	sealed     map[string]SealedArtifact
}

// raw is a candidate's exact bytes, nil when it is not saved.
func (r *releaseStore) raw(id string) json.RawMessage { return r.candidates[id] }

// candidate decodes a saved candidate; the error names an unsaved one.
func (r *releaseStore) candidate(id string) (platform.ReleaseCandidate, error) {
	return platform.ReadCandidate(id, r.candidates[id])
}

// ids are the saved candidates, sorted.
func (r *releaseStore) ids() []string {
	ids := slices.Sorted(maps.Keys(r.candidates))
	return ids
}

func (r *releaseStore) count() int { return len(r.candidates) }

// put saves candidate bytes; a saved candidate is immutable, so other bytes
// under the same id are refused.
func (r *releaseStore) put(id string, raw []byte) error {
	if prior, ok := r.candidates[id]; ok && !bytes.Equal(prior, raw) {
		return fmt.Errorf("immutable release candidate %s changed", id)
	}
	if r.candidates == nil {
		r.candidates = map[string]json.RawMessage{}
	}
	r.candidates[id] = slices.Clone(raw)
	return nil
}

// appliedDigest is the digest a release command key was applied with.
func (r *releaseStore) appliedDigest(key string) (string, bool) {
	d, ok := r.applied[key]
	return d, ok
}

// commit records an applied release command: its candidate, activation and key.
func (r *releaseStore) commit(key, digest, candidateID string, raw []byte, activate bool) error {
	if err := r.put(candidateID, raw); err != nil {
		return err
	}
	if activate {
		r.active = candidateID
	}
	if r.applied == nil {
		r.applied = map[string]string{}
	}
	r.applied[key] = digest
	return nil
}

// seal records an artifact written for a candidate; a different digest for
// the same candidate is a conflict.
func (r *releaseStore) seal(id string, artifact SealedArtifact) error {
	if prior, ok := r.sealed[id]; ok && prior.Digest != artifact.Digest {
		return fmt.Errorf("candidate %s is already sealed with another digest", id)
	}
	if r.sealed == nil {
		r.sealed = map[string]SealedArtifact{}
	}
	r.sealed[id] = artifact
	return nil
}

// snapshot and restore carry the store through the tenant snapshot.
func (r *releaseStore) snapshot(s *tenantState) {
	s.ReleaseCandidates, s.ReleaseApplied, s.ActiveRelease, s.Sealed = maps.Clone(r.candidates), maps.Clone(r.applied), r.active, maps.Clone(r.sealed)
}

func (r *releaseStore) restore(s *tenantState) {
	r.candidates, r.applied, r.active = maps.Clone(s.ReleaseCandidates), maps.Clone(s.ReleaseApplied), s.ActiveRelease
	if s.Sealed != nil {
		r.sealed = s.Sealed
	}
}
