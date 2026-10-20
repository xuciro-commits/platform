package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"slices"
	"strings"
	"time"

	"platformserver/internal/construction"
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
	// this saved candidate (ADR-0011 D2). A save leaves the pointer alone.
	Active    bool   `json:"active,omitempty"`
	UpgradeID string `json:"upgradeId,omitempty"`
	// From names the environment a promoted candidate's sealed bytes came
	// from; the target's audit of the promotion is rebuilt from it on replay.
	From          string                `json:"from,omitempty"`
	Installations []releaseInstallation `json:"installations,omitempty"`
	Bootstrap     json.RawMessage       `json:"bootstrap,omitempty"`
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
	if err := decodeAcceptedJSON(raw, &saved, "release result"); err != nil {
		return saved, err
	}
	hash, err := releaseRequestHash(saved.Tenant, saved.Member, saved.Key, saved.CandidateID, saved.Active, saved.UpgradeID)
	if err != nil || (saved.Version != 1 && saved.Version != 2 && saved.Version != 3 && saved.Version != 4) || saved.Kind != "release-result" || saved.App != construction.ID ||
		saved.Tenant == "" || saved.Member == "" || saved.Key == "" || saved.At.IsZero() ||
		saved.RequestHash != hash {
		return saved, fmt.Errorf("invalid release result identity")
	}
	if saved.Version < 4 && (saved.Version == 3) != (saved.UpgradeID != "") || saved.UpgradeID != "" && !saved.Active {
		return saved, fmt.Errorf("storage upgrade needs an active format-3 result")
	}
	if len(saved.Installations) > 0 && (saved.Version < 2 || !saved.Active) {
		return saved, fmt.Errorf("definition installation needs an active format-2 release result")
	}
	if len(saved.Bootstrap) > 0 {
		if saved.Version != 4 || !saved.Active || len(saved.Installations) == 0 {
			return saved, fmt.Errorf("draft bootstrap needs a complete format-4 activation")
		}
		batch, receipt, err := decodeAcceptedBatch(saved.Bootstrap)
		if err != nil || batch.Tenant != saved.Tenant || batch.App != saved.App || !batch.At.Equal(saved.At) || receipt.GetSubmission().GetPrincipalId() != saved.Member {
			return saved, fmt.Errorf("release bootstrap identity differs: %v", err)
		}
		for _, decision := range batch.Decisions {
			r := new(pb.ChangeRecord)
			if protojson.Unmarshal(decision.Receipt, r) != nil {
				return saved, fmt.Errorf("invalid release bootstrap decision")
			}
			sub := r.GetSubmission()
			if sub.GetPrincipalId() != saved.Member || !strings.HasPrefix(sub.GetIdempotencyKey(), "release-bootstrap:"+saved.CandidateID+":"+saved.Member+":") {
				return saved, fmt.Errorf("release bootstrap belongs to another command")
			}
		}
	}
	if saved.Version == 4 {
		candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
		if err != nil {
			return saved, err
		}
		refs := map[platform.AssetRef]bool{}
		for _, asset := range candidate.Assets {
			refs[asset.Ref] = true
		}
		seen := map[platform.AssetRef]bool{}
		records := map[string]bool{}
		for _, installation := range saved.Installations {
			if installation.Ref == nil || !refs[*installation.Ref] || seen[*installation.Ref] || installation.Record == "" || records[installation.Record] {
				return saved, fmt.Errorf("format-4 release has missing, duplicate or unrelated installation identity")
			}
			seen[*installation.Ref] = true
			records[installation.Record] = true
		}
		if len(saved.Bootstrap) > 0 {
			batch, _, _ := decodeAcceptedBatch(saved.Bootstrap)
			for _, decision := range batch.Decisions {
				r := new(pb.ChangeRecord)
				_ = protojson.Unmarshal(decision.Receipt, r)
				sub := r.GetSubmission()
				ref := sub.GetTarget().GetType() + "/" + sub.GetTarget().GetId()
				if !records[ref] || sub.GetSchema().GetName() != sub.GetTarget().GetType()+".create" || decision.Publication != nil {
					return saved, fmt.Errorf("release bootstrap is not an original create of its installation metadata")
				}
			}
			for _, row := range batch.Rows {
				if !records[row.Type+"/"+row.ID] {
					return saved, fmt.Errorf("release bootstrap has an unrelated row")
				}
			}
			if len(batch.DirectRows) > 0 || batch.Control != nil {
				return saved, fmt.Errorf("release bootstrap has an unrelated callback or control mutation")
			}
		}
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
	if saved.Tenant != t.ID || t.app(construction.ID) == nil {
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
		records := t.records
		var bootstrap *preparedAcceptedBatch
		if len(saved.Bootstrap) > 0 {
			bootstrap, err = t.prepareAcceptedBatch(t.app(saved.App).AcceptedLedger(), saved.Bootstrap)
			if err != nil || bootstrap == nil {
				return saved, fmt.Errorf("release bootstrap cannot prepare: %v", err)
			}
			records = bootstrap.records
		}
		view, err := t.assetInstallationView(records)
		if err != nil {
			return saved, err
		}
		view.records.readRelease = saved.CandidateID
		draft, err := view.stageReleaseInstallationLocked(saved.Installations, false)
		if err != nil {
			return saved, err
		}
		if saved.Version == 4 {
			candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
			if err != nil {
				return saved, err
			}
			running, err := draft.acceptedInstallationReleaseLocked(candidate)
			if err != nil || running.ID != candidate.ID {
				return saved, fmt.Errorf("accepted installation differs from its frozen candidate: %v", err)
			}
		}
		if err := t.anchorReleaseRecords(draft.records); err != nil {
			return saved, err
		}
		if bootstrap != nil {
			bootstrap.records = draft.records
			if _, err := t.promoteAcceptedBatch(bootstrap); err != nil {
				return saved, err
			}
		} else if err := t.records.promoteRecords(draft.records); err != nil {
			return saved, err
		}
		t.definitions = draft.definitions
		for key, app := range draft.owner {
			draft.owner[key] = t.app(app.Manifest().ID)
		}
		t.owner = draft.owner
		registry, err := t.assetRegistryLocked()
		if err != nil {
			return saved, err
		}
		for _, installation := range saved.Installations {
			plan, err := t.installationPlan(registry, installation)
			if err != nil {
				return saved, err
			}
			if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{}); err != nil {
				return saved, err
			}
		}
		if bootstrap != nil {
			t.publishAcceptedBatch(bootstrap.result)
			t.enqueue(saved.At)
		}
	}
	if err := t.releases.commit(saved.Key, saved.Digest, saved.CandidateID, saved.Bytes, saved.Active); err != nil {
		return saved, err
	}
	if saved.Active {
		t.records.readRelease = saved.CandidateID
	}
	t.releases.rememberRequest(saved.Key, saved.RequestHash)
	if saved.Version >= 3 {
		t.committed.saveAnswer("release:"+saved.Key, raw)
	}
	if saved.From != "" {
		t.releases.rememberOrigin(saved.CandidateID, ReleaseOrigin{From: saved.From, Member: saved.Member, At: saved.At})
		t.audit.remember(AuditEntry{At: saved.At, Member: saved.Member, App: PlatformApp, Action: "host.promotion",
			Target: saved.From + "→" + t.ID + ":" + saved.CandidateID})
	}
	return saved, nil
}

// SaveReleaseCandidate freezes a previously previewed saved draft. The
// builder's private definitions are read under the tenant lock; a stale
// preview cannot be persisted by submitting its ID alone. A retry of an
// already-saved ID remains possible even after the editor changes the draft.
func (t *Tenant) SaveReleaseCandidate(m platform.Member, kind platform.AssetKind, draftID, candidateID, key string, now time.Time) (string, error) {
	return t.SaveReleaseCandidates(m, []construction.JointDraftRef{{Kind: kind, ID: draftID}}, candidateID, key, now)
}

// SaveReleaseCandidates persists one or several saved drafts as one immutable
// candidate. The bytes are recomputed under the tenant lock, so a joint
// selection cannot be frozen from a stale browser image (ADR-0011).
func (t *Tenant) SaveReleaseCandidates(m platform.Member, drafts []construction.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, construction.Builder, construction.Publisher) {
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
		candidateBytes = slices.Clone(prior)
	} else {
		preview, candidate, err := t.previewReleaseLocked(drafts, m)
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
	saved := acceptedRelease{Version: 1, Kind: "release-result", Tenant: t.ID, App: construction.ID,
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
// in one committed result (ADR-0011 D2). Unsupported upgrades and changed code
// dependencies are refused before either state is exposed.
func (t *Tenant) ActivateRelease(m platform.Member, candidateID, key string, now time.Time) (string, error) {
	return t.ActivateReleaseWithUpgrade(m, candidateID, key, "", now)
}

func (t *Tenant) ActivateReleaseWithUpgrade(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, construction.Builder, construction.Publisher) {
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
	if prior := t.committed.answer("release:activate:" + key); prior != nil {
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
	var bootstrap json.RawMessage
	if upgradeID != "" {
		candidate, err := platform.ReadCandidate(candidateID, raw)
		if err != nil {
			return "", err
		}
		plan, err := t.releaseUpgradePlanLocked(candidate, m)
		if err != nil {
			return "", err
		}
		if plan == nil || plan.ID != upgradeID {
			return "", fmt.Errorf("storage upgrade plan changed; refresh and review it again")
		}
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return "", err
	}
	migrated, err := t.migrationActivationLocked(candidateID)
	if err != nil {
		return "", err
	}
	installations, bootstrap, err = t.prepareReleaseLifecycleLocked(candidate, m, now, upgradeID != "" || migrated)
	if err != nil {
		return "", err
	}
	if err := t.evaluationsReadyLocked(candidateID, raw); err != nil {
		return "", err
	}
	saved := acceptedRelease{Version: 1, Kind: "release-result", Tenant: t.ID, App: construction.ID,
		Member: m.ID, Key: "activate:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: slices.Clone(raw), Active: true, Installations: installations, Bootstrap: bootstrap}
	if len(installations) > 0 {
		saved.Version = 4
	}
	if upgradeID != "" {
		saved.Version, saved.UpgradeID = 4, upgradeID
	}
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

// Recovery checks the saved installation's controlled assets, not today's
// native prerequisites. Native assets in a release are declarations, never
// installation images: keep their original bytes for this comparison only.
// New activation still requires runtimeAssetOwner.controlled's exact match;
// old Flow execution keeps its separate code-dependency admission gate.
func (t *Tenant) acceptedInstallationReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	code := map[platform.AssetRef]bool{}
	for _, app := range t.apps {
		assets, err := t.codeReleaseAssetsLocked(app)
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		for _, asset := range assets {
			code[asset.Ref] = true
		}
	}
	frozen := map[platform.AssetRef]platform.ReleaseAsset{}
	roots := []platform.AssetRef{}
	for _, asset := range saved.Assets {
		roots = append(roots, asset.Ref)
		if code[asset.Ref] {
			frozen[asset.Ref] = asset
		}
	}
	for index, asset := range available {
		if original, exists := frozen[asset.Ref]; exists {
			available[index] = original
		}
	}
	return t.candidateWithBindings(roots, available, nil)
}

// pendingWorkFitsLocked refuses an activation that would change the action a
// pending approval holds: the approval must run under the release it opened
// in (ADR-0011 D4). There is no migration; decide or withdraw it first.
func (t *Tenant) pendingWorkFitsLocked(candidateID string, raw []byte) error {
	if t.app(platform.WorkApp) == nil {
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
	pending, _, _ := platform.Find[platform.ApprovalRequest](t.automation(platform.WorkApp, false),
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
	entry := Entry{App: construction.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: saved.At}
	if prior, ok := t.releases.request(saved.Key); ok {
		if prior != saved.RequestHash {
			return "", fmt.Errorf("release key %s belongs to another request", saved.Key)
		}
		return candidateID, nil
	}
	if _, applied := t.releases.appliedDigest(saved.Key); applied && t.AcceptResult == nil {
		return "", fmt.Errorf("historical release request needs its journal receipt")
	}
	committed, err := t.acceptResult(entry, saved.Key, saved.RequestHash)
	if err != nil {
		return "", err
	}
	return t.finishCommittedRelease(m, saved, committed)
}

func (t *Tenant) finishCommittedRelease(m platform.Member, saved acceptedRelease, committed []byte) (id string, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("apply committed release: %v", failure)
			t.quarantine(err)
			id = ""
		}
	}()
	candidateID := saved.CandidateID
	applied, err := t.applyAcceptedRelease(committed)
	if err != nil || applied.Key != saved.Key || applied.RequestHash != saved.RequestHash ||
		applied.Member != m.ID || applied.CandidateID != candidateID {
		// Once appended, an invalid result is a tenant recovery fault, not a
		// transient error that may permit further writes over uncertain state.
		t.quarantine(fmt.Errorf("committed release result differs: %v", err))
		return "", fmt.Errorf("committed release result differs: %v", err)
	}
	t.acceptedResultApplied()
	t.changed()
	return applied.CandidateID, nil
}
