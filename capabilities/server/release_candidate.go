package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"encoding/json"
	"fmt"

	"slices"
	"strings"
	"time"

	"platformserver/platform"
	"platformserver/studio/build"
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
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	available, err := registry.ReleaseAssets()
	if err != nil {
		return nil, err
	}
	if !replaceBuilder {
		return available, nil
	}
	code := map[platform.AssetRef]bool{}
	for _, definition := range t.definitions {
		if definition.Source == "code" {
			code[definition.Ref] = true
		}
	}
	out := slices.DeleteFunc(available, func(asset platform.ReleaseAsset) bool { return asset.Ref.App == build.ID && !code[asset.Ref] })
	return append(out, builderAssets...), nil
}

func (t *Tenant) codeReleaseAssetsLocked(app platform.App) ([]platform.ReleaseAsset, error) {
	manifest := app.Manifest()
	definitions := []platform.Definition{}
	special := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, definition := range t.definitions {
		if definition.Ref.App != manifest.ID || definition.Source != "code" {
			continue
		}
		definitions = append(definitions, definition)
		var asset platform.ReleaseAsset
		var err error
		switch definition.Ref.Kind {
		case platform.AssetInterface, platform.AssetProtocol, platform.AssetObservation:
			asset, err = t.definitionAsset(definition)
		case platform.AssetFlow:
			if definition.Flow == nil {
				return nil, fmt.Errorf("native flow has no owner descriptor")
			}
			asset, err = t.nativeFlowAsset(app, strings.TrimPrefix(definition.Ref.Name, manifest.ID+"."), definition.Flow.Version)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		special[definition.Ref] = asset
	}
	assets, err := releaseowner.CodeReleaseAssets(manifest, definitions, special)
	if err != nil {
		return nil, err
	}
	for i := range assets {
		assets[i], err = t.describeOwnedAsset(assets[i])
		if err != nil {
			return nil, err
		}
	}
	return append(assets, t.sharedSystemAssets(manifest.ID)...), nil
}

func (t *Tenant) previewCandidateLocked(selected []platform.AssetRef, builderAssets []platform.ReleaseAsset, additionalRoots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(builderAssets, true)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	var roots []platform.AssetRef
	for _, ref := range selected {
		roots = append(roots, releaseowner.DependentRoots(ref, available)...)
	}
	for _, other := range additionalRoots {
		if slices.ContainsFunc(available, func(a platform.ReleaseAsset) bool { return a.Ref == other }) &&
			!slices.Contains(roots, other) {
			roots = append(roots, other)
		}
	}
	return t.candidateWithBindings(roots, available, selected)
}

// ReferencedDrafts answers which saved record drafts a chosen draft needs, so
// a builder can select them together.
func (t *Tenant) ReferencedDrafts(m platform.Member, kind platform.AssetKind, id string) (releaseowner.ReleaseDraftClosure, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/drafts/referenced", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return releaseowner.ReleaseDraftClosure{}, fmt.Errorf("tenant has no builder")
	}
	drafts, err := owner.ReferencedDrafts(kind, id)
	if err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	if drafts == nil {
		drafts = []build.JointDraftRef{}
	}
	return releaseowner.ReleaseDraftClosure{Drafts: drafts}, nil
}

// PreviewRelease checks authority before looking up the owner's unfiltered
// records and before computing any digest or error path. It never changes the
// installed definitions, persistent records, or active operator work.
func (t *Tenant) PreviewRelease(m platform.Member, kind platform.AssetKind, id string) (releaseowner.ReleasePreview, error) {
	return t.PreviewReleaseDrafts(m, []build.JointDraftRef{{Kind: kind, ID: id}})
}

// PreviewReleaseDrafts compares one or several saved drafts with the installed
// development definitions, and proves the resulting joint candidate installs
// before it can be saved (/D2).
func (t *Tenant) PreviewReleaseDrafts(m platform.Member, drafts []build.JointDraftRef) (releaseowner.ReleasePreview, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/preview", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	reply, candidate, err := t.previewReleaseLocked(drafts, m)
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
			return releaseowner.ReleasePreview{}, fmt.Errorf("candidate object %s: %w", asset.Ref, err)
		}
		// A function/flow candidate retains its source object, but may not
		// retain its create/edit action assets. Ask the original owner for
		// those test inputs using that exact object image, not the live model.
		owner, ok := t.app(build.ID).(build.ConstructionDrafts)
		if !ok {
			return reply, fmt.Errorf("construction owner is unavailable")
		}
		actions, err := owner.CandidateActions(asset)
		if err != nil {
			return reply, err
		}
		for _, action := range actions {
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
func (t *Tenant) previewReleaseLocked(drafts []build.JointDraftRef, member platform.Member) (releaseowner.ReleasePreview, platform.ReleaseCandidate, error) {
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return releaseowner.ReleasePreview{}, platform.ReleaseCandidate{}, fmt.Errorf("tenant has no builder")
	}
	assets, diagnostic := owner.DraftReleaseAssetsMulti(drafts, member)
	reply := releaseowner.ReleasePreview{CandidateActions: []platform.Action{}, Included: []platform.AssetRef{}, Added: []platform.AssetRef{}, Removed: []platform.AssetRef{}, Changed: []platform.AssetRef{}}
	for _, draft := range drafts {
		if draft.ID != "" && !slices.Contains(reply.Drafts, draft) {
			reply.Drafts = append(reply.Drafts, draft)
		}
	}
	if diagnostic != nil {
		reply.Diagnostic = diagnostic.Error()
		// A review still names what is installed now: a draft that cannot be
		// assembled refuses the candidate, and the running closure is what the
		// reader needs to compare against.
		if len(assets.Priors) > 0 {
			if available, err := t.releaseAssetsLocked(nil, false); err == nil {
				var installedRoots []platform.AssetRef
				for _, prior := range assets.Priors {
					for _, owner := range releaseowner.DependentRoots(prior, available) {
						if !slices.Contains(installedRoots, owner) {
							installedRoots = append(installedRoots, owner)
						}
					}
				}
				if current, err := t.candidateWithBindings(installedRoots, available, assets.Priors); err == nil {
					reply.CurrentID = current.ID
				}
			}
		}
		return reply, platform.ReleaseCandidate{}, nil
	}
	var current platform.ReleaseCandidate
	var oldOwners []platform.AssetRef
	if len(assets.Priors) > 0 {
		available, err := t.releaseAssetsLocked(assets.Before, true)
		if err != nil {
			return releaseowner.ReleasePreview{}, platform.ReleaseCandidate{}, err
		}
		for _, prior := range assets.Priors {
			for _, owner := range releaseowner.DependentRoots(prior, available) {
				if !slices.Contains(oldOwners, owner) {
					oldOwners = append(oldOwners, owner)
				}
			}
		}
		current, err = t.candidateWithBindings(oldOwners, available, assets.Priors)
		if err != nil {
			return releaseowner.ReleasePreview{}, platform.ReleaseCandidate{}, fmt.Errorf("installed definition: %w", err)
		}
		reply.CurrentID = current.ID
		// Renames must still check surviving pages/apps that pointed at the
		// old identity. Removed generated assets are not roots of the new
		// release, but a surviving dependent must be closed or diagnosed.
		oldOwners = slices.DeleteFunc(oldOwners, func(ref platform.AssetRef) bool {
			return slices.Contains(assets.Priors, ref) && !slices.Contains(assets.Nexts, ref)
		})
	}
	candidate, err := t.previewCandidateLocked(assets.Nexts, assets.After, oldOwners)
	if err != nil {
		reply.Diagnostic = err.Error()
		return reply, platform.ReleaseCandidate{}, nil
	}
	// The same private installation path that activates a saved candidate runs
	// here against a tenant draft: a joint selection that installs nothing is
	// diagnosed now, before it can be saved (/D4). Preview validates
	// the sealed definitions; storage admission belongs to migration/activation.
	if len(drafts) > 1 {
		if _, _, err := t.prepareReleaseInstallationLocked(candidate, member, time.Now().UTC(), true, false); err != nil {
			reply.Diagnostic = err.Error()
			return reply, platform.ReleaseCandidate{}, nil
		}
	}
	reply.CandidateID = candidate.ID
	for _, asset := range candidate.Assets {
		reply.Included = append(reply.Included, asset.Ref)
	}
	if len(assets.Priors) == 0 {
		for _, asset := range candidate.Assets {
			reply.Added = append(reply.Added, asset.Ref)
		}
		return reply, candidate, nil
	}
	reply.Added, reply.Removed, reply.Changed, err = platform.CandidateDiff(current, candidate)
	return reply, candidate, err
}

// PreviewApplicationDrafts uses the same owner graph and candidate compiler
// as execution. The builder gate precedes access to authoring inventories.
func (t *Tenant) PreviewApplicationDrafts(m platform.Member, id string) (releaseowner.ReleasePreview, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	if !m.Holds(build.ID, build.Builder) {
		return releaseowner.ReleasePreview{}, fmt.Errorf("Only a builder can inspect application drafts")
	}
	t.mu.Lock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		t.mu.Unlock()
		return releaseowner.ReleasePreview{}, fmt.Errorf("Construction owner is unavailable")
	}
	drafts, err := owner.ApplicationDrafts(id)
	t.mu.Unlock()
	if err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	return t.PreviewReleaseDrafts(m, drafts)
}
