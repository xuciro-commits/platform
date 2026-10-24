package platformserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/internal/acceptance"
)

// PackageArchive has no paths or install hooks. Native bytes remain inert;
// installing still requires the same deployment-approved driver artifact.
type PackageArchive struct {
	Format     string                   `json:"format"`
	Descriptor PackageDescriptor        `json:"descriptor"`
	Artifacts  []PackageArchiveArtifact `json:"artifacts,omitempty"`
}
type PackageArchiveArtifact struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
	Bytes     []byte `json:"bytes"`
}
type PackageArtifactInfo struct {
	Digest    string `json:"digest"`
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
}

func decodePackageContent(raw []byte) (PackageDescriptor, []PackageArchiveArtifact, error) {
	var header struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return PackageDescriptor{}, nil, err
	}
	if header.Format == "" {
		var descriptor PackageDescriptor
		err := acceptance.DecodeJSON(raw, &descriptor, "platform package")
		return descriptor, nil, err
	}
	var archive PackageArchive
	if err := acceptance.DecodeJSON(raw, &archive, "platform package archive"); err != nil {
		return PackageDescriptor{}, nil, err
	}
	if archive.Format != "platform.package.v1" {
		return PackageDescriptor{}, nil, fmt.Errorf("Unsupported platform package archive version")
	}
	if err := checkPackageArtifacts(archive.Descriptor, archive.Artifacts); err != nil {
		return PackageDescriptor{}, nil, err
	}
	return archive.Descriptor, archive.Artifacts, nil
}

func checkPackageArtifacts(descriptor PackageDescriptor, artifacts []PackageArchiveArtifact) error {
	expected := map[string]bool{}
	if descriptor.Runtime != nil {
		for _, target := range descriptor.Runtime.Targets {
			expected[strings.TrimPrefix(target.Artifact, "sha256:")] = true
		}
	}
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		sum := sha256.Sum256(artifact.Bytes)
		if !expected[strings.TrimPrefix(artifact.Digest, "sha256:")] || seen[strings.TrimPrefix(artifact.Digest, "sha256:")] || artifact.MediaType != "application/octet-stream" || len(artifact.Bytes) == 0 || len(artifact.Bytes) > MaxPackageImportBytes || hex.EncodeToString(sum[:]) != strings.TrimPrefix(artifact.Digest, "sha256:") {
			return fmt.Errorf("Package artifacts need distinct declared driver digests, supported media types and matching bounded bytes")
		}
		seen[strings.TrimPrefix(artifact.Digest, "sha256:")] = true
	}
	return nil
}

func packageArtifactInfo(artifacts []PackageArchiveArtifact) []PackageArtifactInfo {
	result := []PackageArtifactInfo{}
	for _, artifact := range artifacts {
		result = append(result, PackageArtifactInfo{artifact.Digest, artifact.MediaType, len(artifact.Bytes)})
	}
	slices.SortFunc(result, func(a, b PackageArtifactInfo) int { return strings.Compare(a.Digest, b.Digest) })
	return result
}
