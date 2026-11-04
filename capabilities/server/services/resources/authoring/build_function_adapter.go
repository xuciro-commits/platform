package build

import (
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
)

type functionEnvironment struct {
	b *Build
}

func (e functionEnvironment) Host() platform.BuildHost { return e.b.host }
func (e functionEnvironment) Ledger() *platform.Ledger { return e.b.ledger }
func (e functionEnvironment) App() string              { return ID }
func (e functionEnvironment) Version() string          { return e.b.Manifest().Version }
func (e functionEnvironment) CallRoles() []string      { return []string{Builder, User} }
func (e functionEnvironment) AllRoles() []string       { return e.b.manifestRoles() }
func (e functionEnvironment) AddActions(actions ...platform.Action) {
	e.b.ledger.Catalog.Add(actions...)
}
func (e functionEnvironment) InstalledObject(typ string) (platform.Entity, bool) {
	entity, ok := e.b.installed[typ]
	return entity, ok
}
func (e functionEnvironment) JointEntity(typ string) (platform.EntityInfo, bool) {
	entity, ok := e.b.jointEntities[typ]
	return entity, ok
}
func (e functionEnvironment) DeclaresRole(role string) bool { return e.b.declaresRole(role) }
func (e functionEnvironment) JointDeclaration(ref platform.AssetRef) (platform.ReleaseAsset, bool) {
	asset, ok := e.b.jointDeclarations[ref]
	return asset, ok
}
func (e functionEnvironment) AssetVersion(asset platform.ReleaseAsset) int {
	return e.b.definitionAssetVersion(asset, platform.AssetFunction, "function")
}
func (e functionEnvironment) AllReleaseAssets() ([]platform.ReleaseAsset, error) {
	return e.b.ReleaseAssets()
}

var _ functionauthoring.Environment = functionEnvironment{}
