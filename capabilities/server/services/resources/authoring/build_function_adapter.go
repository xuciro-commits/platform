package build

import (
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
)

type functionEnvironment struct {
	b *Build
}

func (e functionEnvironment) Host() platform.BuildHost   { return e.b.host }
func (e functionEnvironment) Ledger() platform.Authority { return e.b.ledger }
func (e functionEnvironment) App() string                { return ID }
func (e functionEnvironment) Version() string            { return ValueDefinitionVersion }
func (e functionEnvironment) CallRoles() []string        { return []string{Builder, User} }
func (e functionEnvironment) AllRoles() []string         { return e.b.manifestRoles() }
func (e functionEnvironment) AddActions(actions ...platform.Action) {
	e.b.ledger.Catalog().Add(actions...)
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
func (e functionEnvironment) CheckName(name, id string) error { return e.b.checkName(name, id) }
func (e functionEnvironment) ConditionLookup(typ string) (platform.EntityInfo, bool) {
	return e.b.conditionLookup(typ)
}
func (e functionEnvironment) CheckRuleCollections(spec platform.RuleReadSpec, where string, lookup func(string) (platform.EntityInfo, bool)) error {
	return e.b.checkRuleCollections(spec, where, lookup)
}

var _ functionauthoring.Environment = functionEnvironment{}
