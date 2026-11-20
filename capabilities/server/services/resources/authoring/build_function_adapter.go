package authoring

import (
	"context"
	"encoding/json"
	"platformserver/kernel/protocol"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type functionEnvironment struct {
	b *Build
}

func (e functionEnvironment) Host() protocol.BuildHost   { return e.b.host }
func (e functionEnvironment) Ledger() platform.Authority { return e.b.authority }
func (e functionEnvironment) Resolver() publication.Resolver {
	return e.b.resolver
}
func (e functionEnvironment) App() string         { return ID }
func (e functionEnvironment) Version() string     { return ValueDefinitionVersion }
func (e functionEnvironment) CallRoles() []string { return []string{Builder, User} }
func (e functionEnvironment) AllRoles() []string  { return e.b.manifestRoles() }
func (e functionEnvironment) AddActions(actions ...platform.Action) {
	e.b.authority.Catalog().Add(actions...)
}
func (e functionEnvironment) InstalledObject(typ string) (platform.RecordType, bool) {
	entity, ok := e.b.installed[typ]
	return entity, ok
}
func (e functionEnvironment) JointEntity(typ string) (platform.RecordTypeInfo, bool) {
	entity, ok := e.b.jointEntities[typ]
	return entity, ok
}
func (e functionEnvironment) JointDeclaration(ref platform.AssetRef) (platform.ReleaseAsset, bool) {
	asset, ok := e.b.jointDeclarations[ref]
	return asset, ok
}
func (e functionEnvironment) DeclaresRole(role string) bool   { return e.b.declaresRole(role) }
func (e functionEnvironment) CheckName(name, id string) error { return e.b.checkName("", name, id) }
func (e functionEnvironment) ConditionLookup(typ string) (platform.RecordTypeInfo, bool) {
	return e.b.conditionLookup(typ)
}
func (e functionEnvironment) CheckRuleCollections(spec platform.RuleReadSpec, where string, lookup func(string) (platform.RecordTypeInfo, bool)) error {
	return e.b.checkRuleCollections(spec, where, lookup)
}

var _ publication.FunctionEnvironment = functionEnvironment{}

func (b *Build) FunctionDefinition(name string, version int) (platform.AIFunction, int, bool) {
	return b.functions.Definition(name, version)
}

func (b *Build) FunctionReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return b.functions.Resolve(name, sourceVersion)
}

func (b *Build) TrainingCandidates(c platform.Caller, event platform.Event, images map[string]json.RawMessage) []protocol.AITrainingCandidate {
	return b.functions.TrainingCandidates(c, event, images)
}

func (b *Build) Compute(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return b.functions.Compute(ctx, name, input)
}
