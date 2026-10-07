package platformserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Candidate physical isolation, cross-environment promotion and real data
// migration (ADR-0047 §11, ordered by the owner on 2026-10-05).
//
// Isolation: a saved release candidate is written once into the tenant's file
// store as a sealed artifact, addressed by its content digest. What is sealed
// cannot be changed behind the release machinery; activation, promotion and
// support reads read the sealed bytes and verify the digest, so a candidate is
// a thing with an address rather than an entry in a mutable map.
//
// Promotion: an environment is another tenant this host serves. Promotion moves
// a sealed candidate from one tenant to another through the target's own
// release result — the same decode, digest, installation and activation path a
// local save uses (ADR-0039) — after checking that the target hosts every app
// the candidate's assets belong to. Nothing is copied by hand and no second
// deployment state exists.
//
// Migration: real records move with the platform's own import/export
// (ADR-0028 D10). An export writes what a member of the source may read; the
// import submits each row through the target's generated actions as a member of
// the target, keyed by the file's hash and the row, so a migration run twice
// repeats nothing. Types the target does not host refuse the whole run up front.

// SealedArtifact is one candidate's sealed bytes.
type SealedArtifact struct {
	Tenant    string    `json:"tenant"`
	Candidate string    `json:"candidate"`
	Key       string    `json:"key"`
	Digest    string    `json:"digest"`
	Size      int       `json:"size"`
	Assets    int       `json:"assets"`
	SealedAt  time.Time `json:"sealedAt"`
}

// PromotionResult says what one promotion did.
type PromotionResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Candidate string `json:"candidate"`
	Digest    string `json:"digest"`
	Active    bool   `json:"active"`
	Key       string `json:"key"`
	Assets    int    `json:"assets"`
}

// MigrationType is one type's part of a migration.
type MigrationType struct {
	Type    string      `json:"type"`
	Rows    int         `json:"rows"`
	Written int         `json:"written"`
	Refused []ImportRow `json:"refused,omitempty"`
}

// MigrationResult is one migration run: which types, how many rows, and the key
// that makes a second run repeat nothing.
type MigrationResult struct {
	From  string          `json:"from"`
	To    string          `json:"to"`
	Key   string          `json:"key"`
	Types []MigrationType `json:"types"`
}

// SealCandidate writes a saved candidate's exact bytes into the file store as a
// sealed artifact and remembers its digest. Sealing is idempotent: the same
// candidate seals to the same key and digest.
func (t *Tenant) SealCandidate(id string, now time.Time) (SealedArtifact, error) {
	t.mu.Lock()
	raw := slices.Clone(t.releases.raw(id))
	t.mu.Unlock()
	if raw == nil {
		return SealedArtifact{}, fmt.Errorf("release candidate %s is not saved", id)
	}
	candidate, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return SealedArtifact{}, fmt.Errorf("release candidate %s: %w", id, err)
	}
	sum := sha256.Sum256(raw)
	artifact := SealedArtifact{Tenant: t.ID, Candidate: candidate.ID, Key: sealedKey(t.ID, candidate.ID),
		Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(raw), Assets: len(candidate.Assets), SealedAt: now.UTC()}
	if err := t.files().Put(context.Background(), artifact.Key, raw, "application/json"); err != nil {
		return SealedArtifact{}, fmt.Errorf("seal %s: %w", id, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.releases.seal(candidate.ID, artifact); err != nil {
		return SealedArtifact{}, fmt.Errorf("sealed candidate %s changed", candidate.ID)
	}
	return artifact, nil
}

func sealedKey(tenant, candidate string) string {
	return "releases/" + tenant + "/" + strings.ReplaceAll(strings.TrimPrefix(candidate, "sha256-v1:"), ":", "-") + ".json"
}

// SealedArtifact reads back one sealed candidate's manifest.
func (t *Tenant) SealedArtifact(id string) (SealedArtifact, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, a := range t.releases.sealed {
		if a.Candidate == id {
			return a, true
		}
	}
	return SealedArtifact{}, false
}

// sealedBytes reads a sealed artifact and checks its digest, so a promoted or
// activated candidate is exactly what was sealed.
func (t *Tenant) sealedBytes(id string) ([]byte, SealedArtifact, error) {
	artifact, ok := t.SealedArtifact(id)
	if !ok {
		return nil, SealedArtifact{}, fmt.Errorf("release candidate %s is not sealed", id)
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

// sealedOrSeal answers a saved candidate's sealed bytes, sealing it first when
// no artifact is on record: the artifact is derived from the committed bytes
// and sealing is idempotent, so a save that never sealed, or a sealed map not
// carried by a journal-only recovery, is repaired here rather than refused.
func (t *Tenant) sealedOrSeal(id string, now time.Time) ([]byte, SealedArtifact, error) {
	if _, ok := t.SealedArtifact(id); !ok {
		if _, err := t.SealCandidate(id, now); err != nil {
			return nil, SealedArtifact{}, err
		}
	}
	return t.sealedBytes(id)
}

// PromoteCandidate moves one sealed candidate from one tenant to another. The
// source must have it saved (it is sealed on the way if it was not); the target must host every app the candidate's
// assets belong to; the member must hold the target's builder or publisher
// role. The bytes enter the target as its own release result, so its journal,
// installations and activation behave exactly as for a local save.
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
	if activate {
		if err := to.materialiseDrafts(candidate, member, now); err != nil {
			return result, err
		}
	}
	to.mu.Lock()
	defer to.mu.Unlock()
	if to.quarantined() {
		return result, fmt.Errorf("tenant %s is quarantined", to.ID)
	}
	if err := to.releases.put(candidate.ID, raw); err != nil {
		return result, fmt.Errorf("candidate %s exists in %s with different bytes", candidate.ID, to.ID)
	}
	var installations []releaseInstallation
	if activate {
		if installations, err = to.prepareReleaseActivationLocked(candidate.ID, raw); err != nil {
			return result, err
		}
	}
	saved := acceptedRelease{Version: 2, Kind: "release-result", Tenant: to.ID, App: build.ID, Member: member.ID,
		Key: "promotion:" + key, At: now.UTC(), CandidateID: candidate.ID, Bytes: slices.Clone(raw), Active: activate, Installations: installations, From: from.ID}
	if saved.RequestHash, err = releaseRequestHash(to.ID, member.ID, saved.Key, candidate.ID, activate); err != nil {
		return result, err
	}
	// The target's audit of the promotion is written where the result is
	// applied, so a replayed journal carries it; the source changes nothing
	// and keeps the sealed artifact as its record.
	if _, err := to.commitReleaseLocked(member, saved); err != nil {
		return result, err
	}
	return result, nil
}

// MigrateRecords moves real records of the named types from one tenant to
// another with the platform's import/export: the source export reads what the
// member may read there, and every row enters the target through its generated
// actions as the target's member. A type the target does not host refuses the
// run before anything moves. The key is derived from the two tenants and the
// types, so the same migration run twice imports nothing new.
func MigrateRecords(from, to *Tenant, source, target platform.Member, types []string, now time.Time) (MigrationResult, error) {
	if from == nil || to == nil || from.ID == to.ID {
		return MigrationResult{}, fmt.Errorf("migration needs two different environments")
	}
	if len(types) == 0 {
		return MigrationResult{}, fmt.Errorf("name at least one type to migrate")
	}
	sorted := slices.Clone(types)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	hosted := to.hostedTypes()
	for _, typ := range sorted {
		if !hosted[typ] {
			return MigrationResult{}, fmt.Errorf("%s does not host %s", to.ID, typ)
		}
	}
	sum := sha256.Sum256([]byte(from.ID + "\x00" + to.ID + "\x00" + strings.Join(sorted, ",")))
	key := "migration:" + hex.EncodeToString(sum[:8])
	result := MigrationResult{From: from.ID, To: to.ID, Key: key, Types: []MigrationType{}}
	for _, typ := range sorted {
		data, refusal := from.Export(source, typ, platform.Query{}, now)
		if refusal != nil {
			return result, fmt.Errorf("export %s from %s: %s", typ, from.ID, refusal.Code)
		}
		rows, refusal := to.Import(target, typ, data, false, now)
		if refusal != nil {
			return result, fmt.Errorf("import %s into %s: %s", typ, to.ID, refusal.Code)
		}
		part := MigrationType{Type: typ, Rows: len(rows)}
		for _, row := range rows {
			if row.Outcome == "ok" {
				part.Written++
			} else {
				part.Refused = append(part.Refused, row)
			}
		}
		result.Types = append(result.Types, part)
	}
	// Each row entered the target as an audited submission; the run itself is
	// the console's migration manifest (rememberMigration), kept with the
	// snapshot like the rest of the console's state. An audit line here
	// would not survive a replay from the journal alone.
	return result, nil
}

// hostedTypes are the entity types this tenant's apps declare.
func (t *Tenant) hostedTypes() map[string]bool {
	out := map[string]bool{}
	for _, et := range t.records.sortedTypes() {
		out[et.info.Type] = true
	}
	return out
}

// MigrationManifest is the console's record of a migration between environments.
type MigrationManifest struct {
	At     time.Time       `json:"at"`
	From   string          `json:"from"`
	To     string          `json:"to"`
	Key    string          `json:"key"`
	Member string          `json:"member"`
	Types  []MigrationType `json:"types"`
}

// rememberMigration keeps the console's own view of what moved.
func (t *Tenant) rememberMigration(result MigrationResult, member string, now time.Time) {
	t.console.addMigration(MigrationManifest{At: now.UTC(), From: result.From, To: result.To, Key: result.Key, Member: member, Types: result.Types})
}

// Migrations lists what the console moved in or out of this tenant.
func (t *Tenant) Migrations() []MigrationManifest { return t.console.migrationList() }

// grantUsable resolves an open support session of this tenant.
func (t *Tenant) grantUsable(id string, now time.Time) (SupportGrant, error) {
	return t.console.usableSupport(t.ID, id, now)
}

// jsonPayload is the console's helper for small JSON answers.
func jsonPayload(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

// materialiseDrafts gives an environment the builder drafts a candidate's
// assets are published onto. Activation writes "published" onto the owning
// draft row (release_install.go); an environment that never authored the
// application has none, so each missing draft is created here through the
// builder's own create action, as the promoting member, with the writable
// fields of the sealed image and a key derived from the candidate (so the
// member needs the builder role there; a publisher activates only what is
// already drafted). The draft is addressed by its asset name, which is what
// the candidate carries; a draft that already exists under that name is left
// as it is, activation reconciles it.
func (t *Tenant) materialiseDrafts(candidate platform.ReleaseCandidate, member platform.Member, now time.Time) error {
	drafts, err := build.DraftsOf(candidate)
	if err != nil {
		return err
	}
	for _, draft := range drafts {
		t.records.mu.Lock()
		et := t.records.types[draft.Type]
		exists := false
		if et != nil {
			nameField, _ := et.info.Field("name")
			for _, row := range et.rows {
				if row.value.FieldByIndex(nameField.Index).String() == draft.Name && !row.value.FieldByName("Record").Interface().(platform.Record).Archived {
					exists = true
					break
				}
			}
		}
		t.records.mu.Unlock()
		if et == nil {
			return fmt.Errorf("%s does not host %s", t.ID, draft.Type)
		}
		if exists {
			continue
		}
		var image map[string]any
		if err := json.Unmarshal(draft.Image, &image); err != nil {
			return err
		}
		payload := map[string]any{}
		for _, f := range et.info.Fields {
			if v, ok := image[f.Name]; ok && !f.ReadOnly && v != nil {
				payload[f.Name] = v
			}
		}
		raw, _ := json.Marshal(payload)
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID,
			IdempotencyKey: "promotion:" + candidate.ID + ":" + member.ID + ":" + draft.Type + "/" + draft.Name,
			Target:         &pb.EntityRef{Type: draft.Type, Id: draft.Name}, Schema: &pb.SchemaRef{Name: draft.Type + ".create", Version: 1}, Payload: raw}
		if _, refusal := t.Submit(member, sub, now); refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
				return fmt.Errorf("%s has no draft for %s/%s; creating it needs the builder role in %s (a publisher can only activate what is already drafted there)", t.ID, draft.Type, draft.Name, t.ID)
			}
			return fmt.Errorf("create draft %s/%s in %s: %s", draft.Type, draft.Name, t.ID, refusal.Code)
		}
	}
	return nil
}
