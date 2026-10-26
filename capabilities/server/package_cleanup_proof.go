package platformserver

import (
	"encoding/json"
	"maps"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	"platformserver/platform"
)

const SchemaPackageCleanupConfirm = "platform.package.cleanup-confirm"

func (d *Console) PackageCleanupState(id string) (string, string, map[string]string) {
	held := d.packagesOf()[id]
	if held == nil {
		return "", "", nil
	}
	return held.State, cleanupGeneration(*held), d.cleanupProofs(id)
}

type packageCleanupProof struct {
	ID          string            `json:"id"`
	Generation  string            `json:"generation"`
	Connections map[string]string `json:"connections"`
}

func cleanupGeneration(p InstalledPackage) string {
	digest, _ := acceptance.Digest([]any{p.ID, p.Version, p.Artifact, p.InstalledAt, p.ChangedAt})
	return digest
}
func cleanupProofKey(tenant, digest string) string {
	return "packages/" + tenant + "/cleanup/" + digest + ".json"
}

func (d *Console) cleanupProofs(id string) map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	proof := d.packageCleanups[id]
	if held := d.packages[id]; held != nil && proof.Generation == cleanupGeneration(*held) {
		return maps.Clone(proof.Connections)
	}
	return nil
}

func (d *Console) decidePackageCleanup(s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	var input struct{ ID, Digest string }
	if json.Unmarshal(s.GetPayload(), &input) != nil || input.ID == "" || !platform.DriverDigest(input.Digest) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a verified cleanup result")
	}
	raw, err := d.host.GetPackageArtifact(cleanupProofKey(d.tenant, input.Digest))
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The cleanup result is unavailable; check resource cleanup again")
	}
	var proof packageCleanupProof
	if err = acceptance.DecodeJSON(raw, &proof, "package cleanup proof"); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid cleanup result")
	}
	digest, _ := acceptance.Digest(proof)
	held := d.packagesOf()[input.ID]
	if proof.ID != input.ID || digest != input.Digest || held == nil || held.State != "draining" || proof.Generation != cleanupGeneration(*held) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package changed after cleanup; check again")
	}
	return func(*pb.ChangeRecord) {
		if d.packageCleanups == nil {
			d.packageCleanups = map[string]packageCleanupProof{}
		}
		d.packageCleanups[input.ID] = proof
	}, nil
}

func (d *Console) RetainedPackageRuntime(id string) (bool, error) {
	held := d.packagesOf()[id]
	if held == nil || held.Artifact == "" {
		return false, nil
	}
	descriptor, err := d.sealedPackageDescriptor(id, held.Version, held.Artifact)
	return descriptor.Runtime != nil, err
}
