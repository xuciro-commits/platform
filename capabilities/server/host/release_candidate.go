package host

import (
	"context"
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"time"
)

func (t *Tenant) ReleaseCandidate(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (platform.ReleaseCandidate, error) {
		available, err := t.releaseAssetsLocked(nil, false)
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		return t.candidateWithBindings(roots, available, roots)
	})
}

func (t *Tenant) draftWorldLocked(m platform.Member, branch string, now time.Time) (*Tenant, *kernel.Error) {
	if branch == "" {
		return t, nil
	}
	world, refusal := t.branchWorldLocked(m, branch, now)
	if refusal != nil {
		return nil, refusal
	}
	return world.view, nil
}

func (t *Tenant) ReferencedDrafts(m platform.Member, kind platform.AssetKind, id, branch string, now time.Time) (releaseowner.ReleaseDraftClosure, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleaseDraftClosure, error) {
		if err := t.buildAllowed(m, "POST /v1/releases/drafts/referenced", build.Builder); err != nil {
			return releaseowner.ReleaseDraftClosure{}, err
		}
		world, refusal := t.draftWorldLocked(m, branch, now)
		if refusal != nil {
			return releaseowner.ReleaseDraftClosure{}, refusal
		}
		owner := world.builders[build.ID]
		if owner == nil {
			return releaseowner.ReleaseDraftClosure{}, fmt.Errorf("tenant has no builder")
		}
		mine := ""
		if branch == "" {
			mine = m.ID
		}
		return releaseowner.ReferencedDraftClosure(owner, kind, id, mine)
	})
}

func (t *Tenant) buildAllowed(m platform.Member, permission string, roles ...string) error {
	if err := t.Admits(m); err != nil {
		return err
	}
	c := platform.NewCaller(runtime{t: t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, permission, "read", roles, true)); err != nil {
		return err
	}
	return nil
}

func (t *Tenant) builderDrafts(m platform.Member, permission string) (build.ConstructionDrafts, error) {
	if err := t.buildAllowed(m, permission, build.Builder); err != nil {
		return nil, err
	}
	owner := t.builders[build.ID]
	if owner == nil {
		return nil, fmt.Errorf("tenant has no builder")
	}
	return owner, nil
}

func (t *Tenant) DraftChange(m platform.Member, kind platform.AssetKind, id string) (build.DraftChange, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (build.DraftChange, error) {
		owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/change")
		if err != nil {
			return build.DraftChange{}, err
		}
		change, found, err := owner.DraftChangeOf(m, kind, id)
		if err == nil && !found {
			err = fmt.Errorf("The draft has no working changes")
		}
		return change, err
	})
}

func (t *Tenant) DraftRestore(m platform.Member, kind platform.AssetKind, id string, now time.Time) (releaseowner.ReleaseDraftRestore, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleaseDraftRestore, error) {
		owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/restore")
		if err != nil {
			return releaseowner.ReleaseDraftRestore{}, err
		}
		return releaseowner.DraftRestore(owner, t.Entities(m), m, kind, id, now)
	})
}

func (t *Tenant) WorkingDrafts(m platform.Member) (releaseowner.ReleaseWorkingState, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleaseWorkingState, error) {
		owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/working")
		if err != nil {
			return releaseowner.ReleaseWorkingState{}, err
		}
		return releaseowner.WorkingDrafts(owner, m.ID)
	})
}

func (t *Tenant) PreviewReleaseDrafts(m platform.Member, drafts []build.JointDraftRef, branch string, now time.Time) (releaseowner.ReleasePreview, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleasePreview, error) {
		if err := t.buildAllowed(m, "POST /v1/releases/preview", build.Builder); err != nil {
			return releaseowner.ReleasePreview{}, err
		}
		world, refusal := t.draftWorldLocked(m, branch, now)
		if refusal != nil {
			return releaseowner.ReleasePreview{}, refusal
		}
		reply, err := world.releasePlannerLocked().PreviewWithActions(drafts, m)
		if err != nil {
			return reply, err
		}
		definitions, err := world.DefinitionsFor(m)
		if err != nil {
			return reply, err
		}
		releaseowner.HideInvisibleDownstream(&reply, definitions)
		return reply, nil
	})
}

func (t *Tenant) releasePlannerLocked() releaseowner.Planner {
	return releaseowner.Planner{Definitions: t.definitions, Registry: t.assetRegistryLocked, Drafts: t.builders[build.ID],
		Validate: func(candidate platform.ReleaseCandidate, member platform.Member, at time.Time) error {
			_, _, err := t.prepareReleaseInstallationLocked(candidate, member, at, true, false)
			return err
		}}
}
func (t *Tenant) releaseAssetsLocked(assets []platform.ReleaseAsset, replace bool) ([]platform.ReleaseAsset, error) {
	return t.releasePlannerLocked().Assets(assets, replace)
}
func (t *Tenant) codeReleaseAssetsLocked(app platform.App) ([]platform.ReleaseAsset, error) {
	return releaseowner.CodeAssets(app, t.definitions, t.describer().DefinitionAsset, t.nativeFlowAsset, t.describer().DescribeOwnedAsset)
}
func (t *Tenant) previewReleaseLocked(drafts []build.JointDraftRef, member platform.Member) (releaseowner.ReleasePreview, platform.ReleaseCandidate, error) {
	return t.releasePlannerLocked().Preview(drafts, member)
}

func (t *Tenant) candidateWithBindings(roots []platform.AssetRef, available []platform.ReleaseAsset, fixed []platform.AssetRef) (platform.ReleaseCandidate, error) {
	return t.releasePlannerLocked().WithBindings(roots, available, fixed)
}

func (t *Tenant) releaseAcceptance() releaseowner.ReleaseAcceptance {
	return releaseowner.ReleaseAcceptance{
		Tenant: t.ID, Store: &t.releases, Projection: t.Projection,
		Builder:     func() bool { return t.App(build.ID) != nil },
		Admits:      t.Admits,
		Quarantined: t.quarantined,
		Answer:      func(key string) json.RawMessage { return t.Committed.Answer(key) },
		SaveAnswer:  func(key string, raw json.RawMessage) { t.Committed.SaveAnswer(key, raw) },
		Promoted: func(at time.Time, member, target string) {
			t.AuditLog.Remember(platform.AuditEntry{At: at, Member: member, App: controlowner.PlatformApp, Action: "host.promotion", Target: target})
		},
		Enqueue:     t.enqueue,
		Preview:     t.previewReleaseLocked,
		UpgradePlan: t.releaseUpgradePlanLocked,
		Lifecycle:   t.prepareReleaseLifecycleLocked,
		Bootstrap: func(app string, raw json.RawMessage) (*releaseowner.AcceptedBootstrap, error) {
			batch, err := t.PrepareBatch(t.App(app).AcceptedLedger(), raw, t.batchApply())
			if err != nil || batch == nil {
				return nil, err
			}
			return &releaseowner.AcceptedBootstrap{Records: batch.Records,
				Promote: func(draft *records.Store) error {
					batch.Records = draft
					_, err := t.PromoteBatch(batch, t.batchApply())
					return err
				},
				Publish: func() { t.publishAcceptedBatch(batch.Result) }}, nil
		},
		Stage: func(base *records.Store, candidateID string, installations []platform.ReleaseInstallation) (releaseowner.StagedInstallation, error) {
			view, err := t.assetInstallationView(base)
			if err != nil {
				return releaseowner.StagedInstallation{}, err
			}
			view.Projection.Current().SelectRelease(candidateID)
			draft, err := view.stageReleaseInstallationLocked(installations, false)
			if err != nil {
				return releaseowner.StagedInstallation{}, err
			}
			return releaseowner.StagedInstallation{Records: draft.Projection,
				Assets:      draft.acceptedAssets(),
				Definitions: func() error { return t.installReleaseDefinitions(draft, installations) }}, nil
		},
		Journaled: func() bool { return t.AcceptResult != nil },
		Prepare: func(e releaseowner.ReleaseEntry) bool {
			if t.preparedResult == nil {
				return false
			}
			*t.preparedResult = preparedAccepted{entry: releaseEntry(e), key: e.Key, hash: e.Hash, uploads: t.preparedResult.uploads}
			return true
		},
		Accept: func(e releaseowner.ReleaseEntry) ([]byte, error) {
			return t.acceptResult(releaseEntry(e), e.Key, e.Hash)
		},
		Applied:    t.acceptedResultApplied,
		Changed:    t.changed,
		Quarantine: t.quarantine,
	}
}

func releaseEntry(e releaseowner.ReleaseEntry) Entry {
	return Entry{App: build.ID, Kind: "accepted-result", Principal: e.Principal, Body: e.Body, At: e.At}
}

func (t *Tenant) SaveReleaseCandidates(m platform.Member, drafts []build.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	return decideWithError(t, "release", func(t *Tenant) (string, error) {
		return t.releaseAcceptance().Save(m, drafts, candidateID, key, now)
	}, t.applyPreparedRelease(m))
}

func (t *Tenant) ActivateRelease(m platform.Member, candidateID, key string, now time.Time) (string, error) {
	return t.ActivateReleaseWithUpgrade(m, candidateID, key, "", now)
}

func (t *Tenant) ActivateReleaseWithUpgrade(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	return decideWithError(t, "release", func(t *Tenant) (string, error) {
		return t.releaseAcceptance().Activate(m, candidateID, key, upgradeID, now)
	}, t.applyPreparedRelease(m))
}

func (t *Tenant) applyPreparedRelease(m platform.Member) func(*Tenant, preparedAccepted) (string, error) {
	return func(t *Tenant, p preparedAccepted) (string, error) {
		return t.releaseAcceptance().ApplyPrepared(m, releaseowner.ReleaseEntry{Principal: p.entry.Principal, Body: p.entry.Body, At: p.entry.At, Key: p.key, Hash: p.hash})
	}
}

func (t *Tenant) acceptedAssets() releaseowner.AcceptedAssets {
	return releaseowner.AcceptedAssets{
		Available: func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) },
		WithBindings: func(roots []platform.AssetRef, available []platform.ReleaseAsset) (platform.ReleaseCandidate, error) {
			return t.candidateWithBindings(roots, available, nil)
		},
		Apps:       func() []platform.App { return t.apps },
		CodeAssets: t.codeReleaseAssetsLocked,
	}
}

func (t *Tenant) runningReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	return releaseowner.RunningRelease(saved, t.acceptedAssets())
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
