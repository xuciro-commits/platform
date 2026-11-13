package host

import (
	"context"
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

func (t *Tenant) sealer() releaseowner.Sealer {
	return releaseowner.Sealer{
		Tenant: t.ID,
		Raw: func(id string) ([]byte, error) {
			raw, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]byte, *kernel.Error) { return slices.Clone(t.releases.Raw(id)), nil })
			if refusal != nil {
				return nil, refusal
			}
			return raw, nil
		},
		Put: func(key string, raw []byte) error {
			return t.files().Put(context.Background(), key, raw, "application/json")
		},
		Commit: func(artifact platform.SealedArtifact) error {
			t.Lane.Lock()
			defer t.Lane.Unlock()
			if err := t.releases.Seal(artifact.Candidate, artifact); err != nil {
				return err
			}
			t.StateEpoch.Add(1)
			t.changedOwner(build.ID)
			return nil
		},
		Find: t.SealedArtifact,
		Open: func(key string) (io.ReadCloser, error) {
			reader, _, err := t.files().Get(context.Background(), key)
			return reader, err
		},
	}
}

func (t *Tenant) SealCandidate(id string, now time.Time) (platform.SealedArtifact, error) {
	return t.sealer().Seal(id, now)
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

func PromoteCandidate(from, to *Tenant, candidateID, key string, activate bool, member platform.Member, now time.Time) (releaseowner.PromotionResult, error) {
	if from == nil || to == nil || from.ID == to.ID {
		return releaseowner.PromotionResult{}, fmt.Errorf("promotion needs two different environments")
	}
	plan, err := releaseowner.PlanPromotion(from.ID, to.ID, candidateID, key, activate, member, now, from.sealer().SealedOrSeal)
	if err != nil {
		return releaseowner.PromotionResult{}, err
	}
	return decideWithError(to, "release:promote", func(to *Tenant) (releaseowner.PromotionResult, error) {
		return plan.Prepare(releaseowner.PromotionDestination{ID: to.ID, Quarantined: to.quarantined(), HasApp: func(app string) bool { return to.App(app) != nil },
			PriorHash: to.releases.Request, PriorBytes: to.releases.Raw,
		}, to.prepareReleaseLifecycleLocked, func(saved acceptedformat.Release) error {
			_, err := to.releaseAcceptance().Commit(member, saved)
			return err
		})
	}, func(to *Tenant, prepared preparedAccepted) (releaseowner.PromotionResult, error) {
		_, err := to.applyPreparedRelease(member)(to, prepared)
		return plan.Result, err
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
