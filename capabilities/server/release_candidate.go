package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/apps/build"
	"platformserver/platform"
)

// releaseCandidateLocked constructs a read-only release from actual owner
// declarations. It never uses Definitions(member): that discovery surface
// removes inaccessible fields and actions, so its bytes are not a contract.
// The caller holds the tenant lock while collecting builder and code assets.
func (t *Tenant) ReleaseCandidate(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.releaseCandidateLocked(roots)
}

func (t *Tenant) releaseCandidateLocked(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	return t.candidateWithBindings(roots, available, roots)
}

func (t *Tenant) releaseAssetsLocked(builderAssets []platform.ReleaseAsset, replaceBuilder bool) ([]platform.ReleaseAsset, error) {
	var available []platform.ReleaseAsset
	for _, app := range t.apps {
		manifest := app.Manifest()
		if source, ok := app.(interface {
			ReleaseAssets() ([]platform.ReleaseAsset, error)
		}); ok {
			if replaceBuilder && manifest.ID == build.ID {
				available = append(available, builderAssets...)
			} else {
				owned, err := source.ReleaseAssets()
				if err != nil {
					return nil, fmt.Errorf("%s release assets: %w", manifest.ID, err)
				}
				available = append(available, owned...)
			}
		}
		for _, def := range t.definitions {
			if def.Ref.App != manifest.ID || def.Source != "code" {
				continue
			}
			switch def.Ref.Kind {
			case platform.AssetObject:
				i := slices.IndexFunc(manifest.Entities, func(e platform.Entity) bool { return e.Type == def.Ref.Name })
				if i < 0 || def.Entity == nil {
					return nil, fmt.Errorf("code object %s has no owner declaration", def.Ref)
				}
				body, err := json.Marshal(codeObjectDescriptor(manifest.Entities[i], *def.Entity))
				if err != nil {
					return nil, fmt.Errorf("encode code object %s: %w", def.Ref, err)
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
					SourceVersion: manifest.Version, Requires: def.Requires, Body: body})
			case platform.AssetAction:
				action, ok := manifest.Actions.Action(def.Ref.Name)
				if !ok {
					return nil, fmt.Errorf("code action %s has no owner declaration", def.Ref)
				}
				body, err := json.Marshal(codeActionDescriptor(action))
				if err != nil {
					return nil, fmt.Errorf("encode code action %s: %w", def.Ref, err)
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
					SourceVersion: manifest.Version, Requires: def.Requires, Body: body})
			case platform.AssetPropertyType:
				if def.PropertyType == nil {
					return nil, fmt.Errorf("property has no descriptor")
				}
				body, err := json.Marshal(def.PropertyType)
				if err != nil {
					return nil, err
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, SourceVersion: manifest.Version, ContractVersion: def.ContractVersion, Body: body})
			case platform.AssetLinkType:
				if def.LinkType == nil {
					return nil, fmt.Errorf("code link type has no descriptor")
				}
				body, err := json.Marshal(def.LinkType)
				if err != nil {
					return nil, err
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, SourceVersion: manifest.Version, ContractVersion: def.ContractVersion, Requires: def.Requires, Body: body})
			case platform.AssetQuery:
				if def.Query == nil {
					return nil, fmt.Errorf("code query %s has no owner declaration", def.Ref)
				}
				body, err := json.Marshal(def.Query)
				if err != nil {
					return nil, fmt.Errorf("encode code query %s: %w", def.Ref, err)
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
					SourceVersion: manifest.Version, Requires: def.Requires, Body: body})
			case platform.AssetFunction:
				if def.Function == nil {
					return nil, fmt.Errorf("code function %s has no owner declaration", def.Ref)
				}
				body, err := json.Marshal(def.Function)
				if err != nil {
					return nil, err
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
					SourceVersion: manifest.Version, Requires: def.Requires, Body: body})
			case platform.AssetCompute:
				if def.Operation == nil {
					return nil, fmt.Errorf("compute %s has no owner declaration", def.Ref)
				}
				body, err := json.Marshal(def.Operation)
				if err != nil {
					return nil, err
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion, SourceVersion: manifest.Version, Requires: def.Requires, Body: body})
			case platform.AssetPage:
				if def.Page == nil {
					return nil, fmt.Errorf("code page %s has no owner declaration", def.Ref)
				}
				asset, err := platform.PageReleaseAsset(manifest.ID, manifest.Version, *def.Page)
				if err != nil {
					return nil, err
				}
				available = append(available, asset)
			}
		}
		if len(manifest.Pages) > 0 {
			pages := make([]string, len(manifest.Pages))
			for i, page := range manifest.Pages {
				pages[i] = page.Name
			}
			asset, err := platform.ApplicationReleaseAsset(manifest.ID, manifest.Version,
				platform.Application{Name: manifest.ID, Title: manifest.Title, Pages: pages})
			if err != nil {
				return nil, err
			}
			available = append(available, asset)
		}
	}
	return available, nil
}

// A changed object or page can affect a page or application above it. Include
// those owners in the preview roots so its ID and diff describe the actual
// impacted closure, not just the object picked in the editor.
func dependentRoots(ref platform.AssetRef, available []platform.ReleaseAsset) []platform.AssetRef {
	roots := []platform.AssetRef{ref}
	seen := map[platform.AssetRef]bool{ref: true}
	for changed := true; changed; {
		changed = false
		for _, asset := range available {
			if seen[asset.Ref] {
				continue
			}
			if slices.ContainsFunc(asset.Requires, func(dep platform.AssetRef) bool { return seen[dep] }) {
				seen[asset.Ref] = true
				roots = append(roots, asset.Ref)
				changed = true
			}
		}
	}
	return roots
}

func (t *Tenant) previewCandidateLocked(ref platform.AssetRef, builderAssets []platform.ReleaseAsset, additionalRoots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(builderAssets, true)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	roots := dependentRoots(ref, available)
	for _, other := range additionalRoots {
		if slices.ContainsFunc(available, func(a platform.ReleaseAsset) bool { return a.Ref == other }) &&
			!slices.Contains(roots, other) {
			roots = append(roots, other)
		}
	}
	return t.candidateWithBindings(roots, available, []platform.AssetRef{ref})
}

// ReleasePreview is a builder-only comparison between the installed
// development definition and one saved draft. The IDs are candidate IDs,
// not published versions or an active release pointer.
type ReleasePreview struct {
	CurrentID   string              `json:"currentId,omitempty"`
	CandidateID string              `json:"candidateId,omitempty"`
	Included    []platform.AssetRef `json:"included"`
	Added       []platform.AssetRef `json:"added"`
	Removed     []platform.AssetRef `json:"removed"`
	Changed     []platform.AssetRef `json:"changed"`
	Diagnostic  string              `json:"diagnostic,omitempty"`
	// CandidateActions are owner-compiled draft inputs for builder test forms,
	// not the installed member catalog or permission to execute.
	CandidateActions []platform.Action `json:"candidateActions"`
}

type ReleasePreviewRequest struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}

type ReleaseSaveRequest struct {
	Kind        platform.AssetKind `json:"kind"`
	ID          string             `json:"id"`
	CandidateID string             `json:"candidateId"`
	Key         string             `json:"key"`
}

type ReleaseSaved struct {
	ID string `json:"id"`
}

// ReleaseActivateRequest moves the tenant's release pointer to a saved candidate.
type ReleaseActivateRequest struct {
	CandidateID string `json:"candidateId"`
	Key         string `json:"key"`
}

// ReleaseActive is the tenant's active release, empty before any activation.
type ReleaseActive struct {
	ID string `json:"id"`
}

// PreviewRelease checks authority before looking up the owner's unfiltered
// records and before computing any digest or error path. It never changes the
// installed definitions, persistent records, or active operator work.
func (t *Tenant) PreviewRelease(m platform.Member, kind platform.AssetKind, id string) (ReleasePreview, error) {
	if err := t.admits(m); err != nil {
		return ReleasePreview{}, err
	}
	if m.Roles[build.ID] != build.Builder {
		return ReleasePreview{}, fmt.Errorf("builder role required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	reply, candidate, err := t.previewReleaseLocked(kind, id)
	reply.CandidateActions = []platform.Action{}
	if err != nil {
		return reply, err
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != build.ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object build.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return ReleasePreview{}, fmt.Errorf("candidate object %s: %w", asset.Ref, err)
		}
		// A function/flow candidate retains its source object, but may not
		// retain its create/edit action assets. Ask the original owner for
		// those test inputs using that exact object image, not the live model.
		for _, action := range platform.EntityActions(build.Entity(object)) {
			if action.Payload == nil {
				action.Payload = []platform.Field{}
			}
			reply.CandidateActions = append(reply.CandidateActions, action)
		}
	}
	return reply, nil
}

// The candidate returned here is exactly what the builder saw in the
// comparison. A later save must recompute it under the tenant lock and reject
// a stale candidate ID rather than trusting bytes supplied by the browser.
func (t *Tenant) previewReleaseLocked(kind platform.AssetKind, id string) (ReleasePreview, platform.ReleaseCandidate, error) {
	owner, ok := t.app(build.ID).(*build.Build)
	if !ok {
		return ReleasePreview{}, platform.ReleaseCandidate{}, fmt.Errorf("tenant has no builder")
	}
	before, after, oldRoot, newRoot, hadPrior, diagnostic := owner.DraftReleaseAssets(kind, id)
	reply := ReleasePreview{CandidateActions: []platform.Action{}, Included: []platform.AssetRef{}, Added: []platform.AssetRef{}, Removed: []platform.AssetRef{}, Changed: []platform.AssetRef{}}
	var current platform.ReleaseCandidate
	var oldOwners []platform.AssetRef
	if hadPrior {
		var err error
		available, err := t.releaseAssetsLocked(before, true)
		if err != nil {
			return ReleasePreview{}, platform.ReleaseCandidate{}, err
		}
		oldOwners = dependentRoots(oldRoot, available)
		current, err = t.candidateWithBindings(oldOwners, available, []platform.AssetRef{oldRoot})
		if err != nil {
			return ReleasePreview{}, platform.ReleaseCandidate{}, fmt.Errorf("installed definition: %w", err)
		}
		reply.CurrentID = current.ID
		// Renames must still check surviving pages/apps that pointed at the
		// old identity. Removed generated assets are not roots of the new
		// release, but a surviving dependent must be closed or diagnosed.
		oldOwners = slices.DeleteFunc(oldOwners, func(ref platform.AssetRef) bool { return ref == oldRoot && ref != newRoot })
	}
	if diagnostic != nil {
		reply.Diagnostic = diagnostic.Error()
		return reply, platform.ReleaseCandidate{}, nil
	}
	candidate, err := t.previewCandidateLocked(newRoot, after, oldOwners)
	if err != nil {
		reply.Diagnostic = err.Error()
		return reply, platform.ReleaseCandidate{}, nil
	}
	reply.CandidateID = candidate.ID
	for _, asset := range candidate.Assets {
		reply.Included = append(reply.Included, asset.Ref)
	}
	if !hadPrior {
		for _, asset := range candidate.Assets {
			reply.Added = append(reply.Added, asset.Ref)
		}
		return reply, candidate, nil
	}
	reply.Added, reply.Removed, reply.Changed, err = platform.CandidateDiff(current, candidate)
	return reply, candidate, err
}

// Code behavior is pinned by Manifest.Version rather than serialized Go
// callbacks (ADR-0039 D3). This body preserves the complete declarative
// security and lifecycle contract that JSON discovery intentionally omits.
func codeObjectDescriptor(entity platform.Entity, info platform.EntityInfo) any {
	type scope struct {
		Levels       map[string]string `json:"levels"`
		Default      string            `json:"default"`
		Structure    string            `json:"structure"`
		Unit         string            `json:"unit"`
		Owner        string            `json:"owner"`
		Participants bool              `json:"participants"`
		Through      bool              `json:"through"`
	}
	type transition struct {
		Name, Title, Description string
		From, To                 []string
		Roles                    []string
		Payload                  []platform.Field
		Capability               string
		Approval                 any
		Do, After                bool
	}
	type lifecycle struct {
		Field, Initial string
		States         []platform.State
		Transitions    []transition
	}
	var steps *lifecycle
	if entity.Lifecycle != nil {
		steps = &lifecycle{Field: entity.Lifecycle.Field, Initial: entity.Lifecycle.Initial, States: entity.Lifecycle.States}
		for _, step := range entity.Lifecycle.Transitions {
			steps.Transitions = append(steps.Transitions, transition{
				Name: step.Name, Title: step.Title, Description: step.Description, From: step.From, To: step.To,
				Roles: step.Roles, Payload: step.Payload, Capability: step.Capability,
				Approval: codeApprovalDescriptor(step.Approval), Do: step.Do != nil, After: step.After != nil,
			})
		}
	}
	return struct {
		Type      string                `json:"type"`
		Entity    platform.EntityInfo   `json:"entity"`
		Scope     scope                 `json:"scope"`
		Standard  platform.Standard     `json:"standard"`
		Derived   []platform.Derivation `json:"derived"`
		Withheld  string                `json:"withheld"`
		Lifecycle *lifecycle            `json:"lifecycle"`
	}{
		Type: info.Type, Entity: info,
		Scope: scope{Levels: entity.Scope.Levels, Default: entity.Scope.Default, Structure: entity.Scope.Structure,
			Unit: entity.Scope.Unit, Owner: entity.Scope.Owner, Participants: entity.Scope.Participants != nil,
			Through: entity.Scope.Through != nil},
		Standard: entity.Standard, Derived: entity.Derived, Withheld: entity.Withheld, Lifecycle: steps,
	}
}

func codeActionDescriptor(action platform.Action) any {
	return struct {
		Schema   string          `json:"schema"`
		Target   string          `json:"target"`
		Action   platform.Action `json:"action"`
		Roles    []string        `json:"roles"`
		Approval any             `json:"approval"`
	}{Schema: action.Schema, Target: action.Target, Action: action, Roles: action.Roles,
		Approval: codeApprovalDescriptor(action.Approval)}
}

func codeApprovalDescriptor(approval *platform.Approval) any {
	if approval == nil {
		return nil
	}
	type level struct {
		Title, Structure, Role, AppRole, Member string
		All                                     bool
		HasWhen                                 bool
		Due                                     int64
		WorkingDays                             int
	}
	levels := make([]level, 0, len(approval.Levels))
	for _, l := range approval.Levels {
		levels = append(levels, level{Title: l.Title, Structure: l.Structure, Role: l.Role, AppRole: l.AppRole,
			Member: l.Member, All: l.All, HasWhen: l.When != nil, Due: int64(l.Due), WorkingDays: l.WorkingDays})
	}
	return struct {
		Pending, Rejected string
		Levels            []level
	}{Pending: approval.Pending, Rejected: approval.Rejected, Levels: levels}
}
