package host

import releaseowner "platformserver/services/devops/releases"

import (
	"platformserver/kernel/transport"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) registerDefinitions() error {
	entities := []platform.EntityInfo{}
	for _, et := range t.Projection.SortedTypes() {
		entities = append(entities, et.Info())
	}
	err := build.CodeDefinitions{Apps: t.apps, Entities: entities,
		Narrowable:    func(info platform.EntityInfo) []string { return t.narrowable(t.Projection.Type(info.Type)) },
		CheckSections: t.checkSections,
		BindInterface: func(q platform.NamedQuery) (platform.NamedQuery, error) { return t.bindInterfaceQuery(q) },
		InstallLink:   t.Projection.InstallLinkConstraint,
		NativeFlow:    t.nativeFlowAsset,
		CheckObject:   t.checkObjectProperties,
		Describe: func(def platform.Definition) (platform.Definition, error) {
			return t.describer().DescribeDefinition(def)
		},
		Store:      func(def platform.Definition) { t.setDefinitions(append(t.definitions, def)) },
		Current:    func() []platform.Definition { return t.definitions },
		UniqueRefs: releaseowner.UniqueRefs,
	}.Register()
	if err != nil {
		return err
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

func (t *Tenant) Definitions(m platform.Member) []platform.Definition {
	definitions, err := t.DefinitionsFor(m)
	if err != nil {
		return []platform.Definition{}
	}
	return definitions
}

func (t *Tenant) DefinitionsFor(m platform.Member) ([]platform.Definition, error) {
	m = t.authorizationMember(m)
	gen, key := t.definitionsFingerprint(), transport.Rights(m)+"/"+strconv.FormatUint(t.Projection.Generation(), 10)
	if out, ok := t.viewCache().Definitions(gen, key); ok {
		return out, nil
	}
	registered, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		return nil, err
	}
	out := t.definitionsFrom(m, registered)
	t.viewCache().StoreDefinitions(gen, key, out)
	return out, nil
}

func (r runtime) VisibleApplications(c platform.Caller) []platform.AssetRef {
	refs := []platform.AssetRef{}
	for _, definition := range r.t.Definitions(c.Member) {
		if definition.Application != nil {
			refs = append(refs, definition.Ref)
		}
	}
	return refs
}
func (d *stagedDecision) VisibleApplications(c platform.Caller) []platform.AssetRef {
	return (runtime{t: d.tenant}).VisibleApplications(c)
}

func (t *Tenant) definitionsFingerprint() uint64 { return t.DefinitionEpoch.Load() }

func (t *Tenant) setDefinitions(definitions []platform.Definition) {
	t.definitions = definitions
	t.DefinitionEpoch.Add(1)
	t.i18n.InstallApplications(definitions)
}
