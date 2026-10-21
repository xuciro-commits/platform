package result

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/acceptance"
	"platformserver/internal/construction"
	"platformserver/platform"
	"strings"
	"time"
)

type Release struct {
	Version     int       `json:"version"`
	Kind        string    `json:"kind"`
	Tenant      string    `json:"tenant"`
	App         string    `json:"app"`
	Member      string    `json:"member"`
	Key         string    `json:"key"`
	At          time.Time `json:"at"`
	CandidateID string    `json:"candidateId"`
	Bytes       []byte    `json:"bytes"`
	// Active marks an activation: the tenant's single release pointer moves to
	// this saved candidate. A save leaves the pointer alone.
	Active    bool   `json:"active,omitempty"`
	UpgradeID string `json:"upgradeId,omitempty"`
	// From names the environment a promoted candidate's sealed bytes came
	// from; the target's audit of the promotion is rebuilt from it on replay.
	From          string                `json:"from,omitempty"`
	Installations []ReleaseInstallation `json:"installations,omitempty"`
	Bootstrap     json.RawMessage       `json:"bootstrap,omitempty"`
	RequestHash   string                `json:"requestHash"`
	Digest        string                `json:"digest"`
}

func ReleaseRequestHash(tenant, member, key, id string, active bool, upgrade ...string) (string, error) {
	upgradeID := ""
	if len(upgrade) > 0 {
		upgradeID = upgrade[0]
	}
	return acceptance.Digest(struct {
		Tenant, Member, Key, CandidateID string
		Active                           bool
		UpgradeID                        string `json:"UpgradeID,omitempty"`
	}{tenant, member, key, id, active, upgradeID})
}

func EncodeRelease(saved Release) ([]byte, error) {
	var err error
	saved.Digest, err = DigestRelease(saved)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return nil, err
	}
	_, err = DecodeRelease(raw)
	return raw, err
}

func DigestRelease(saved Release) (string, error) {
	saved.Digest = ""
	return acceptance.Digest(saved)
}

func DecodeRelease(raw []byte) (Release, error) {
	var saved Release
	if len(raw) == 0 || len(raw) > 24<<20 {
		return saved, fmt.Errorf("release result is empty or too large")
	}
	if err := acceptance.DecodeJSON(raw, &saved, "release result"); err != nil {
		return saved, err
	}
	hash, err := ReleaseRequestHash(saved.Tenant, saved.Member, saved.Key, saved.CandidateID, saved.Active, saved.UpgradeID)
	if err != nil || (saved.Version != 1 && saved.Version != 2 && saved.Version != 3 && saved.Version != 4) || saved.Kind != "release-result" || saved.App != construction.ID ||
		saved.Tenant == "" || saved.Member == "" || saved.Key == "" || saved.At.IsZero() ||
		saved.RequestHash != hash {
		return saved, fmt.Errorf("invalid release result identity")
	}
	if saved.Version < 4 && (saved.Version == 3) != (saved.UpgradeID != "") || saved.UpgradeID != "" && !saved.Active {
		return saved, fmt.Errorf("storage upgrade needs an active format-3 result")
	}
	if len(saved.Installations) > 0 && (saved.Version < 2 || !saved.Active) {
		return saved, fmt.Errorf("definition installation needs an active format-2 release result")
	}
	if len(saved.Bootstrap) > 0 {
		if saved.Version != 4 || !saved.Active || len(saved.Installations) == 0 {
			return saved, fmt.Errorf("draft bootstrap needs a complete format-4 activation")
		}
		batch, receipt, err := DecodeBatch(saved.Bootstrap)
		if err != nil || batch.Tenant != saved.Tenant || batch.App != saved.App || !batch.At.Equal(saved.At) || receipt.GetSubmission().GetPrincipalId() != saved.Member {
			return saved, fmt.Errorf("release bootstrap identity differs: %v", err)
		}
		for _, decision := range batch.Decisions {
			r := new(pb.ChangeRecord)
			if protojson.Unmarshal(decision.Receipt, r) != nil {
				return saved, fmt.Errorf("invalid release bootstrap decision")
			}
			sub := r.GetSubmission()
			if sub.GetPrincipalId() != saved.Member || !strings.HasPrefix(sub.GetIdempotencyKey(), "release-bootstrap:"+saved.CandidateID+":"+saved.Member+":") {
				return saved, fmt.Errorf("release bootstrap belongs to another command")
			}
		}
	}
	if saved.Version == 4 {
		candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
		if err != nil {
			return saved, err
		}
		refs := map[platform.AssetRef]bool{}
		for _, asset := range candidate.Assets {
			refs[asset.Ref] = true
		}
		seen := map[platform.AssetRef]bool{}
		records := map[string]bool{}
		for _, installation := range saved.Installations {
			if installation.Ref == nil || !refs[*installation.Ref] || seen[*installation.Ref] || installation.Record == "" || records[installation.Record] {
				return saved, fmt.Errorf("format-4 release has missing, duplicate or unrelated installation identity")
			}
			seen[*installation.Ref] = true
			records[installation.Record] = true
		}
		if len(saved.Bootstrap) > 0 {
			batch, _, _ := DecodeBatch(saved.Bootstrap)
			for _, decision := range batch.Decisions {
				r := new(pb.ChangeRecord)
				_ = protojson.Unmarshal(decision.Receipt, r)
				sub := r.GetSubmission()
				ref := sub.GetTarget().GetType() + "/" + sub.GetTarget().GetId()
				if !records[ref] || sub.GetSchema().GetName() != sub.GetTarget().GetType()+".create" || decision.Publication != nil {
					return saved, fmt.Errorf("release bootstrap is not an original create of its installation metadata")
				}
			}
			for _, row := range batch.Rows {
				if !records[row.Type+"/"+row.ID] {
					return saved, fmt.Errorf("release bootstrap has an unrelated row")
				}
			}
			if len(batch.DirectRows) > 0 || batch.Control != nil {
				return saved, fmt.Errorf("release bootstrap has an unrelated callback or control mutation")
			}
		}
	}
	digest, err := DigestRelease(saved)
	if err != nil || saved.Digest != digest {
		return saved, fmt.Errorf("release result digest differs")
	}
	if _, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes); err != nil {
		return saved, fmt.Errorf("release result candidate: %w", err)
	}
	return saved, nil
}

// InstallationRecord reads the address omitted by the original format-2/3
// Build writer. Those formats used the publication type plus the image's ID;
// the owner still decodes and validates the complete installation on recovery.
func (saved Release) InstallationRecord(installation ReleaseInstallation) (string, error) {
	if installation.Record != "" {
		return installation.Record, nil
	}
	if saved.Version != 2 && saved.Version != 3 {
		return "", fmt.Errorf("release installation record is absent")
	}
	typ, ok := strings.CutSuffix(installation.Schema, ".publish")
	var record platform.Record
	if !ok || typ == "" || json.Unmarshal(installation.Image, &record) != nil || record.ID == "" {
		return "", fmt.Errorf("legacy installation record is invalid")
	}
	return typ + "/" + record.ID, nil
}
