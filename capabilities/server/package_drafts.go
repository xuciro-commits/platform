package platformserver

import transport "platformserver/kernel/transport"

import releaseowner "platformserver/studio/releases"

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/build"
	"platformserver/studio/packages"
	"time"
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
	h.routesPackagePreparation(rt)
	h.routesPackageSources(rt)
	h.routesComposition(rt)
	rt.authorized(transport.Route{Pattern: "POST /v1/packages/{id}/drafts", Summary: "Create original Build drafts from an installed package; publication and permissions stay separate", Body: PackageDraftRequest{}, Answer: PackageDraftResult{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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

// CreatePackageDrafts reuses the original release bootstrap and composite
// accepted result. It owns only Build decisions, never the Package license.
func (t *Tenant) CreatePackageDrafts(m platform.Member, id, version, key string, at time.Time) (PackageDraftResult, error) {
	var answer PackageDraftResult
	if err := t.admits(m); err != nil {
		return answer, err
	}
	if !holdsIndependentBuildRole(m, build.Builder) {
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
	console, ok := t.app(controlowner.PlatformApp).(platform.BlueprintCatalog)
	if !ok || t.app(build.ID) == nil {
		return answer, fmt.Errorf("this tenant has no package and Build owners")
	}
	roots, assets, err := console.InstalledBlueprint(id, version)
	if err != nil {
		return answer, err
	}
	blueprint := platform.PackageBlueprint{Roots: roots, Assets: assets}
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return answer, fmt.Errorf("this tenant has no Build draft owner")
	}
	code, err := (hostView{t: t}).CodeReleaseAssets()
	if err != nil {
		return answer, err
	}
	candidate, err := packages.BlueprintCandidate(blueprint, owner, code)
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
	// Archived records retain their history. A new explicit creation keeps the
	// logical asset name but uses a request-stable target ID when it is occupied.
	context.DraftIDs = map[platform.AssetRef]string{}
	commandKey := "package-drafts:" + m.ID + ":" + key
	priorEdits := map[platform.AssetRef]CompositeEdit{}
	for batch := 0; ; batch++ {
		batchKey := "composite:" + commandKey
		if batch > 0 {
			batchKey = fmt.Sprintf("%s:%d", batchKey, batch)
		}
		raw := t.committed.CompositeAnswer(batchKey)
		if len(raw) == 0 {
			break
		}
		prior, err := decodeAcceptedComposite(raw)
		if err != nil {
			return answer, err
		}
		for _, edit := range prior.Edits {
			var payload struct{ Name string }
			if err := json.Unmarshal(edit.Payload, &payload); err != nil {
				return answer, err
			}
			found := false
			for _, draft := range drafts {
				if draft.Type == edit.Target.GetType() && draft.Name == payload.Name {
					context.DraftIDs[draft.Ref], priorEdits[draft.Ref] = edit.Target.GetId(), edit
					found = true
				}
			}
			if !found {
				return answer, fmt.Errorf("package draft key belongs to another request")
			}
		}
	}
	createRefs := map[platform.AssetRef]bool{}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(m.ID+"\n"+key)))
	for _, draft := range drafts {
		if _, accepted := priorEdits[draft.Ref]; accepted {
			createRefs[draft.Ref] = true
			continue
		}
		if et := t.records.types[draft.Type]; et != nil {
			for _, row := range et.rows.All() {
				if !row.Value.FieldByName("Record").Interface().(platform.Record).Archived && businessField(et.info, row.Value, "name") == draft.Name {
					context.DraftIDs[draft.Ref] = row.Value.FieldByName("Record").Interface().(platform.Record).ID
				}
			}
			if context.DraftIDs[draft.Ref] != "" {
				continue
			}
			if row := et.rows.Get(draft.Name); row != nil && row.Value.FieldByName("Record").Interface().(platform.Record).Archived {
				context.DraftIDs[draft.Ref] = draft.Name + "-" + digest[:12]
			}
		}
		createRefs[draft.Ref] = true
	}
	if len(context.DraftIDs) > 0 {
		drafts, err = registry.ReleaseDrafts(candidate.Assets, context)
		if err != nil {
			return answer, err
		}
	}
	if len(drafts) == 0 || len(drafts) > build.JointDraftLimit {
		return answer, fmt.Errorf("an application package needs one to %d owner drafts", build.JointDraftLimit)
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
		if et == nil || et.info.App != build.ID {
			return PackageDraftResult{}, fmt.Errorf("draft %s has no Build owner", draft.Ref)
		}
		payload, err := releaseowner.AssetDraftPayload(et.info, draft.Image)
		if err != nil {
			return PackageDraftResult{}, err
		}
		edit := CompositeEdit{Schema: draft.Schema, Target: &pb.EntityRef{Type: draft.Type, Id: draft.Name}, Payload: payload}
		if prior, ok := priorEdits[draft.Ref]; ok && (!sameCompositePayload(prior.Payload, payload) || prior.Target.GetId() != draft.Name) {
			return answer, fmt.Errorf("package draft key belongs to another request")
		}
		if createRefs[draft.Ref] {
			edits = append(edits, edit)
		}
		answer.Drafts = append(answer.Drafts, PackageDraft{Ref: draft.Ref, Type: draft.Type, ID: draft.Name})
		if draft.Ref == primary {
			answer.Application, answer.ApplicationID = primary.Name, draft.Name
		}
	}
	if answer.ApplicationID == "" {
		return PackageDraftResult{}, fmt.Errorf("an application package needs at least one root application draft")
	}
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
		saved := acceptedComposite{Version: 2, Kind: "composite-result", Tenant: t.ID, App: build.ID, Member: m.ID, Key: "composite:" + batchKey, Context: contextHash, At: at.UTC(), Edits: edits[from:until]}
		if contextHash != "" {
			saved.Version = 3
		}
		saved.RequestHash, err = compositeRequestHash(t.ID, m.ID, saved.Key, saved.Edits, saved.Context)
		if err != nil {
			return answer, err
		}
		if prior := t.committed.CompositeAnswer(saved.Key); len(prior) > 0 {
			accepted, err := decodeAcceptedComposite(prior)
			if err != nil || accepted.RequestHash != saved.RequestHash {
				return PackageDraftResult{}, fmt.Errorf("package draft key belongs to another request")
			}
			if _, err = compositeAnswer(accepted); err != nil {
				return PackageDraftResult{}, err
			}
			continue
		}
		decision, changes, err := t.bootstrapOwnerDraftsLocked(candidate, context, false, &releaseowner.OwnerDraftRequest{Key: batchKey, From: from, Until: until, Refs: createRefs})
		if err != nil {
			return PackageDraftResult{}, err
		}
		saved.Changes = changes
		raw, err := encodeAcceptedComposite(saved)
		if err != nil {
			return PackageDraftResult{}, err
		}
		principal, _ := json.Marshal(m)
		committed, err := t.acceptResult(Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: decision.flowVersions()}, saved.Key, saved.RequestHash)
		if err != nil {
			return PackageDraftResult{}, err
		}
		if _, err = t.finishCommittedComposite(m, saved, committed); err != nil {
			return PackageDraftResult{}, err
		}
	}
	return answer, nil
}
