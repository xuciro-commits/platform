package releases

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"platformserver/platform"
	"strings"
	"time"
)

func SealArtifact(tenant, id string, raw []byte, now time.Time) (platform.SealedArtifact, error) {
	if raw == nil {
		return platform.SealedArtifact{}, fmt.Errorf("release candidate %s is not saved", id)
	}
	candidate, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return platform.SealedArtifact{}, fmt.Errorf("release candidate %s: %w", id, err)
	}
	sum := sha256.Sum256(raw)
	return platform.SealedArtifact{Tenant: tenant, Candidate: candidate.ID, Key: "releases/" + tenant + "/" + strings.ReplaceAll(strings.TrimPrefix(candidate.ID, "sha256-v1:"), ":", "-") + ".json",
		Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(raw), Assets: len(candidate.Assets), SealedAt: now.UTC()}, nil
}

func VerifySealed(id string, artifact platform.SealedArtifact, data []byte) error {
	sum := sha256.Sum256(data)
	if "sha256:"+hex.EncodeToString(sum[:]) != artifact.Digest {
		return fmt.Errorf("sealed candidate %s: bytes differ from the sealed digest", id)
	}
	if _, err := platform.ReadCandidate(id, data); err != nil {
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

func MissingAssetHost(candidate platform.ReleaseCandidate, tenant string, hasApp func(string) bool) error {
	for _, asset := range candidate.Assets {
		if !hasApp(asset.Ref.App) {
			return fmt.Errorf("%s does not host %s, which %s belongs to", tenant, asset.Ref.App, asset.Ref.Name)
		}
	}
	return nil
}
