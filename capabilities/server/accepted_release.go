package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

// acceptedRelease commits immutable candidate bytes or an activation. Format 2
// carries frozen owner publication images beside the pointer; neither is
// reconstructed from a later mutable draft.
type acceptedRelease struct {
	Version     int       `json:"version"`
	Kind        string    `json:"kind"`
	Tenant      string    `json:"tenant"`
	App         string    `json:"app"`
	Member      string    `json:"member"`
	Key         string    `json:"key"`
	At          time.Time `json:"at"`
	CandidateID string    `json:"candidateId"`
	Bytes       []byte    `json:"bytes"`
	// Active marks an activation: the tenant's single release pointer moves to
	// this saved candidate (ADR-0039 D2). A save leaves the pointer alone.
	Active        bool                  `json:"active,omitempty"`
	UpgradeID     string                `json:"upgradeId,omitempty"`
	Installations []releaseInstallation `json:"installations,omitempty"`
	RequestHash   string                `json:"requestHash"`
	Digest        string                `json:"digest"`
}

func releaseRequestHash(tenant, member, key, id string, active bool, upgrade ...string) (string, error) {
	upgradeID := ""
	if len(upgrade) > 0 {
		upgradeID = upgrade[0]
	}
	return canonicalDigest(struct {
		Tenant, Member, Key, CandidateID string
		Active                           bool
		UpgradeID                        string `json:"UpgradeID,omitempty"`
	}{tenant, member, key, id, active, upgradeID})
}

func encodeAcceptedRelease(saved acceptedRelease) ([]byte, error) {
	var err error
	saved.Digest, err = releaseDigest(saved)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return nil, err
	}
	_, err = decodeAcceptedRelease(raw)
	return raw, err
}

func releaseDigest(saved acceptedRelease) (string, error) {
	saved.Digest = ""
	return canonicalDigest(saved)
}

func decodeAcceptedRelease(raw []byte) (acceptedRelease, error) {
	var saved acceptedRelease
	if len(raw) == 0 || len(raw) > 24<<20 {
		return saved, fmt.Errorf("release result is empty or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saved); err != nil {
		return saved, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return saved, fmt.Errorf("release result has trailing data")
	}
	hash, err := releaseRequestHash(saved.Tenant, saved.Member, saved.Key, saved.CandidateID, saved.Active, saved.UpgradeID)
	if err != nil || (saved.Version != 1 && saved.Version != 2 && saved.Version != 3) || saved.Kind != "release-result" || saved.App != build.ID ||
		saved.Tenant == "" || saved.Member == "" || saved.Key == "" || saved.At.IsZero() ||
		saved.RequestHash != hash {
		return saved, fmt.Errorf("invalid release result identity")
	}
	if (saved.Version == 3) != (saved.UpgradeID != "") || saved.Version == 3 && !saved.Active {
		return saved, fmt.Errorf("storage upgrade needs an active format-3 result")
	}
	if len(saved.Installations) > 0 && (saved.Version < 2 || !saved.Active) {
		return saved, fmt.Errorf("definition installation needs an active format-2 release result")
	}
	digest, err := releaseDigest(saved)
	if err != nil || saved.Digest != digest {
		return saved, fmt.Errorf("release result digest differs")
	}
	if _, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes); err != nil {
		return saved, fmt.Errorf("release result candidate: %w", err)
	}
	return saved, nil
}

func (t *Tenant) applyAcceptedRelease(raw []byte) (acceptedRelease, error) {
	saved, err := decodeAcceptedRelease(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID || t.app(build.ID) == nil {
		return saved, fmt.Errorf("release result belongs to another tenant or unavailable builder")
	}
	if prior := t.releases.raw(saved.CandidateID); prior != nil && !bytes.Equal(prior, saved.Bytes) {
		return saved, fmt.Errorf("immutable release candidate %s changed", saved.CandidateID)
	}
	if prior, ok := t.releases.appliedDigest(saved.Key); ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("release result idempotency key changed")
		}
		return saved, nil
	}
	if len(saved.Installations) > 0 {
		if !saved.Active {
			return saved, fmt.Errorf("a saved candidate cannot install definitions")
		}
		draft, err := t.stageReleaseInstallationLocked(saved.Installations, false)
		if err != nil {
			return saved, err
		}
		if err := t.records.promoteRecords(draft.records); err != nil {
			return saved, err
		}
		t.owner, t.definitions = draft.owner, draft.definitions
		publisher := t.app(build.ID).(*build.Build)
		for _, installation := range saved.Installations {
			if err := publisher.ApplyAcceptedPublication(installation.Schema, installation.Image); err != nil {
				return saved, err
			}
		}
	}
	if err := t.releases.commit(saved.Key, saved.Digest, saved.CandidateID, saved.Bytes, saved.Active); err != nil {
		return saved, err
	}
	if saved.Version == 3 {
		t.committed.saveAnswer("release:"+saved.Key, raw)
	}
	return saved, nil
}

// SaveReleaseCandidate freezes a previously previewed saved draft. The
// builder's private definitions are read under the tenant lock; a stale
// preview cannot be persisted by submitting its ID alone. A retry of an
// already-saved ID remains possible even after the editor changes the draft.
func (t *Tenant) SaveReleaseCandidate(m platform.Member, kind platform.AssetKind, draftID, candidateID, key string, now time.Time) (string, error) {
	return t.SaveReleaseCandidates(m, []build.JointDraftRef{{Kind: kind, ID: draftID}}, candidateID, key, now)
}

// SaveReleaseCandidates persists one or several saved drafts as one immutable
// candidate. The bytes are recomputed under the tenant lock, so a joint
// selection cannot be frozen from a stale browser image (ADR-0048 D4).
func (t *Tenant) SaveReleaseCandidates(m platform.Member, drafts []build.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, build.Builder, build.Publisher) {
		return "", fmt.Errorf("builder or publisher role required")
	}
	if candidateID == "" || key == "" || len(key) > 200 || now.IsZero() {
		return "", fmt.Errorf("candidate ID and idempotency key are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	var candidateBytes []byte
	if prior := t.releases.raw(candidateID); prior != nil {
		if t.AcceptResult == nil { // in memory: already saved, nothing new to record
			return candidateID, nil
		}
		candidateBytes = slices.Clone(prior)
	} else {
		preview, candidate, err := t.previewReleaseLocked(drafts)
		if err != nil {
			return "", err
		}
		if preview.Diagnostic != "" {
			return "", fmt.Errorf("release candidate: %s", preview.Diagnostic)
		}
		if candidate.ID != candidateID {
			return "", fmt.Errorf("release candidate changed since preview")
		}
		candidateBytes = candidate.Bytes
	}
	saved := acceptedRelease{Version: 1, Kind: "release-result", Tenant: t.ID, App: build.ID,
		Member: m.ID, Key: "release:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: candidateBytes}
	// The idempotency key has a separate namespace from builder submissions.
	var err error
	saved.RequestHash, err = releaseRequestHash(t.ID, m.ID, saved.Key, candidateID, false)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

// ActivateRelease installs the supported saved closure and moves its pointer
// in one committed result (ADR-0039 D2). Unsupported upgrades and changed code
// dependencies are refused before either state is exposed.
func (t *Tenant) ActivateRelease(m platform.Member, candidateID, key string, now time.Time) (string, error) {
	return t.ActivateReleaseWithUpgrade(m, candidateID, key, "", now)
}

func (t *Tenant) ActivateReleaseWithUpgrade(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, build.Builder, build.Publisher) {
		return "", fmt.Errorf("builder or publisher role required")
	}
	if candidateID == "" || key == "" || len(key) > 200 || now.IsZero() {
		return "", fmt.Errorf("candidate ID and idempotency key are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	if prior := t.committed.answers["release:activate:"+key]; prior != nil {
		saved, err := decodeAcceptedRelease(prior)
		hash, hashErr := releaseRequestHash(t.ID, m.ID, "activate:"+key, candidateID, true, upgradeID)
		if err != nil || hashErr != nil || hash != saved.RequestHash {
			return "", fmt.Errorf("activation key belongs to another request")
		}
		return saved.CandidateID, nil
	}
	raw := t.releases.raw(candidateID)
	if raw == nil {
		return "", fmt.Errorf("release candidate %s is not saved", candidateID)
	}
	var installations []releaseInstallation
	if upgradeID != "" {
		candidate, err := platform.ReadCandidate(candidateID, raw)
		if err != nil {
			return "", err
		}
		plan, err := t.releaseUpgradePlanLocked(candidate)
		if err != nil {
			return "", err
		}
		if plan == nil || plan.ID != upgradeID {
			return "", fmt.Errorf("storage upgrade plan changed; refresh and review it again")
		}
	}
	if t.runningMatchesLocked(candidateID, raw) != nil {
		var err error
		installations, err = t.prepareReleaseActivationLocked(candidateID, raw, upgradeID != "")
		if err != nil {
			return "", err
		}
	} else if err := t.pendingWorkFitsLocked(candidateID, raw); err != nil {
		return "", err
	}
	if err := t.evaluationsReadyLocked(candidateID, raw); err != nil {
		return "", err
	}
	saved := acceptedRelease{Version: 1, Kind: "release-result", Tenant: t.ID, App: build.ID,
		Member: m.ID, Key: "activate:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: slices.Clone(raw), Active: true, Installations: installations}
	if len(installations) > 0 {
		saved.Version = 2
	}
	if upgradeID != "" {
		saved.Version, saved.UpgradeID = 3, upgradeID
	}
	var err error
	saved.RequestHash, err = releaseRequestHash(t.ID, m.ID, saved.Key, candidateID, true, upgradeID)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

func (t *Tenant) runningMatchesLocked(candidateID string, raw []byte) error {
	saved, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	running, err := t.runningReleaseLocked(saved)
	if err != nil {
		return fmt.Errorf("release differs from the running definitions: %w", err)
	}
	if running.ID == candidateID {
		return nil
	}
	added, removed, changed, err := platform.CandidateDiff(saved, running)
	if err != nil {
		return err
	}
	return fmt.Errorf("release differs from the running definitions: changed %v, missing %v, extra %v", changed, removed, added)
}

// Read-side status and activation compare the same owner-resolved closure.
func (t *Tenant) runningReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	// Rebuild the same closure from what runs now: equal content yields the
	// same canonical ID; anything else is reported by asset path.
	roots := make([]platform.AssetRef, 0, len(saved.Assets))
	for _, asset := range saved.Assets {
		roots = append(roots, asset.Ref)
	}
	return t.candidateWithBindings(roots, available, nil)
}

// pendingWorkFitsLocked refuses an activation that would change the action a
// pending approval holds: the approval must run under the release it opened
// in (ADR-0039 D4). There is no migration; decide or withdraw it first.
func (t *Tenant) pendingWorkFitsLocked(candidateID string, raw []byte) error {
	if t.app(work.ID) == nil {
		return nil
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	actionIn := func(c platform.ReleaseCandidate, name string) []byte {
		for _, a := range c.Assets {
			if a.Ref.Kind == platform.AssetAction && a.Ref.Name == name {
				out, _ := json.Marshal(a)
				return out
			}
		}
		return nil
	}
	pending, _, _ := platform.Find[work.ApprovalRequest](t.automation(work.ID, false),
		platform.Query{Domain: json.RawMessage(`[["state","=","pending"]]`), Sort: []string{"id"}})
	for _, request := range pending {
		next := actionIn(candidate, request.Action)
		if next == nil || request.Release == candidateID {
			continue
		}
		if saved := t.releases.raw(request.Release); saved != nil {
			started, err := platform.ReadCandidate(request.Release, saved)
			if err == nil && bytes.Equal(actionIn(started, request.Action), next) {
				continue
			}
		}
		return fmt.Errorf("pending approval %s holds %s under another release; decide or withdraw it before activating", request.ID, request.Action)
	}
	return nil
}

// ActiveRelease is the tenant's active release ID, empty before any activation.
func (t *Tenant) ActiveRelease() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.releases.active
}

// commitReleaseLocked appends one release result and applies only what the
// journal returned; the caller holds the tenant lock.
func (t *Tenant) commitReleaseLocked(m platform.Member, saved acceptedRelease) (string, error) {
	candidateID := saved.CandidateID
	raw, err := encodeAcceptedRelease(saved)
	if err != nil {
		return "", err
	}
	principal, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	entry := Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: saved.At}
	if t.AcceptResult == nil {
		// The in-memory development host journals through Record, like its
		// other inputs; replay applies the same saved bytes.
		if _, err := t.applyAcceptedRelease(raw); err != nil {
			return "", err
		}
		if t.Record != nil {
			t.Record(entry)
		}
		t.changed()
		return candidateID, nil
	}
	committed, err := t.AcceptResult(entry, saved.Key, saved.RequestHash)
	if err != nil {
		return "", err
	}
	applied, err := t.applyAcceptedRelease(committed)
	if err != nil || applied.Key != saved.Key || applied.RequestHash != saved.RequestHash ||
		applied.Member != m.ID || applied.CandidateID != candidateID {
		// Once appended, an invalid result is a tenant recovery fault, not a
		// transient error that may permit further writes over uncertain state.
		t.quarantine(fmt.Errorf("committed release result differs: %v", err))
		return "", fmt.Errorf("committed release result differs: %v", err)
	}
	t.changed()
	return applied.CandidateID, nil
}
