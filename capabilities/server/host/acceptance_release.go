package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	kernelrecords "platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"
)

func (t *Tenant) applyAcceptedRelease(raw []byte) (acceptedformat.Release, error) {
	saved, err := acceptedformat.DecodeRelease(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID || t.App(build.ID) == nil {
		return saved, fmt.Errorf("release result belongs to another tenant or unavailable builder")
	}
	if prior := t.releases.Raw(saved.CandidateID); prior != nil && !bytes.Equal(prior, saved.Bytes) {
		return saved, fmt.Errorf("immutable release candidate %s changed", saved.CandidateID)
	}
	if prior, ok := t.releases.AppliedDigest(saved.Key); ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("release result idempotency key changed")
		}
		return saved, nil
	}
	if len(saved.Installations) > 0 {
		records := t.Projection
		var bootstrap *runtimekernel.PreparedBatch
		if len(saved.Bootstrap) > 0 {
			bootstrap, err = t.prepareAcceptedBatch(t.App(saved.App).AcceptedLedger(), saved.Bootstrap)
			if err != nil || bootstrap == nil {
				return saved, fmt.Errorf("release bootstrap cannot prepare: %v", err)
			}
			records = bootstrap.Records
		}
		view, err := t.assetInstallationView(records)
		if err != nil {
			return saved, err
		}
		view.Projection.Current().SelectRelease(saved.CandidateID)
		draft, err := view.stageReleaseInstallationLocked(saved.Installations, false)
		if err != nil {
			return saved, err
		}
		if err := kernelrecords.ApplyMediaVersionImages(draft.Projection, saved.MediaVersions); err != nil {
			return saved, err
		}
		if saved.Version >= 4 {
			candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
			if err != nil {
				return saved, err
			}
			running, err := draft.acceptedInstallationReleaseLocked(candidate)
			if err != nil || running.ID != candidate.ID {
				return saved, fmt.Errorf("accepted installation differs from its frozen candidate: %v", err)
			}
		}
		if err := t.anchorReleaseRecords(draft.Projection); err != nil {
			return saved, err
		}
		if bootstrap != nil {
			bootstrap.Records = draft.Projection
			if _, err := t.promoteAcceptedBatch(bootstrap); err != nil {
				return saved, err
			}
		} else if err := t.Projection.PromoteRecords(draft.Projection); err != nil {
			return saved, err
		}
		if err := t.installReleaseDefinitions(draft, saved.Installations); err != nil {
			return saved, err
		}
		if bootstrap != nil {
			t.publishAcceptedBatch(bootstrap.Result)
			t.enqueue(saved.At)
		}
	}
	if err := t.releases.Commit(saved.Key, saved.Digest, saved.CandidateID, saved.Bytes, saved.Active); err != nil {
		return saved, err
	}
	if saved.Active {
		t.Projection.Current().SelectRelease(saved.CandidateID)
	}
	t.releases.RememberRequest(saved.Key, saved.RequestHash)
	if saved.Version >= 3 {
		t.Committed.SaveAnswer("release:"+saved.Key, raw)
	}
	if saved.From != "" {
		t.releases.RememberOrigin(saved.CandidateID, platform.ReleaseOrigin{From: saved.From, Member: saved.Member, At: saved.At})
		t.audit.Remember(platform.AuditEntry{At: saved.At, Member: saved.Member, App: controlowner.PlatformApp, Action: "host.promotion",
			Target: saved.From + "→" + t.ID + ":" + saved.CandidateID})
	}
	return saved, nil
}

func (t *Tenant) SaveReleaseCandidates(m platform.Member, drafts []build.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	return decideWithError(t, "release", func(t *Tenant) (string, error) {
		return t.prepareSaveReleaseCandidates(m, drafts, candidateID, key, now)
	}, func(t *Tenant, candidate preparedAccepted) (string, error) {
		return t.applyPreparedRelease(m, candidate)
	})
}

func (t *Tenant) prepareSaveReleaseCandidates(m platform.Member, drafts []build.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	if err := t.Admits(m); err != nil {
		return "", err
	}
	if err := releaseowner.CheckRequest(m, candidateID, key, now, t.quarantined); err != nil {
		return "", err
	}
	candidateBytes, err := releaseowner.SavedCandidateBytes(t.releases.Raw(candidateID), func() (releaseowner.ReleasePreview, platform.ReleaseCandidate, error) {
		return t.previewReleaseLocked(drafts, m)
	}, candidateID)
	if err != nil {
		return "", err
	}
	saved := acceptedformat.Release{Version: 1, Kind: "release-result", Tenant: t.ID, App: build.ID,
		Member: m.ID, Key: "release:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: candidateBytes}
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(t.ID, m.ID, saved.Key, candidateID, false)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

func (t *Tenant) ActivateRelease(m platform.Member, candidateID, key string, now time.Time) (string, error) {
	return t.ActivateReleaseWithUpgrade(m, candidateID, key, "", now)
}

func (t *Tenant) ActivateReleaseWithUpgrade(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	return decideWithError(t, "release", func(t *Tenant) (string, error) {
		return t.prepareActivateReleaseWithUpgrade(m, candidateID, key, upgradeID, now)
	}, func(t *Tenant, candidate preparedAccepted) (string, error) {
		return t.applyPreparedRelease(m, candidate)
	})
}

func (t *Tenant) prepareActivateReleaseWithUpgrade(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	if err := t.Admits(m); err != nil {
		return "", err
	}
	if err := releaseowner.CheckRequest(m, candidateID, key, now, t.quarantined); err != nil {
		return "", err
	}
	if prior := t.Committed.Answer("release:activate:" + key); prior != nil {
		saved, err := acceptedformat.DecodeRelease(prior)
		hash, hashErr := acceptedformat.ReleaseRequestHash(t.ID, m.ID, "activate:"+key, candidateID, true, upgradeID)
		if err != nil || hashErr != nil || hash != saved.RequestHash {
			return "", fmt.Errorf("activation key belongs to another request")
		}
		return saved.CandidateID, nil
	}
	raw := t.releases.Raw(candidateID)
	if raw == nil {
		return "", fmt.Errorf("release candidate %s is not saved", candidateID)
	}
	installations, bootstrap, err := releaseowner.PlanActivation(candidateID, raw, upgradeID, releaseowner.Activation{
		UpgradePlan: func(candidate platform.ReleaseCandidate) (*releaseowner.ReleaseUpgradePlan, error) {
			return t.releaseUpgradePlanLocked(candidate, m)
		},
		Lifecycle: func(candidate platform.ReleaseCandidate) ([]platform.ReleaseInstallation, json.RawMessage, error) {
			return t.prepareReleaseLifecycleLocked(candidate, m, now, upgradeID != "")
		},
	})
	if err != nil {
		return "", err
	}
	saved := acceptedformat.Release{Version: 1, Kind: "release-result", Tenant: t.ID, App: build.ID,
		Member: m.ID, Key: "activate:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: slices.Clone(raw), Active: true, Installations: installations, Bootstrap: bootstrap}
	if len(installations) > 0 {
		saved.Version = 4
	}
	if upgradeID != "" {
		saved.Version, saved.UpgradeID = 4, upgradeID
	}
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(t.ID, m.ID, saved.Key, candidateID, true, upgradeID)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

func (t *Tenant) acceptedAssets() releaseowner.AcceptedAssets {
	return releaseowner.AcceptedAssets{
		Available: func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) },
		WithBindings: func(roots []platform.AssetRef, available []platform.ReleaseAsset) (platform.ReleaseCandidate, error) {
			return t.candidateWithBindings(roots, available, nil)
		},
		Code: func() (map[platform.AssetRef]bool, error) {
			code := map[platform.AssetRef]bool{}
			for _, app := range t.apps {
				assets, err := t.codeReleaseAssetsLocked(app)
				if err != nil {
					return nil, err
				}
				for _, asset := range assets {
					code[asset.Ref] = true
				}
			}
			return code, nil
		},
	}
}

func (t *Tenant) runningReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	return releaseowner.RunningRelease(saved, t.acceptedAssets())
}

func (t *Tenant) acceptedInstallationReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	return releaseowner.AcceptedInstallationRelease(saved, t.acceptedAssets())
}

func (t *Tenant) pendingWorkFitsLocked(candidateID string, raw []byte) error {
	return releaseowner.PendingWorkFits(candidateID, raw, releaseowner.PendingWork{
		Hosted: func() bool { return t.App(platform.WorkApp) != nil },
		Approvals: func() []platform.ApprovalRequest {
			pending, _, _ := platform.Find[platform.ApprovalRequest](t.automation(platform.WorkApp, false),
				platform.Query{Domain: json.RawMessage(`[["state","=","pending"]]`), Sort: []string{"id"}})
			return pending
		},
		Saved: func(id string) []byte { return t.releases.Raw(id) },
	})
}

func (t *Tenant) ActiveRelease() string {
	if t.readOrigin != nil {
		return t.releases.Active()
	}
	if image := t.readImage.Load(); image != nil {
		return image.state.releases.Active()
	}
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) { return t.releases.Active(), nil })
	return value
}

func (t *Tenant) commitReleaseLocked(m platform.Member, saved acceptedformat.Release) (string, error) {
	candidateID := saved.CandidateID
	if saved.Active {
		snapshots, err := kernelrecords.PrepareMediaVersionImages(t.Projection, saved.Installations)
		if err != nil {
			return "", err
		}
		if len(snapshots) > 0 {
			saved.Version = 5
			saved.MediaVersions = snapshots
		}
	}
	raw, err := acceptedformat.EncodeRelease(saved)
	if err != nil {
		return "", err
	}
	principal, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	entry := Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: saved.At}
	if prior, ok := t.releases.Request(saved.Key); ok {
		if prior != saved.RequestHash {
			return "", fmt.Errorf("release key %s belongs to another request", saved.Key)
		}
		return candidateID, nil
	}
	if _, applied := t.releases.AppliedDigest(saved.Key); applied && t.AcceptResult == nil {
		return "", fmt.Errorf("historical release request needs its journal receipt")
	}
	if t.preparedResult != nil {
		*t.preparedResult = preparedAccepted{entry: entry, key: saved.Key, hash: saved.RequestHash, uploads: t.preparedResult.uploads}
		return candidateID, nil
	}
	committed, err := t.acceptResult(entry, saved.Key, saved.RequestHash)
	if err != nil {
		return "", err
	}
	return t.finishCommittedRelease(m, saved, committed)
}

func (t *Tenant) finishCommittedRelease(m platform.Member, saved acceptedformat.Release, committed []byte) (id string, err error) {
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
		t.quarantine(fmt.Errorf("committed release result differs: %v", err))
		return "", fmt.Errorf("committed release result differs: %v", err)
	}
	t.acceptedResultApplied()
	t.changed()
	return applied.CandidateID, nil
}

func (t *Tenant) applyPreparedRelease(m platform.Member, candidate preparedAccepted) (string, error) {
	saved, err := acceptedformat.DecodeRelease(candidate.entry.Body)
	if err != nil {
		return "", err
	}
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		return "", err
	}
	return t.finishCommittedRelease(m, saved, committed)
}
