package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"slices"
	"time"

	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

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
	raw, err := releaseowner.ReadSealed(id, artifact, func() (io.ReadCloser, error) {
		reader, _, err := t.files().Get(context.Background(), artifact.Key)
		return reader, err
	})
	return raw, artifact, err
}

func (t *Tenant) sealedOrSeal(id string, now time.Time) ([]byte, platform.SealedArtifact, error) {
	if _, ok := t.SealedArtifact(id); !ok {
		if _, err := t.SealCandidate(id, now); err != nil {
			return nil, platform.SealedArtifact{}, err
		}
	}
	return t.sealedBytes(id)
}

func PromoteCandidate(from, to *Tenant, candidateID, key string, activate bool, member platform.Member, now time.Time) (releaseowner.PromotionResult, error) {
	if from == nil || to == nil || from.ID == to.ID {
		return releaseowner.PromotionResult{}, fmt.Errorf("promotion needs two different environments")
	}
	if !build.HoldsIndependentRole(member, build.Builder, build.Publisher) {
		return releaseowner.PromotionResult{}, fmt.Errorf("promotion needs the builder or publisher role in %s", to.ID)
	}
	raw, artifact, err := from.sealedOrSeal(candidateID, now)
	if err != nil {
		return releaseowner.PromotionResult{}, err
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return releaseowner.PromotionResult{}, err
	}
	result := releaseowner.PromotionResult{From: from.ID, To: to.ID, Candidate: candidate.ID, Digest: artifact.Digest, Active: activate, Key: key, Assets: len(candidate.Assets)}
	return decideWithError(to, "release:promote", func(to *Tenant) (releaseowner.PromotionResult, error) {
		replay, err := releaseowner.PromotionDestination{
			ID: to.ID, Quarantined: to.quarantined(), HasApp: func(app string) bool { return to.App(app) != nil },
			PriorHash:  to.releases.Request,
			PriorBytes: to.releases.Raw,
			Hash: func() (string, error) {
				return acceptedformat.ReleaseRequestHash(to.ID, member.ID, "promotion:"+key, candidate.ID, activate)
			},
		}.Preflight(candidate, raw, key)
		if err != nil || replay {
			return result, err
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
	}, func(to *Tenant, prepared preparedAccepted) (releaseowner.PromotionResult, error) {
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
	_, err := t.acceptHostAction(controlowner.MigrationAction(result), member, now, func(image *platform.HostControlImage) (platform.SupportGrant, error) {
		controlowner.RecordMigration(image, result, member, now)
		return platform.SupportGrant{}, nil
	}, evidence...)
	return err
}

func (t *Tenant) grantMember(id, missing string, now time.Time) (platform.Member, error) {
	grant, err := t.console.UsableSupport(t.ID, id, now)
	if err != nil {
		return platform.Member{}, err
	}
	member, ok := t.Member(grant.Member)
	if !ok {
		return platform.Member{}, errors.New(missing)
	}
	return member, nil
}
