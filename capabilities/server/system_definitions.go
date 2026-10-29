package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	releaseowner "platformserver/studio/releases"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/studio/build"

	"platformserver/internal/systemscope"
	"platformserver/platform"
)

func systemDefinitionRefusal(err error) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "System definitions: {reason}", err.Error())
}

func (h hostView) FreezeSystemDefinitions(c platform.Caller, refs []platform.AssetRef, now time.Time) (json.RawMessage, *kernel.Error) {
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System self-hosting is frozen; use static maintenance to reconcile retained work")
}

func (h hostView) PrepareSystemDefinitions(c platform.Caller, raw json.RawMessage, key string, now time.Time) (json.RawMessage, *kernel.Error) {
	if h.t.ID != systemscope.Tenant || c.Tenant != systemscope.Tenant || c.App != systemscope.App {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the system authority activates system definitions")
	}
	// A successful report may finish only its original pre-freeze activation.
	// It cannot turn an old candidate into a new deployment request.
	activationID, original := strings.CutPrefix(key, "system:")
	activationValue, known := h.t.held(systemscope.ActivationType + "/" + activationID)
	var activation struct {
		Release int
		Result  string
	}
	if !original || !known || json.Unmarshal(platform.Raw(activationValue), &activation) != nil || activation.Result != systemscope.StateRequested {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System self-hosting is frozen; only retained activation outcomes may be reconciled")
	}
	releaseValue, known := h.t.held(fmt.Sprintf("%s/%04d", systemscope.ReleaseType, activation.Release))
	var release struct{ Candidate json.RawMessage }
	var retained, frozen platform.SystemDefinitionCandidate
	if !known || json.Unmarshal(platform.Raw(releaseValue), &release) != nil || json.Unmarshal(release.Candidate, &retained) != nil || json.Unmarshal(raw, &frozen) != nil || retained.ID != frozen.ID || !bytes.Equal(retained.Bytes, frozen.Bytes) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The retained activation must use its original system candidate")
	}
	candidate, err := platform.ReadCandidate(frozen.ID, frozen.Bytes)
	if err != nil {
		return nil, systemDefinitionRefusal(err)
	}
	for _, definition := range h.t.definitions {
		if definition.Source == "code" {
			continue
		}
		if !slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == definition.Ref }) {
			return nil, systemDefinitionRefusal(fmt.Errorf("retiring system definition %s needs its original owner's retirement plan", definition.Ref))
		}
	}
	installations, bootstrap, err := h.t.prepareReleaseLifecycleLocked(candidate, c.Member, now, false)
	if err != nil {
		return nil, systemDefinitionRefusal(err)
	}
	saved := acceptedformat.Release{Version: 4, Kind: "release-result", Tenant: h.t.ID, App: build.ID, Member: c.ID, Key: "activate:" + key, At: now.UTC(), CandidateID: candidate.ID, Bytes: candidate.Bytes, Active: true, Installations: installations, Bootstrap: bootstrap}
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(h.t.ID, c.ID, saved.Key, candidate.ID, true)
	if err != nil {
		return nil, systemDefinitionRefusal(err)
	}
	projection, err := h.t.systemDefinitionProjection(saved)
	if err != nil {
		return nil, systemDefinitionRefusal(err)
	}
	if peers := h.t.systemDefinitionState.peers.Load(); peers != nil {
		for _, tenant := range peers.tenants {
			if tenant == h.t {
				continue
			}
			if err := tenant.checkSystemDefinitionProjection(projection); err != nil {
				return nil, systemDefinitionRefusal(fmt.Errorf("tenant %s: %w", tenant.ID, err))
			}
		}
	}
	image, err := acceptedformat.EncodeRelease(saved)
	if err != nil {
		return nil, systemDefinitionRefusal(err)
	}
	return image, nil
}

func (h hostView) ValidateSystemDefinitions(raw json.RawMessage) error {
	saved, err := acceptedformat.DecodeRelease(raw)
	if err != nil {
		return err
	}
	if h.t.ID != systemscope.Tenant || saved.Tenant != h.t.ID || saved.App != build.ID || !saved.Active || !strings.HasPrefix(saved.Key, "activate:system:") {
		return fmt.Errorf("invalid system definition installation")
	}
	return nil
}
func (h hostView) ApplySystemDefinitions(raw json.RawMessage) error {
	if err := h.ValidateSystemDefinitions(raw); err != nil {
		return err
	}
	saved, err := h.t.applyAcceptedRelease(raw)
	if err != nil {
		return err
	}
	projection, err := h.t.systemDefinitionProjection(saved)
	if err != nil {
		return err
	}
	h.t.systemDefinitions.Store(projection)
	return nil
}

// This is a derived immutable runtime view of the system authority's accepted
// release, never another registry, journal, or set of owner metadata rows.
type systemDefinitionProjection struct {
	id            string
	assets        []platform.ReleaseAsset
	installations []platform.ReleaseInstallation
}

type systemDefinitionPeers struct{ tenants []*Tenant }

type systemDefinitionState struct {
	peers     atomic.Pointer[systemDefinitionPeers]
	source    atomic.Pointer[Tenant]
	published atomic.Pointer[systemDefinitionProjection]
	applied   string
}

func (t *Tenant) systemDefinitionProjection(saved acceptedformat.Release) (*systemDefinitionProjection, error) {
	candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
	if err != nil {
		return nil, err
	}
	projection := &systemDefinitionProjection{id: candidate.ID, installations: slices.Clone(saved.Installations)}
	code := map[platform.AssetRef]bool{}
	for _, definition := range t.definitions {
		if definition.Source == "code" {
			code[definition.Ref] = true
		}
	}
	for _, asset := range candidate.Assets {
		if !code[asset.Ref] {
			projection.assets = append(projection.assets, asset)
		}
	}
	return projection, nil
}

// Startup snapshots already retain the original release results. Reconstruct
// the cache from their exact accepted images, without preparing a new release.
func (t *Tenant) restoreSystemDefinitionProjection() error {
	if t.ID != systemscope.Tenant || t.releases.Active() == "" {
		return nil
	}
	for key, raw := range t.committed.AllAnswers() {
		if !strings.HasPrefix(key, "release:activate:system:") {
			continue
		}
		saved, err := acceptedformat.DecodeRelease(raw)
		if err != nil {
			return err
		}
		if saved.CandidateID != t.releases.Active() {
			continue
		}
		projection, err := t.systemDefinitionProjection(saved)
		if err != nil {
			return err
		}
		t.systemDefinitions.Store(projection)
		return nil
	}
	return nil
}

func (h *Host) bindSystemDefinitions(tenants []*Tenant) {
	var system *Tenant
	for _, tenant := range tenants {
		if tenant.ID == systemscope.Tenant {
			system = tenant
			break
		}
	}
	if system == nil {
		return
	}
	system.systemDefinitionState.peers.Store(&systemDefinitionPeers{tenants: slices.Clone(tenants)})
	for _, tenant := range tenants {
		if tenant != system {
			tenant.systemDefinitionState.source.Store(system)
		}
	}
}

func (t *Tenant) applySystemDefinitionProjection() error {
	system := t.systemDefinitionState.source.Load()
	if system == nil {
		return nil
	}
	projection := system.systemDefinitions.Load()
	if projection == nil || projection.id == t.systemDefinitionState.applied {
		return nil
	}
	view, err := t.assetInstallationView(t.records)
	if err != nil {
		return err
	}
	if err := view.installSystemDefinitionProjection(projection, true); err != nil {
		return err
	}
	if err := t.validateAssetStorage(view, false); err != nil {
		return err
	}
	if err := t.anchorReleaseRecords(view.records); err != nil {
		return err
	}
	if err := t.records.PromoteRecords(view.records); err != nil {
		return err
	}
	// Validation ran against private owners. The original owners install the
	// same saved images to extend their original ledger declarations as well.
	if err := t.installSystemDefinitionProjection(projection, false); err != nil {
		return err
	}
	t.systemDefinitionState.published.Store(projection)
	t.systemDefinitionState.applied = projection.id
	return nil
}

func (t *Tenant) installSystemDefinitionProjection(projection *systemDefinitionProjection, private bool) error {
	registry, err := t.newAssetRegistryLocked(true)
	if err != nil {
		return err
	}
	refs := map[platform.AssetRef]bool{}
	for _, asset := range projection.assets {
		refs[asset.Ref] = true
	}
	for _, definition := range t.definitions {
		if refs[definition.Ref] && definition.Source != "system" {
			return fmt.Errorf("system definition %s conflicts with a tenant-owned definition", definition.Ref)
		}
	}
	end, err := registry.BeginBootstrap(projection.assets)
	if err != nil {
		return err
	}
	defer end()
	t.installingSet = true
	defer func() { t.installingSet = false }()
	for _, installation := range projection.installations {
		if installation.Ref == nil || !refs[*installation.Ref] {
			continue
		}
		plan, err := releaseowner.InstallationPlan(registry, installation)
		if err != nil {
			return err
		}
		if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{Private: private}); err != nil {
			return err
		}
		if reader, ok := t.app(plan.Ref.App).(platform.AssetDefinitionReader); ok && (reader.DefinitionMetadataKind(plan.Ref.Kind) || plan.Ref.Kind == platform.AssetAgent) {
			var asset platform.ReleaseAsset
			for _, original := range projection.assets {
				if original.Ref == plan.Ref {
					asset = original
					break
				}
			}
			definition, err := reader.DefinitionDescriptor(asset)
			if err != nil {
				return err
			}
			t.setDefinitions(append(slices.DeleteFunc(slices.Clone(t.definitions), func(d platform.Definition) bool { return d.Ref == definition.Ref }), definition))
		}
	}
	if err := t.validateAssetSet(); err != nil {
		return err
	}
	for i := range t.definitions {
		if refs[t.definitions[i].Ref] {
			t.definitions[i].Source = "system"
			t.definitions[i].OwnerRecord = ""
		}
	}
	t.setDefinitions(t.definitions)
	return nil
}

// A shared declaration is an immutable prerequisite of local releases. Its
// exact accepted descriptor stays with the system owner, like native assets.
func (t *Tenant) sharedSystemAssets(app string) []platform.ReleaseAsset {
	projection := t.systemDefinitionState.published.Load()
	if projection == nil {
		return nil
	}
	var out []platform.ReleaseAsset
	for _, asset := range projection.assets {
		if asset.Ref.App == app {
			out = append(out, asset)
		}
	}
	return out
}

func (h hostView) SystemDefinitionReadOnly(ref platform.AssetRef) bool {
	for _, definition := range h.t.definitions {
		if definition.Ref == ref && definition.Source == "system" {
			return true
		}
	}
	return false
}

// Compatibility checks retain the existing tenant's own rows and grants.
// They prepare only a private runtime and never append a business decision.
func (t *Tenant) checkSystemDefinitionProjection(projection *systemDefinitionProjection) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.recordReadError != nil {
		return t.recordReadError
	}
	view, err := t.assetInstallationView(t.records)
	if err != nil {
		return err
	}
	if err := view.installSystemDefinitionProjection(projection, true); err != nil {
		return err
	}
	return t.validateAssetStorage(view, false)
}
