package platformserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
	"platformserver/studio/build"
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
	t.mu.Lock()
	raw := slices.Clone(t.releases.Raw(id))
	t.mu.Unlock()
	if raw == nil {
		return platform.SealedArtifact{}, fmt.Errorf("release candidate %s is not saved", id)
	}
	candidate, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("release candidate %s: %w", id, err)
	}
	sum := sha256.Sum256(raw)
	artifact := platform.SealedArtifact{Tenant: t.ID, Candidate: candidate.ID, Key: sealedKey(t.ID, candidate.ID),
		Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(raw), Assets: len(candidate.Assets), SealedAt: now.UTC()}
	if err := t.files().Put(context.Background(), artifact.Key, raw, "application/json"); err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("seal %s: %w", id, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.releases.Seal(candidate.ID, artifact); err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("sealed candidate %s changed", candidate.ID)
	}
	return artifact, nil
}

func sealedKey(tenant, candidate string) string {
	return "releases/" + tenant + "/" + strings.ReplaceAll(strings.TrimPrefix(candidate, "sha256-v1:"), ":", "-") + ".json"
}

func (t *Tenant) SealedArtifact(id string) (platform.SealedArtifact, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, a := range t.releases.Sealed() {
		if a.Candidate == id {
			return a, true
		}
	}
	return platform.SealedArtifact{}, false
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
	sum := sha256.Sum256(buf.Bytes())
	if "sha256:"+hex.EncodeToString(sum[:]) != artifact.Digest {
		return nil, artifact, fmt.Errorf("sealed candidate %s: bytes differ from the sealed digest", id)
	}
	if _, err := platform.ReadCandidate(id, buf.Bytes()); err != nil {
		return nil, artifact, fmt.Errorf("sealed candidate %s: %w", id, err)
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
	if !holdsIndependentBuildRole(member, build.Builder, build.Publisher) {
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
	for _, asset := range candidate.Assets {
		if to.app(asset.Ref.App) == nil {
			return PromotionResult{}, fmt.Errorf("%s does not host %s, which %s belongs to", to.ID, asset.Ref.App, asset.Ref.Name)
		}
	}
	result := PromotionResult{From: from.ID, To: to.ID, Candidate: candidate.ID, Digest: artifact.Digest, Active: activate, Key: key, Assets: len(candidate.Assets)}

	to.mu.Lock()
	defer to.mu.Unlock()
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
}

func MigrateRecords(from, to *Tenant, source, target platform.Member, types []string, now time.Time) (platform.MigrationResult, error) {
	if from == nil || to == nil || from.ID == to.ID {
		return platform.MigrationResult{}, fmt.Errorf("migration needs two different environments")
	}
	if len(types) == 0 {
		return platform.MigrationResult{}, fmt.Errorf("name at least one type to migrate")
	}
	sorted := slices.Clone(types)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	hosted := to.hostedTypes()
	for _, typ := range sorted {
		if !hosted[typ] {
			return platform.MigrationResult{}, fmt.Errorf("%s does not host %s", to.ID, typ)
		}
	}
	sum := sha256.Sum256([]byte(from.ID + "\x00" + to.ID + "\x00" + strings.Join(sorted, ",")))
	key := "migration:" + hex.EncodeToString(sum[:8])
	result := platform.MigrationResult{From: from.ID, To: to.ID, Key: key, Types: []platform.MigrationType{}}
	for _, typ := range sorted {
		data, refusal := from.Export(source, typ, platform.Query{}, now)
		if refusal != nil {
			return result, fmt.Errorf("export %s from %s: %s", typ, from.ID, refusal.Code)
		}
		rows, refusal := to.Import(target, typ, data, false, now)
		if refusal != nil {
			return result, fmt.Errorf("import %s into %s: %s", typ, to.ID, refusal.Code)
		}
		part := platform.MigrationType{Type: typ, Rows: len(rows)}
		for _, row := range rows {
			if row.Outcome == "ok" {
				part.Written++
			} else {
				part.Refused = append(part.Refused, row)
			}
		}
		result.Types = append(result.Types, part)
	}
	return result, nil
}

func (t *Tenant) hostedTypes() map[string]bool {
	out := map[string]bool{}
	for _, et := range t.records.SortedTypes() {
		out[et.Info().Type] = true
	}
	return out
}

func (t *Tenant) rememberMigration(result platform.MigrationResult, member string, now time.Time, evidence ...acceptedformat.AuthorizationEvidence) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.acceptHostControl("migration", member, result, now,
		platform.AuditEntry{Action: "host.migration", Target: result.Key}, func(image *platform.HostControlImage) (platform.SupportGrant, error) {
			image.Migrations = append(image.Migrations, platform.MigrationManifest{At: now.UTC(), From: result.From, To: result.To, Key: result.Key, Member: member, Types: result.Types})
			return platform.SupportGrant{}, nil
		}, evidence...)
	return err
}

func (t *Tenant) Migrations() []platform.MigrationManifest { return t.console.Migrations() }

func (t *Tenant) grantUsable(id string, now time.Time) (platform.SupportGrant, error) {
	return t.console.UsableSupport(t.ID, id, now)
}
