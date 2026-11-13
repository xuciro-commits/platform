package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"slices"
	"time"

	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

type PromotionResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Candidate string `json:"candidate"`
	Digest    string `json:"digest"`
	Active    bool   `json:"active"`
	Key       string `json:"key"`
	Assets    int    `json:"assets"`
}

func (t *Tenant) SealCandidate(id string, now time.Time) (platform.SealedArtifact, error) {
	raw, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]byte, *kernel.Error) { return slices.Clone(t.releases.Raw(id)), nil })
	if refusal != nil {
		return platform.SealedArtifact{}, refusal
	}
	artifact, err := releaseowner.SealArtifact(t.ID, id, raw, now)
	if err != nil {
		return platform.SealedArtifact{}, err
	}
	if err := t.files().Put(context.Background(), artifact.Key, raw, "application/json"); err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("seal %s: %w", id, err)
	}
	t.Lane.Lock()
	defer t.Lane.Unlock()
	if err := t.releases.Seal(artifact.Candidate, artifact); err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("sealed candidate %s changed", artifact.Candidate)
	}
	t.StateEpoch.Add(1)
	t.changedOwner(build.ID)
	return artifact, nil
}

func (t *Tenant) SealedArtifact(id string) (platform.SealedArtifact, bool) {
	state := t
	if t.readOrigin == nil {
		if image := t.readImage.Load(); image != nil {
			state = image.state
		} else {
			t.Lane.Lock()
			defer t.Lane.Unlock()
		}
	}
	return releaseowner.FindSealed(state.releases.Sealed(), id)
}

func (t *Tenant) sealedBytes(id string) ([]byte, platform.SealedArtifact, error) {
	artifact, ok := t.SealedArtifact(id)
	if !ok {
		return nil, platform.SealedArtifact{}, fmt.Errorf("release candidate %s is not sealed", id)
	}
	reader, _, err := t.files().Get(context.Background(), artifact.Key)
	if err != nil {
		return nil, artifact, fmt.Errorf("sealed candidate %s: %w", id, err)
	}
	defer reader.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return nil, artifact, fmt.Errorf("sealed candidate %s: %w", id, err)
	}
	if err := releaseowner.VerifySealed(id, artifact, buf.Bytes()); err != nil {
		return nil, artifact, err
	}
	return buf.Bytes(), artifact, nil
}

func (t *Tenant) sealedOrSeal(id string, now time.Time) ([]byte, platform.SealedArtifact, error) {
	if _, ok := t.SealedArtifact(id); !ok {
		if _, err := t.SealCandidate(id, now); err != nil {
			return nil, platform.SealedArtifact{}, err
		}
	}
	return t.sealedBytes(id)
}

func PromoteCandidate(from, to *Tenant, candidateID, key string, activate bool, member platform.Member, now time.Time) (PromotionResult, error) {
	if from == nil || to == nil || from.ID == to.ID {
		return PromotionResult{}, fmt.Errorf("promotion needs two different environments")
	}
	if !build.HoldsIndependentRole(member, build.Builder, build.Publisher) {
		return PromotionResult{}, fmt.Errorf("promotion needs the builder or publisher role in %s", to.ID)
	}
	raw, artifact, err := from.sealedOrSeal(candidateID, now)
	if err != nil {
		return PromotionResult{}, err
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return PromotionResult{}, err
	}
	result := PromotionResult{From: from.ID, To: to.ID, Candidate: candidate.ID, Digest: artifact.Digest, Active: activate, Key: key, Assets: len(candidate.Assets)}
	return decideWithError(to, "release:promote", func(to *Tenant) (PromotionResult, error) {
		if err := releaseowner.MissingAssetHost(candidate, to.ID, func(app string) bool { return to.App(app) != nil }); err != nil {
			return result, err
		}
		if to.quarantined() {
			return result, fmt.Errorf("tenant %s is quarantined", to.ID)
		}
		if prior, ok := to.releases.Request("promotion:" + key); ok {
			hash, hashErr := acceptedformat.ReleaseRequestHash(to.ID, member.ID, "promotion:"+key, candidate.ID, activate)
			if hashErr != nil || prior != hash {
				return result, fmt.Errorf("promotion key belongs to another request")
			}
			return result, nil
		}
		if prior := to.releases.Raw(candidate.ID); prior != nil && !bytes.Equal(prior, raw) {
			return result, fmt.Errorf("candidate %s exists in %s with different bytes", candidate.ID, to.ID)
		}
		var installations []platform.ReleaseInstallation
		var bootstrap json.RawMessage
		if activate {
			if installations, bootstrap, err = to.prepareReleaseLifecycleLocked(candidate, member, now, false); err != nil {
				return result, err
			}
		}
		saved := acceptedformat.Release{Version: 2, Kind: "release-result", Tenant: to.ID, App: build.ID, Member: member.ID,
			Key: "promotion:" + key, At: now.UTC(), CandidateID: candidate.ID, Bytes: slices.Clone(raw), Active: activate, Installations: installations, Bootstrap: bootstrap, From: from.ID}
		if activate {
			saved.Version = 4
		}
		if saved.RequestHash, err = acceptedformat.ReleaseRequestHash(to.ID, member.ID, saved.Key, candidate.ID, activate); err != nil {
			return result, err
		}
		if _, err := to.commitReleaseLocked(member, saved); err != nil {
			return result, err
		}
		return result, nil
	}, func(to *Tenant, prepared preparedAccepted) (PromotionResult, error) {
		if _, err := to.applyPreparedRelease(member, prepared); err != nil {
			return result, err
		}
		return result, nil
	})
}

func MigrateRecords(from, to *Tenant, source, target platform.Member, types []string, now time.Time) (platform.MigrationResult, error) {
	if from == nil || to == nil || from.ID == to.ID {
		return platform.MigrationResult{}, fmt.Errorf("migration needs two different environments")
	}
	return controlowner.MigrateRecords(from.ID, to.ID, types, controlowner.MigrationHooks{
		Hosted: to.hostedTypes,
		Export: func(typ string) ([]byte, *kernel.Error) {
			return from.Export(context.Background(), source, typ, platform.Query{}, now)
		},
		Import: func(typ string, data []byte) ([]platform.ImportRow, *kernel.Error) {
			return to.Import(target, typ, data, false, now)
		},
	})
}

func (t *Tenant) hostedTypes() map[string]bool {
	out := map[string]bool{}
	for _, et := range t.Projection.SortedTypes() {
		out[et.Info().Type] = true
	}
	return out
}

func (t *Tenant) rememberMigration(result platform.MigrationResult, member string, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) error {
	t.Lane.Lock()
	defer t.Lane.Unlock()
	_, err := t.acceptHostControl("migration", member, result, now,
		platform.AuditEntry{Action: "host.migration", Target: result.Key}, func(image *platform.HostControlImage) (platform.SupportGrant, error) {
			image.Migrations = append(image.Migrations, platform.MigrationManifest{At: now.UTC(), From: result.From, To: result.To, Key: result.Key, Member: member, Types: result.Types})
			return platform.SupportGrant{}, nil
		}, evidence...)
	return err
}

func (t *Tenant) grantUsable(id string, now time.Time) (platform.SupportGrant, error) {
	return t.console.UsableSupport(t.ID, id, now)
}
