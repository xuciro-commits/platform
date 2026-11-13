package releases

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

type PromotionRequest struct {
	From        string `json:"from"`
	Candidate   string `json:"candidate"`
	Key         string `json:"key"`
	Activate    bool   `json:"activate"`
	TargetGrant string `json:"targetGrant"`
}

func (r PromotionRequest) Complete() bool { return r.Key != "" && r.TargetGrant != "" }

type PromotionResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Candidate string `json:"candidate"`
	Digest    string `json:"digest"`
	Active    bool   `json:"active"`
	Key       string `json:"key"`
	Assets    int    `json:"assets"`
}

type PromotionSnapshot struct {
	Tenant  string
	Active  string
	Origins map[string]platform.ReleaseOrigin
}

func (s *Store) PromotionSnapshot(tenant string) PromotionSnapshot {
	snapshot := PromotionSnapshot{Tenant: tenant, Active: s.Active(), Origins: map[string]platform.ReleaseOrigin{}}
	for _, id := range s.Ids() {
		if origin, ok := s.Origin(id); ok {
			snapshot.Origins[id] = origin
		}
	}
	return snapshot
}

func PromotionTargets(source, candidate string, snapshots []PromotionSnapshot) []PromotionTarget {
	var out []PromotionTarget
	for _, snapshot := range snapshots {
		if snapshot.Tenant == source {
			continue
		}
		origin, ok := snapshot.Origins[candidate]
		if ok && origin.From == source {
			out = append(out, PromotionTarget{Tenant: snapshot.Tenant, Member: origin.Member, At: origin.At, Active: snapshot.Active == candidate})
		}
	}
	return out
}

func WithPromotions(page ReleasePage, source string, snapshots []PromotionSnapshot) ReleasePage {
	for i := range page.Candidates {
		page.Candidates[i].PromotedTo = PromotionTargets(source, page.Candidates[i].ID, snapshots)
	}
	return page
}

type PromotionDestination struct {
	ID          string
	Quarantined bool
	HasApp      func(string) bool
	PriorHash   func(key string) (string, bool)
	PriorBytes  func(candidate string) json.RawMessage
}

func (t PromotionDestination) Preflight(candidate platform.ReleaseCandidate, raw []byte, key string, hash func() (string, error)) (bool, error) {
	if err := MissingAssetHost(candidate, t.ID, t.HasApp); err != nil {
		return false, err
	}
	if t.Quarantined {
		return false, fmt.Errorf("tenant %s is quarantined", t.ID)
	}
	if prior, ok := t.PriorHash("promotion:" + key); ok {
		hash, err := hash()
		if err != nil || prior != hash {
			return false, fmt.Errorf("promotion key belongs to another request")
		}
		return true, nil
	}
	if prior := t.PriorBytes(candidate.ID); prior != nil && !bytes.Equal(prior, raw) {
		return false, fmt.Errorf("candidate %s exists in %s with different bytes", candidate.ID, t.ID)
	}
	return false, nil
}

func ReadSealed(id string, artifact platform.SealedArtifact, open func() (io.ReadCloser, error)) ([]byte, error) {
	reader, err := open()
	if err != nil {
		return nil, fmt.Errorf("sealed candidate %s: %w", id, err)
	}
	defer reader.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return nil, fmt.Errorf("sealed candidate %s: %w", id, err)
	}
	if err := VerifySealed(id, artifact, buf.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type PromotionPlan struct {
	Result    PromotionResult
	Candidate platform.ReleaseCandidate
	Raw       []byte
	From      string
	Key       string
	Activate  bool
	Member    platform.Member
	At        time.Time
}

func PlanPromotion(from, to, candidateID, key string, activate bool, member platform.Member, now time.Time, sealed func(id string, now time.Time) ([]byte, platform.SealedArtifact, error)) (PromotionPlan, error) {
	if !build.HoldsIndependentRole(member, build.Builder, build.Publisher) {
		return PromotionPlan{}, fmt.Errorf("promotion needs the builder or publisher role in %s", to)
	}
	raw, artifact, err := sealed(candidateID, now)
	if err != nil {
		return PromotionPlan{}, err
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return PromotionPlan{}, err
	}
	return PromotionPlan{
		Result:    PromotionResult{From: from, To: to, Candidate: candidate.ID, Digest: artifact.Digest, Active: activate, Key: key, Assets: len(candidate.Assets)},
		Candidate: candidate, Raw: raw, From: from, Key: key, Activate: activate, Member: member, At: now,
	}, nil
}

func (p PromotionPlan) Prepare(destination PromotionDestination, lifecycle func(platform.ReleaseCandidate, platform.Member, time.Time, bool) ([]platform.ReleaseInstallation, json.RawMessage, error), commit func(acceptedformat.Release) error) (PromotionResult, error) {
	replay, err := destination.Preflight(p.Candidate, p.Raw, p.Key, func() (string, error) {
		return acceptedformat.ReleaseRequestHash(destination.ID, p.Member.ID, "promotion:"+p.Key, p.Candidate.ID, p.Activate)
	})
	if err != nil || replay {
		return p.Result, err
	}
	var installations []platform.ReleaseInstallation
	var bootstrap json.RawMessage
	if p.Activate {
		if installations, bootstrap, err = lifecycle(p.Candidate, p.Member, p.At, false); err != nil {
			return p.Result, err
		}
	}
	saved := acceptedformat.Release{Version: 2, Kind: "release-result", Tenant: destination.ID, App: build.ID, Member: p.Member.ID,
		Key: "promotion:" + p.Key, At: p.At.UTC(), CandidateID: p.Candidate.ID, Bytes: slices.Clone(p.Raw), Active: p.Activate, Installations: installations, Bootstrap: bootstrap, From: p.From}
	if p.Activate {
		saved.Version = 4
	}
	if saved.RequestHash, err = acceptedformat.ReleaseRequestHash(destination.ID, p.Member.ID, saved.Key, p.Candidate.ID, p.Activate); err != nil {
		return p.Result, err
	}
	return p.Result, commit(saved)
}

type Sealer struct {
	Tenant string
	Files  platform.FileStore
	Raw    func(id string) ([]byte, error)
	Commit func(artifact platform.SealedArtifact) error
	Find   func(id string) (platform.SealedArtifact, bool)
}

func (s Sealer) Seal(id string, now time.Time) (platform.SealedArtifact, error) {
	raw, err := s.Raw(id)
	if err != nil {
		return platform.SealedArtifact{}, err
	}
	artifact, err := SealArtifact(s.Tenant, id, raw, now)
	if err != nil {
		return platform.SealedArtifact{}, err
	}
	if err := s.Files.Put(context.Background(), artifact.Key, raw, "application/json"); err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("seal %s: %w", id, err)
	}
	if err := s.Commit(artifact); err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("sealed candidate %s changed", artifact.Candidate)
	}
	return artifact, nil
}

func (s Sealer) SealedOrSeal(id string, now time.Time) ([]byte, platform.SealedArtifact, error) {
	if _, ok := s.Find(id); !ok {
		if _, err := s.Seal(id, now); err != nil {
			return nil, platform.SealedArtifact{}, err
		}
	}
	artifact, ok := s.Find(id)
	if !ok {
		return nil, platform.SealedArtifact{}, fmt.Errorf("release candidate %s is not sealed", id)
	}
	raw, err := ReadSealed(id, artifact, func() (io.ReadCloser, error) {
		reader, _, err := s.Files.Get(context.Background(), artifact.Key)
		return reader, err
	})
	return raw, artifact, err
}
