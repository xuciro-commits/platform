package releases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"platformserver/platform"
)

type State struct {
	ReleaseRequests   map[string]string
	ReleaseCandidates map[string]json.RawMessage
	ReleaseApplied    map[string]string
	ActiveRelease     string
	Sealed            map[string]platform.SealedArtifact
	ReleaseOrigins    map[string]platform.ReleaseOrigin
}

type Store struct {
	candidates map[string]json.RawMessage
	applied    map[string]string
	active     string
	sealed     map[string]platform.SealedArtifact
	origins    map[string]platform.ReleaseOrigin
	requests   map[string]string
}

func (r *Store) Request(key string) (string, bool) {
	h, ok := r.requests[key]
	return h, ok
}

func (r *Store) RememberRequest(key, hash string) {
	if r.requests == nil {
		r.requests = map[string]string{}
	}
	r.requests[key] = hash
}

func (r *Store) Raw(id string) json.RawMessage { return r.candidates[id] }

func (r *Store) Candidate(id string) (platform.ReleaseCandidate, error) {
	return platform.ReadCandidate(id, r.candidates[id])
}

func (r *Store) Ids() []string {
	ids := slices.Sorted(maps.Keys(r.candidates))
	return ids
}

func (r *Store) Count() int { return len(r.candidates) }

func (r *Store) Put(id string, raw []byte) error {
	if prior, ok := r.candidates[id]; ok && !bytes.Equal(prior, raw) {
		return fmt.Errorf("immutable release candidate %s changed", id)
	}
	if r.candidates == nil {
		r.candidates = map[string]json.RawMessage{}
	}
	r.candidates[id] = slices.Clone(raw)
	return nil
}

func (r *Store) AppliedDigest(key string) (string, bool) {
	d, ok := r.applied[key]
	return d, ok
}

func (r *Store) Commit(key, digest, candidateID string, raw []byte, activate bool) error {
	if err := r.Put(candidateID, raw); err != nil {
		return err
	}
	if activate {
		r.active = candidateID
	}
	if r.applied == nil {
		r.applied = map[string]string{}
	}
	r.applied[key] = digest
	return nil
}

func (r *Store) Seal(id string, artifact platform.SealedArtifact) error {
	if prior, ok := r.sealed[id]; ok && prior.Digest != artifact.Digest {
		return fmt.Errorf("candidate %s is already sealed with another digest", id)
	}
	if r.sealed == nil {
		r.sealed = map[string]platform.SealedArtifact{}
	}
	r.sealed[id] = artifact
	return nil
}

func (r *Store) RememberOrigin(id string, origin platform.ReleaseOrigin) {
	if _, ok := r.origins[id]; ok {
		return
	}
	if r.origins == nil {
		r.origins = map[string]platform.ReleaseOrigin{}
	}
	r.origins[id] = origin
}

func (r *Store) Origin(id string) (platform.ReleaseOrigin, bool) {
	o, ok := r.origins[id]
	return o, ok
}

func (r *Store) SnapshotState() (s State) {
	s.ReleaseRequests = maps.Clone(r.requests)
	s.ReleaseCandidates, s.ReleaseApplied, s.ActiveRelease, s.Sealed, s.ReleaseOrigins = maps.Clone(r.candidates), maps.Clone(r.applied), r.active, maps.Clone(r.sealed), maps.Clone(r.origins)
	return s
}

func (r *Store) Restore(s State) {
	r.requests = maps.Clone(s.ReleaseRequests)
	r.candidates, r.applied, r.active, r.origins = maps.Clone(s.ReleaseCandidates), maps.Clone(s.ReleaseApplied), s.ActiveRelease, maps.Clone(s.ReleaseOrigins)
	if s.Sealed != nil {
		r.sealed = s.Sealed
	}
}

func (r *Store) Active() string                             { return r.active }
func (r *Store) Candidates() map[string]json.RawMessage     { return maps.Clone(r.candidates) }
func (r *Store) Applied() map[string]string                 { return maps.Clone(r.applied) }
func (r *Store) Sealed() map[string]platform.SealedArtifact { return maps.Clone(r.sealed) }

func (r *Store) SelectPreview(id string) { r.active = id }

type runtimeAssetOwner struct {
	platform.App
	lifecycle  platform.AssetLifecycleOwner
	code       []platform.ReleaseAsset
	nativeFlow func(platform.AssetRef, string) (platform.ReleaseAsset, error)
}

func (o runtimeAssetOwner) AssetKinds() []platform.AssetKind {
	var kinds []platform.AssetKind
	if o.lifecycle != nil {
		kinds = append(kinds, o.lifecycle.AssetKinds()...)
	}
	for _, asset := range o.code {
		if !slices.Contains(kinds, asset.Ref.Kind) {
			kinds = append(kinds, asset.Ref.Kind)
		}
	}
	return kinds
}
func (o runtimeAssetOwner) ReleaseAssets() ([]platform.ReleaseAsset, error) {
	out := slices.Clone(o.code)
	if o.lifecycle != nil {
		owned, err := o.lifecycle.ReleaseAssets()
		if err != nil {
			return nil, err
		}
		for _, asset := range owned {
			if !slices.ContainsFunc(o.code, func(original platform.ReleaseAsset) bool { return original.Ref == asset.Ref }) {
				out = append(out, asset)
			}
		}
	}
	return out, nil
}
func (o runtimeAssetOwner) ResolvePinned(ref platform.AssetRef, version string, contentVersion ...string) (platform.ReleaseAsset, error) {
	for _, asset := range o.code {
		if asset.Ref == ref && asset.SourceVersion == version && len(contentVersion) == 0 {
			return asset, nil
		}
	}
	if ref.Kind == platform.AssetFlow && len(contentVersion) == 0 && o.nativeFlow != nil {
		return o.nativeFlow(ref, version)
	}
	if o.lifecycle != nil {
		return o.lifecycle.ResolvePinned(ref, version, contentVersion...)
	}
	return platform.ReleaseAsset{}, fmt.Errorf("code asset %s has no retained source version %s", ref, version)
}
func (o runtimeAssetOwner) controlled(assets []platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, asset := range assets {
		i := slices.IndexFunc(o.code, func(code platform.ReleaseAsset) bool { return code.Ref == asset.Ref })
		if i < 0 {
			out = append(out, asset)
			continue
		}
		selected := o.code[i]
		if asset.Ref.Kind == platform.AssetFlow && o.nativeFlow != nil {
			retained, err := o.nativeFlow(asset.Ref, asset.SourceVersion)
			if err != nil {
				return nil, err
			}
			selected = retained
		}
		matches, err := ImmutableCodeAssetMatches(selected, asset)
		if err != nil {
			return nil, err
		}
		if !matches {
			return nil, fmt.Errorf("immutable code asset %s differs from the target runtime", asset.Ref)
		}
	}
	if len(out) > 0 && o.lifecycle == nil {
		return nil, fmt.Errorf("app %s has no controlled asset lifecycle", o.Manifest().ID)
	}
	return out, nil
}

func (o runtimeAssetOwner) AssetEnvironmentRequirements(asset platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	controlled, err := o.controlled([]platform.ReleaseAsset{asset})
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return nil, nil
	}
	return o.lifecycle.AssetEnvironmentRequirements(asset)
}
func (o runtimeAssetOwner) ReleaseDrafts(assets []platform.ReleaseAsset, context platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	controlled, err := o.controlled(assets)
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return nil, nil
	}
	return o.lifecycle.ReleaseDrafts(controlled, context)
}
func (o runtimeAssetOwner) BeginAssetBootstrap(assets []platform.ReleaseAsset) (func(), error) {
	controlled, err := o.controlled(assets)
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return func() {}, nil
	}
	return o.lifecycle.BeginAssetBootstrap(controlled)
}
func (o runtimeAssetOwner) PrepareReleaseAssets(assets []platform.ReleaseAsset, context platform.AssetReleaseContext) ([]platform.AssetInstallation, error) {
	controlled, err := o.controlled(assets)
	if err != nil {
		return nil, err
	}
	if len(controlled) == 0 {
		return nil, nil
	}
	return o.lifecycle.PrepareReleaseAssets(controlled, context)
}
func (o runtimeAssetOwner) DecodeAssetInstallation(schema string, image json.RawMessage) (platform.AssetInstallation, error) {
	if o.lifecycle == nil {
		return platform.AssetInstallation{}, fmt.Errorf("code owner %s cannot install %s", o.Manifest().ID, schema)
	}
	return o.lifecycle.DecodeAssetInstallation(schema, bytes.Clone(image))
}
func (o runtimeAssetOwner) ValidateAssetInstallation(plan platform.AssetInstallation) error {
	if o.lifecycle == nil {
		return fmt.Errorf("code owner %s has no installation image", o.Manifest().ID)
	}
	return o.lifecycle.ValidateAssetInstallation(plan)
}
func (o runtimeAssetOwner) ApplyAssetInstallation(plan platform.AssetInstallation, context platform.AssetApplyContext) error {
	if o.lifecycle == nil {
		return fmt.Errorf("code owner %s has no installation image", o.Manifest().ID)
	}
	return o.lifecycle.ApplyAssetInstallation(plan, context)
}

func ValidateCodeAsset(owner platform.App, code []platform.ReleaseAsset, asset platform.ReleaseAsset) error {
	if !slices.ContainsFunc(code, func(current platform.ReleaseAsset) bool { return current.Ref == asset.Ref }) {
		return fmt.Errorf("asset %s is not an original code declaration", asset.Ref)
	}
	remaining, err := (runtimeAssetOwner{App: owner, code: code}).controlled([]platform.ReleaseAsset{asset})
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("asset %s is not an immutable prerequisite", asset.Ref)
	}
	return nil
}

func NewRegistry(apps []platform.App, codeAssets func(platform.App) ([]platform.ReleaseAsset, error), sharedAssets func(string) []platform.ReleaseAsset, nativeFlow func(platform.App, platform.AssetRef, string) (platform.ReleaseAsset, error), systemUpdate bool) (*platform.AssetRegistry, error) {
	registry, err := platform.NewAssetRegistry()
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		code, err := codeAssets(app)
		if err != nil {
			return nil, err
		}
		if systemUpdate {
			shared := sharedAssets(app.Manifest().ID)
			code = slices.DeleteFunc(code, func(asset platform.ReleaseAsset) bool {
				return slices.ContainsFunc(shared, func(original platform.ReleaseAsset) bool { return original.Ref == asset.Ref })
			})
		}
		lifecycle, _ := app.(platform.AssetLifecycleOwner)
		owner := runtimeAssetOwner{App: app, lifecycle: lifecycle, code: code, nativeFlow: func(ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
			return nativeFlow(app, ref, source)
		}}
		if len(owner.AssetKinds()) == 0 {
			continue
		}
		if err := registry.Register(owner); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
