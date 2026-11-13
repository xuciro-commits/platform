package releases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

type AcceptedBootstrap struct {
	Records *records.Store
	Promote func(records *records.Store) error
	Publish func()
}

type StagedInstallation struct {
	Records     *records.Store
	Assets      AcceptedAssets
	Definitions func() error
}

type ReleaseEntry struct {
	Principal json.RawMessage
	Body      json.RawMessage
	At        time.Time
	Key, Hash string
}

type ReleaseAcceptance struct {
	Tenant      string
	Store       *Store
	Projection  *records.Store
	Builder     func() bool
	Admits      func(platform.Member) *kernel.Error
	Quarantined func() bool
	Answer      func(key string) json.RawMessage
	SaveAnswer  func(key string, raw json.RawMessage)
	Promoted    func(at time.Time, member, target string)
	Enqueue     func(time.Time)
	Preview     func(drafts []build.JointDraftRef, member platform.Member) (ReleasePreview, platform.ReleaseCandidate, error)
	UpgradePlan func(candidate platform.ReleaseCandidate, member platform.Member) (*ReleaseUpgradePlan, error)
	Lifecycle   func(candidate platform.ReleaseCandidate, member platform.Member, at time.Time, upgrade bool) ([]platform.ReleaseInstallation, json.RawMessage, error)
	Bootstrap   func(app string, raw json.RawMessage) (*AcceptedBootstrap, error)
	Stage       func(records *records.Store, candidateID string, installations []platform.ReleaseInstallation) (StagedInstallation, error)
	Journaled   func() bool
	Prepare     func(ReleaseEntry) bool
	Accept      func(ReleaseEntry) ([]byte, error)
	Applied     func()
	Changed     func()
	Quarantine  func(error)
}

func (a ReleaseAcceptance) Apply(raw []byte) (acceptedformat.Release, error) {
	saved, err := acceptedformat.DecodeRelease(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != a.Tenant || !a.Builder() {
		return saved, fmt.Errorf("release result belongs to another tenant or unavailable builder")
	}
	if prior := a.Store.Raw(saved.CandidateID); prior != nil && !bytes.Equal(prior, saved.Bytes) {
		return saved, fmt.Errorf("immutable release candidate %s changed", saved.CandidateID)
	}
	if prior, ok := a.Store.AppliedDigest(saved.Key); ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("release result idempotency key changed")
		}
		return saved, nil
	}
	if len(saved.Installations) > 0 {
		view := a.Projection
		var bootstrap *AcceptedBootstrap
		if len(saved.Bootstrap) > 0 {
			bootstrap, err = a.Bootstrap(saved.App, saved.Bootstrap)
			if err != nil || bootstrap == nil {
				return saved, fmt.Errorf("release bootstrap cannot prepare: %v", err)
			}
			view = bootstrap.Records
		}
		staged, err := a.Stage(view, saved.CandidateID, saved.Installations)
		if err != nil {
			return saved, err
		}
		if err := records.ApplyMediaVersionImages(staged.Records, saved.MediaVersions); err != nil {
			return saved, err
		}
		if saved.Version >= 4 {
			candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
			if err != nil {
				return saved, err
			}
			running, err := AcceptedInstallationRelease(candidate, staged.Assets)
			if err != nil || running.ID != candidate.ID {
				return saved, fmt.Errorf("accepted installation differs from its frozen candidate: %v", err)
			}
		}
		if err := staged.Records.Anchor(a.Projection); err != nil {
			return saved, err
		}
		if bootstrap != nil {
			if err := bootstrap.Promote(staged.Records); err != nil {
				return saved, err
			}
		} else if err := a.Projection.PromoteRecords(staged.Records); err != nil {
			return saved, err
		}
		if err := staged.Definitions(); err != nil {
			return saved, err
		}
		if bootstrap != nil {
			bootstrap.Publish()
			a.Enqueue(saved.At)
		}
	}
	if err := a.Store.Commit(saved.Key, saved.Digest, saved.CandidateID, saved.Bytes, saved.Active); err != nil {
		return saved, err
	}
	if saved.Active {
		a.Projection.Current().SelectRelease(saved.CandidateID)
	}
	a.Store.RememberRequest(saved.Key, saved.RequestHash)
	if saved.Version >= 3 {
		a.SaveAnswer("release:"+saved.Key, raw)
	}
	if saved.From != "" {
		a.Store.RememberOrigin(saved.CandidateID, platform.ReleaseOrigin{From: saved.From, Member: saved.Member, At: saved.At})
		a.Promoted(saved.At, saved.Member, saved.From+"→"+a.Tenant+":"+saved.CandidateID)
	}
	return saved, nil
}

func (a ReleaseAcceptance) Save(m platform.Member, drafts []build.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	if err := a.admitted(m, candidateID, key, now); err != nil {
		return "", err
	}
	candidateBytes, err := SavedCandidateBytes(a.Store.Raw(candidateID), func() (ReleasePreview, platform.ReleaseCandidate, error) {
		return a.Preview(drafts, m)
	}, candidateID)
	if err != nil {
		return "", err
	}
	saved := acceptedformat.Release{Version: 1, Kind: "release-result", Tenant: a.Tenant, App: build.ID,
		Member: m.ID, Key: "release:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: candidateBytes}
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(a.Tenant, m.ID, saved.Key, candidateID, false)
	if err != nil {
		return "", err
	}
	return a.Commit(m, saved)
}

func (a ReleaseAcceptance) Activate(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	if err := a.admitted(m, candidateID, key, now); err != nil {
		return "", err
	}
	if prior := a.Answer("release:activate:" + key); prior != nil {
		saved, err := acceptedformat.DecodeRelease(prior)
		hash, hashErr := acceptedformat.ReleaseRequestHash(a.Tenant, m.ID, "activate:"+key, candidateID, true, upgradeID)
		if err != nil || hashErr != nil || hash != saved.RequestHash {
			return "", fmt.Errorf("activation key belongs to another request")
		}
		return saved.CandidateID, nil
	}
	raw := a.Store.Raw(candidateID)
	if raw == nil {
		return "", fmt.Errorf("release candidate %s is not saved", candidateID)
	}
	installations, bootstrap, err := PlanActivation(candidateID, raw, upgradeID, Activation{
		UpgradePlan: func(candidate platform.ReleaseCandidate) (*ReleaseUpgradePlan, error) {
			return a.UpgradePlan(candidate, m)
		},
		Lifecycle: func(candidate platform.ReleaseCandidate) ([]platform.ReleaseInstallation, json.RawMessage, error) {
			return a.Lifecycle(candidate, m, now, upgradeID != "")
		},
	})
	if err != nil {
		return "", err
	}
	saved := acceptedformat.Release{Version: 1, Kind: "release-result", Tenant: a.Tenant, App: build.ID,
		Member: m.ID, Key: "activate:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: slices.Clone(raw), Active: true, Installations: installations, Bootstrap: bootstrap}
	if len(installations) > 0 {
		saved.Version = 4
	}
	if upgradeID != "" {
		saved.Version, saved.UpgradeID = 4, upgradeID
	}
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(a.Tenant, m.ID, saved.Key, candidateID, true, upgradeID)
	if err != nil {
		return "", err
	}
	return a.Commit(m, saved)
}

func (a ReleaseAcceptance) admitted(m platform.Member, candidateID, key string, now time.Time) error {
	if refusal := a.Admits(m); refusal != nil {
		return refusal
	}
	return CheckRequest(m, candidateID, key, now, a.Quarantined)
}

func (a ReleaseAcceptance) Commit(m platform.Member, saved acceptedformat.Release) (string, error) {
	candidateID := saved.CandidateID
	if saved.Active {
		snapshots, err := records.PrepareMediaVersionImages(a.Projection, saved.Installations)
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
	entry := ReleaseEntry{Principal: principal, Body: raw, At: saved.At, Key: saved.Key, Hash: saved.RequestHash}
	if prior, ok := a.Store.Request(saved.Key); ok {
		if prior != saved.RequestHash {
			return "", fmt.Errorf("release key %s belongs to another request", saved.Key)
		}
		return candidateID, nil
	}
	if _, applied := a.Store.AppliedDigest(saved.Key); applied && !a.Journaled() {
		return "", fmt.Errorf("historical release request needs its journal receipt")
	}
	if a.Prepare(entry) {
		return candidateID, nil
	}
	committed, err := a.Accept(entry)
	if err != nil {
		return "", err
	}
	return a.finish(m, saved, committed)
}

func (a ReleaseAcceptance) ApplyPrepared(m platform.Member, entry ReleaseEntry) (string, error) {
	saved, err := acceptedformat.DecodeRelease(entry.Body)
	if err != nil {
		return "", err
	}
	committed, err := a.Accept(entry)
	if err != nil {
		return "", err
	}
	return a.finish(m, saved, committed)
}

func (a ReleaseAcceptance) finish(m platform.Member, saved acceptedformat.Release, committed []byte) (id string, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("apply committed release: %v", failure)
			a.Quarantine(err)
			id = ""
		}
	}()
	candidateID := saved.CandidateID
	applied, err := a.Apply(committed)
	if err != nil || applied.Key != saved.Key || applied.RequestHash != saved.RequestHash ||
		applied.Member != m.ID || applied.CandidateID != candidateID {
		a.Quarantine(fmt.Errorf("committed release result differs: %v", err))
		return "", fmt.Errorf("committed release result differs: %v", err)
	}
	a.Applied()
	a.Changed()
	return applied.CandidateID, nil
}
