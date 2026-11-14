package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	"slices"
)

func (t *Tenant) Catalog(m platform.Member) []platform.Action {
	gen, key := t.definitionsFingerprint(), transport.Rights(m)
	if out, ok := t.viewCache().Catalog(gen, key); ok {
		return out
	}
	m = m.WithAuthorization(t.services.Authorization)
	out := actions.CatalogHooks{Apps: t.LiveApps, Entities: func() []platform.EntityInfo { return t.Entities(m) }, Provider: func(used string) (platform.App, string, bool) { return t.router().Provider(used) },
		SchemaVersion: t.actionSchemaVersion}.Of(m)
	t.viewCache().StoreCatalog(gen, key, out)
	return out
}

func (t *Tenant) Declarations() []*pb.AuthorityDeclaration {
	return runtimekernel.DeclarationsOf(t.apps)
}

func (t *Tenant) manifests() []platform.Manifest {
	gen := t.definitionsFingerprint()
	out := []platform.Manifest{}
	for _, a := range t.LiveApps() {
		out = append(out, transport.ManifestOf(a, gen))
	}
	return out
}

func (t *Tenant) Apps() []transport.AppInfo { return transport.AppInfos(t.manifests()) }

func (t *Tenant) AppsOf(m platform.Member) []transport.AppEntry {
	return transport.AppEntries(t.manifests(), m.Roles)
}

func (t *Tenant) Member(id string) (platform.Member, bool) {
	if t.identity == nil {
		return platform.Member{}, false
	}
	member, found := t.identity.MemberByID(id)
	return member.WithAuthorization(t.services.Authorization), found
}

func (t *Tenant) AppIDs() []string { return slices.Clone(t.ids) }

func (t *Tenant) manifestById(id string) platform.Manifest {
	a := t.App(id)
	if a == nil {
		return platform.Manifest{}
	}
	return transport.ManifestOf(a, t.definitionsFingerprint())
}
