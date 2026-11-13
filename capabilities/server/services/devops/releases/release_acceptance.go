package releases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

type AcceptedAssets struct {
	Available    func() ([]platform.ReleaseAsset, error)
	WithBindings func(roots []platform.AssetRef, available []platform.ReleaseAsset) (platform.ReleaseCandidate, error)
	Apps         func() []platform.App
	CodeAssets   func(platform.App) ([]platform.ReleaseAsset, error)
}

func RunningRelease(saved platform.ReleaseCandidate, assets AcceptedAssets) (platform.ReleaseCandidate, error) {
	available, err := assets.Available()
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	roots := make([]platform.AssetRef, 0, len(saved.Assets))
	installed := make(map[platform.AssetRef]bool, len(available))
	for _, asset := range available {
		installed[asset.Ref] = true
	}
	for _, asset := range saved.Assets {
		if installed[asset.Ref] {
			roots = append(roots, asset.Ref)
		}
	}
	if len(roots) == 0 {
		return platform.ReleaseCandidate{}, nil
	}
	return assets.WithBindings(roots, available)
}

func AcceptedInstallationRelease(saved platform.ReleaseCandidate, assets AcceptedAssets) (platform.ReleaseCandidate, error) {
	available, err := assets.Available()
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	code := map[platform.AssetRef]bool{}
	for _, app := range assets.Apps() {
		owned, err := assets.CodeAssets(app)
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		for _, asset := range owned {
			code[asset.Ref] = true
		}
	}
	frozen := map[platform.AssetRef]platform.ReleaseAsset{}
	roots := []platform.AssetRef{}
	for _, asset := range saved.Assets {
		roots = append(roots, asset.Ref)
		if code[asset.Ref] {
			frozen[asset.Ref] = asset
		}
	}
	for index, asset := range available {
		if original, exists := frozen[asset.Ref]; exists {
			available[index] = original
		}
	}
	return assets.WithBindings(roots, available)
}

type PendingWork struct {
	Hosted    func() bool
	Approvals func() []platform.ApprovalRequest
	Saved     func(id string) []byte
}

func PendingWorkFits(candidateID string, raw []byte, work PendingWork) error {
	if !work.Hosted() {
		return nil
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	actionIn := func(c platform.ReleaseCandidate, name string) []byte {
		for _, a := range c.Assets {
			if a.Ref.Kind == platform.AssetAction && a.Ref.Name == name {
				out, _ := json.Marshal(a)
				return out
			}
		}
		return nil
	}
	for _, request := range work.Approvals() {
		next := actionIn(candidate, request.Action)
		if next == nil || request.Release == candidateID {
			continue
		}
		if saved := work.Saved(request.Release); saved != nil {
			started, err := platform.ReadCandidate(request.Release, saved)
			if err == nil && bytes.Equal(actionIn(started, request.Action), next) {
				continue
			}
		}
		return fmt.Errorf("pending approval %s holds %s under another release; decide or withdraw it before activating", request.ID, request.Action)
	}
	return nil
}

func CheckRequest(m platform.Member, candidateID, key string, now time.Time, quarantined func() bool) error {
	if !build.HoldsIndependentRole(m, build.Builder, build.Publisher) {
		return fmt.Errorf("builder or publisher role required")
	}
	if candidateID == "" || key == "" || len(key) > 200 || now.IsZero() {
		return fmt.Errorf("candidate ID and idempotency key are required")
	}
	if quarantined() {
		return fmt.Errorf("tenant is quarantined")
	}
	return nil
}

func ResourceProject(candidate platform.ReleaseCandidate, resources []build.Resource) string {
	projects := func(apps bool) []string {
		out := []string{}
		for _, asset := range candidate.Assets {
			if apps != (asset.Ref.Kind == platform.AssetApp) {
				continue
			}
			for _, resource := range resources {
				if resource.Ref == asset.Ref && resource.Project != "" && !slices.Contains(out, resource.Project) {
					out = append(out, resource.Project)
				}
			}
		}
		return out
	}
	if found := projects(true); len(found) > 0 {
		if len(found) == 1 {
			return found[0]
		}
		return ""
	}
	found := projects(false)
	if len(found) == 1 {
		return found[0]
	}
	return ""
}

type Activation struct {
	UpgradePlan func(candidate platform.ReleaseCandidate) (*ReleaseUpgradePlan, error)
	Lifecycle   func(candidate platform.ReleaseCandidate) ([]platform.ReleaseInstallation, json.RawMessage, error)
}

func PlanActivation(candidateID string, raw []byte, upgradeID string, activation Activation) ([]platform.ReleaseInstallation, json.RawMessage, error) {
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return nil, nil, err
	}
	if upgradeID != "" {
		plan, err := activation.UpgradePlan(candidate)
		if err != nil {
			return nil, nil, err
		}
		if plan == nil || plan.ID != upgradeID {
			return nil, nil, fmt.Errorf("storage upgrade plan changed; refresh and review it again")
		}
	}
	return activation.Lifecycle(candidate)
}

func SavedCandidateBytes(prior []byte, preview func() (ReleasePreview, platform.ReleaseCandidate, error), candidateID string) ([]byte, error) {
	if prior != nil {
		return slices.Clone(prior), nil
	}
	reply, candidate, err := preview()
	if err != nil {
		return nil, err
	}
	if reply.Diagnostic != "" {
		return nil, fmt.Errorf("release candidate: %s", reply.Diagnostic)
	}
	if candidate.ID != candidateID {
		return nil, fmt.Errorf("release candidate changed since preview")
	}
	return candidate.Bytes, nil
}
