package releases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/publication"

	"strings"
	"time"
)

func SealArtifact(resolver publication.Resolver, tenant, id string, raw []byte, now time.Time) (platform.SealedArtifact, error) {
	if raw == nil {
		return platform.SealedArtifact{}, fmt.Errorf("release candidate %s is not saved", id)
	}
	candidate, err := resolver.ReadCandidate(id, raw)
	if err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("release candidate %s: %w", id, err)
	}
	sum := sha256.Sum256(raw)
	return platform.SealedArtifact{Tenant: tenant, Candidate: candidate.ID, Key: candidateArtifactKey(tenant, candidate.ID),
		Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(raw), Assets: len(candidate.Assets), SealedAt: now.UTC()}, nil
}

type FlowRetention struct {
	Store   *Store
	Files   platform.FileStore
	Tenant  string
	Records *records.Store
}

func (r FlowRetention) Candidate(id string) (protocol.ReleaseCandidate, error) {
	if r.Store.Raw(id) != nil {
		return r.Store.Candidate(id)
	}
	return ReadRetainedCandidate(context.Background(), r.Store.resolver, r.Files, r.Tenant, id)
}

func (r FlowRetention) RetainManaged(ref platform.AssetRef, candidate protocol.ReleaseCandidate) (bool, error) {
	directory := r.Records.Type(build.ResourceType)
	if directory == nil {
		return false, nil
	}
	row := directory.Rows().Get(protocol.ResourceID(r.Tenant, ref))
	if row == nil {
		return false, nil
	}
	resource := row.Value.Interface().(protocol.Resource)
	if resource.ManagedBy == "" || resource.ManagedBy == "platform" {
		return false, nil
	}
	if err := RetainCandidate(context.Background(), r.Store.resolver, r.Files, r.Tenant, candidate); err != nil {
		return false, err
	}
	return true, nil
}

func candidateArtifactKey(tenant, id string) string {
	return "releases/" + tenant + "/" + strings.TrimPrefix(id, "sha256-v1:") + ".json"
}

func RetainCandidate(ctx context.Context, resolver publication.Resolver, files platform.FileStore, tenant string, candidate protocol.ReleaseCandidate) error {
	artifact, err := SealArtifact(resolver, tenant, candidate.ID, candidate.Bytes, time.Time{})
	if err != nil {
		return err
	}
	if files.Exists(ctx, artifact.Key) {
		_, err := ReadRetainedCandidate(ctx, resolver, files, tenant, candidate.ID)
		return err
	}
	return files.Put(ctx, artifact.Key, candidate.Bytes, "application/json")
}

func ReadRetainedCandidate(ctx context.Context, resolver publication.Resolver, files platform.FileStore, tenant, id string) (protocol.ReleaseCandidate, error) {
	digest, ok := strings.CutPrefix(id, "sha256-v1:")
	if decoded, err := hex.DecodeString(digest); !ok || err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
		return protocol.ReleaseCandidate{}, fmt.Errorf("invalid retained release identity")
	}
	reader, size, err := files.Get(ctx, candidateArtifactKey(tenant, id))
	if err != nil {
		return protocol.ReleaseCandidate{}, err
	}
	defer reader.Close()
	if size > 16<<20 {
		return protocol.ReleaseCandidate{}, fmt.Errorf("release candidate exceeds 16 MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, (16<<20)+1))
	if err != nil {
		return protocol.ReleaseCandidate{}, err
	}
	return resolver.ReadCandidate(id, raw)
}

func VerifySealed(resolver publication.Resolver, id string, artifact platform.SealedArtifact, data []byte) error {
	sum := sha256.Sum256(data)
	if "sha256:"+hex.EncodeToString(sum[:]) != artifact.Digest {
		return fmt.Errorf("sealed candidate %s: bytes differ from the sealed digest", id)
	}
	if _, err := resolver.ReadCandidate(id, data); err != nil {
		return fmt.Errorf("sealed candidate %s: %w", id, err)
	}
	return nil
}

func FindSealed(sealed map[string]platform.SealedArtifact, candidate string) (platform.SealedArtifact, bool) {
	for _, artifact := range sealed {
		if artifact.Candidate == candidate {
			return artifact, true
		}
	}
	return platform.SealedArtifact{}, false
}

func MissingAssetHost(candidate protocol.ReleaseCandidate, tenant string, hasApp func(string) bool) error {
	for _, asset := range candidate.Assets {
		if !hasApp(asset.Ref.App) {
			return fmt.Errorf("%s does not host %s, which %s belongs to", tenant, asset.Ref.App, asset.Ref.Name)
		}
	}
	return nil
}
