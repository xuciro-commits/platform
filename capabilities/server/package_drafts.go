package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

type PackageDraftRequest struct {
	Version string `json:"version"`
	Key     string `json:"key"`
}
type PackageDraft struct {
	Ref  platform.AssetRef `json:"ref"`
	Type string            `json:"type"`
	ID   string            `json:"id"`
}
type PackageDraftResult struct {
	Package       string         `json:"package"`
	Version       string         `json:"version"`
	Key           string         `json:"key"`
	Application   string         `json:"application"`
	ApplicationID string         `json:"applicationID"`
	Drafts        []PackageDraft `json:"drafts"`
}

func (h *Host) routesPackageDrafts(rt *routes) {
	h.routesPackageSources(rt)
	h.routesComposition(rt)
	rt.authorized(Route{Pattern: "POST /v1/packages/{id}/drafts", Summary: "Create original Build drafts from an installed package; publication and permissions stay separate", Body: PackageDraftRequest{}, Answer: PackageDraftResult{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request PackageDraftRequest
		if !decodeReleaseBody(w, r, &request) {
			return
		}
		answer, err := t.CreatePackageDrafts(m, r.PathValue("id"), request.Version, request.Key, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
}

func (d *Console) packageBlueprint(id, version string) (PackageBlueprint, error) {
	d.mu.Lock()
	held := d.packages[id]
	artifact := ""
	if held != nil && held.State == "active" && held.Version == version {
		artifact = held.Artifact
	}
	d.mu.Unlock()
	if artifact == "" {
		return PackageBlueprint{}, fmt.Errorf("package %s@%s is not the installed active version", id, version)
	}
	descriptor, err := d.sealedPackageDescriptor(id, version, artifact)
	if err != nil {
		return PackageBlueprint{}, err
	}
	if descriptor.Blueprint == nil {
		return PackageBlueprint{}, fmt.Errorf("package %s@%s has no application blueprint", id, version)
	}
	return *descriptor.Blueprint, nil
}

func (d *Console) InstalledBlueprint(id, version string) ([]platform.AssetRef, []platform.ReleaseAsset, error) {
	blueprint, err := d.packageBlueprint(id, version)
	return blueprint.Roots, blueprint.Assets, err
}

var _ host.BlueprintCatalog = (*Console)(nil)

// CreatePackageDrafts reuses the original release bootstrap and composite
// accepted result. It owns only Build decisions, never the Package license.
func (t *Tenant) CreatePackageDrafts(m platform.Member, id, version, key string, at time.Time) (PackageDraftResult, error) {
	var answer PackageDraftResult
	if err := t.admits(m); err != nil {
		return answer, err
	}
	if !holdsIndependentBuildRole(m, construction.Builder) {
		return answer, fmt.Errorf("builder role required")
	}
	if id == "" || version == "" || key == "" || len(key) > 128 || at.IsZero() {
		return answer, fmt.Errorf("package, exact version and idempotency key are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return answer, fmt.Errorf("tenant is quarantined")
	}
	console, ok := t.app(PlatformApp).(host.BlueprintCatalog)
	if !ok || t.app(construction.ID) == nil {
		return answer, fmt.Errorf("this tenant has no package and Build owners")
	}
	roots, assets, err := console.InstalledBlueprint(id, version)
	if err != nil {
		return answer, err
	}
	blueprint := PackageBlueprint{Roots: roots, Assets: assets}
	owner, ok := t.app(construction.ID).(host.ConstructionDrafts)
	if !ok {
		return answer, fmt.Errorf("this tenant has no Build draft owner")
	}
	code, err := (hostView{t: t}).CodeReleaseAssets()
	if err != nil {
		return answer, err
	}
	candidate, err := blueprint.candidate(owner, code)
	if err != nil {
		return answer, err
	}
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return answer, err
	}
	context := platform.AssetReleaseContext{Member: m, At: at, DraftsOnly: true}
	drafts, err := registry.ReleaseDrafts(candidate.Assets, context)
	if err != nil {
		return answer, err
	}
	if len(drafts) == 0 || len(drafts) > construction.JointDraftLimit {
		return answer, fmt.Errorf("an application package needs one to %d owner drafts", construction.JointDraftLimit)
	}
	edits := make([]CompositeEdit, 0, len(drafts))
	answer = PackageDraftResult{Package: id, Version: version, Key: key, Drafts: make([]PackageDraft, 0, len(drafts))}
	var primary platform.AssetRef
	for _, root := range blueprint.Roots {
		if root.Kind == platform.AssetApp {
			primary = root
			break
		}
	}
	for _, draft := range drafts {
		et := t.records.types[draft.Type]
		if et == nil || et.info.App != construction.ID {
			return PackageDraftResult{}, fmt.Errorf("draft %s has no Build owner", draft.Ref)
		}
		payload, err := assetDraftPayload(et.info, draft.Image)
		if err != nil {
			return PackageDraftResult{}, err
		}
		edits = append(edits, CompositeEdit{Schema: draft.Schema, Target: &pb.EntityRef{Type: draft.Type, Id: draft.Name}, Payload: payload})
		answer.Drafts = append(answer.Drafts, PackageDraft{Ref: draft.Ref, Type: draft.Type, ID: draft.Name})
		if draft.Ref == primary {
			answer.Application, answer.ApplicationID = primary.Name, draft.Name
		}
	}
	if answer.ApplicationID == "" {
		return PackageDraftResult{}, fmt.Errorf("an application package needs at least one root application draft")
	}
	commandKey := "package-drafts:" + m.ID + ":" + key
	contextHash := ""
	if len(edits) > 64 {
		contextHash, err = compositeRequestHash(t.ID, m.ID, commandKey, edits)
		if err != nil {
			return answer, err
		}
	}
	for from := 0; from < len(edits); from += 64 {
		until := min(from+64, len(edits))
		batchKey := commandKey
		if from > 0 {
			batchKey = fmt.Sprintf("%s:%d", commandKey, from/64)
		}
		saved := acceptedComposite{Version: 2, Kind: "composite-result", Tenant: t.ID, App: construction.ID, Member: m.ID, Key: "composite:" + batchKey, Context: contextHash, At: at.UTC(), Edits: edits[from:until]}
		if contextHash != "" {
			saved.Version = 3
		}
		saved.RequestHash, err = compositeRequestHash(t.ID, m.ID, saved.Key, saved.Edits, saved.Context)
		if err != nil {
			return answer, err
		}
		if prior := t.committed.compositeAnswer(saved.Key); len(prior) > 0 {
			accepted, err := decodeAcceptedComposite(prior)
			if err != nil || accepted.RequestHash != saved.RequestHash {
				return PackageDraftResult{}, fmt.Errorf("package draft key belongs to another request")
			}
			if _, err = compositeAnswer(accepted); err != nil {
				return PackageDraftResult{}, err
			}
			continue
		}
		_, changes, err := t.bootstrapOwnerDraftsLocked(candidate, context, false, &ownerDraftRequest{Key: batchKey, From: from, Until: until})
		if err != nil {
			return PackageDraftResult{}, err
		}
		saved.Changes = changes
		raw, err := encodeAcceptedComposite(saved)
		if err != nil {
			return PackageDraftResult{}, err
		}
		principal, _ := json.Marshal(m)
		committed, err := t.acceptResult(Entry{App: construction.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: at}, saved.Key, saved.RequestHash)
		if err != nil {
			return PackageDraftResult{}, err
		}
		if _, err = t.finishCommittedComposite(m, saved, committed); err != nil {
			return PackageDraftResult{}, err
		}
	}
	return answer, nil
}
