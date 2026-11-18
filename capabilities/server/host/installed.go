package host

import (
	"fmt"
	releaseowner "platformserver/services/devops/releases"

	pageowner "platformserver/services/apps/pages"
	"slices"
	"strings"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func (t *Tenant) Install(app platform.Service, e platform.RecordType, actions []platform.Action, pages ...platform.Page) error {
	return build.ObjectInstall{
		Owners: t.owner, Owner: t.App, Installing: func() bool { return t.installingSet },
		Properties: t.checkObjectProperties, RecordSets: func(info platform.RecordTypeInfo) error {
			return t.objectOwner(t.Projection).CheckRecordSetDeclarations(info)
		}, Entity: t.Entity,
		Install:     t.Projection.Install,
		Definitions: func() []platform.Definition { return t.definitions }, SetDefinitions: t.setDefinitions,
		Define: t.installDefinitions,
	}.Run(app, e, actions, pages)
}

func (t *Tenant) storeDefinition(index int, def platform.Definition) {
	if index >= 0 {
		t.definitions[index] = def
	} else {
		t.definitions = append(t.definitions, def)
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	t.setDefinitions(t.definitions)
}

func (t *Tenant) installDefinitions(app string, info platform.RecordTypeInfo, actions []platform.Action, pages []platform.Page, protocols ...platform.Provision) error {
	var err error
	for _, definition := range releaseowner.ObjectDefinitions(app, info, actions, pages, protocols...) {
		var def platform.Definition
		if def, err = t.describer().DescribeDefinition(definition); err != nil {
			break
		}
		if i := slices.IndexFunc(t.definitions, func(x platform.Definition) bool { return x.Ref == def.Ref }); i >= 0 {
			next := slices.Clone(t.definitions)
			next[i] = def
			t.setDefinitions(next)
			continue
		}
		t.setDefinitions(append(t.definitions, def))
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return err
}

func (t *Tenant) reinstall() error {
	before := t.installingSet
	t.installingSet = true
	defer func() { t.installingSet = before }()
	if err := t.registerOwnedAssetMetadata(); err != nil {
		return err
	}
	if owner := t.App(build.ID); owner != nil {
		if err := owner.(interface{ Reinstall() error }).Reinstall(); err != nil {
			return fmt.Errorf("tenant %s: %s: %v", t.ID, build.ID, err)
		}
	}
	if err := t.registerOwnedAssetMetadata(); err != nil {
		return err
	}
	t.installingSet = before
	return t.validateAssetSet()
}

func (t *Tenant) InstallPage(app platform.Service, p platform.Page) error {
	return t.installPage(app, p, true)
}

func (t *Tenant) installPage(app platform.Service, p platform.Page, targets bool, engines ...platform.RuleEngine) error {
	canonical, info, err := t.pageBindings().CheckInstall(p, app.Manifest().ID, targets, engines...)
	if err != nil {
		return err
	}
	return t.installDefinitions(app.Manifest().ID, info, nil, []platform.Page{canonical})
}

func (t *Tenant) checkSections(p platform.Page, page platform.RecordTypeInfo) error {
	return t.pageBindings().CheckSections(p, page)
}

func (t *Tenant) pageBindings() pageowner.Bindings {
	return pageowner.Bindings{Entity: t.Entity, Criteria: t.modelCriteria, Rules: t.services.Rules,
		Operation: t.pageOperation, Definitions: t.definitions, Interface: t.bindInterfaceQuery,
		Action: func(name string) (string, platform.Action, bool) {
			owner := t.App(t.owner["action:"+name])
			if owner == nil {
				return "", platform.Action{}, false
			}
			action, ok := owner.Manifest().Actions.Action(name)
			return owner.Manifest().ID, action, ok
		},
		FunctionAsset: func(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
			owner := t.FunctionAssets[ref.App]
			if owner == nil {
				return platform.ReleaseAsset{}, fmt.Errorf("page function owner is unavailable")
			}
			return owner.FunctionReleaseAsset(ref.Name, version)
		},
	}
}

func (t *Tenant) Entity(typ string) (platform.RecordTypeInfo, bool) {
	return t.Projection.EntityInfo(typ)
}

func (t *Tenant) InstallApplication(app platform.Service, a platform.Application) error {
	id := app.Manifest().ID
	if err := t.checkApplicationExtension(platform.AssetRef{App: id, Kind: platform.AssetApp, Name: a.Name}, a); err != nil {
		return err
	}
	err := t.pageBindings().CheckApplication(id, a, func(ref platform.AssetRef) (bool, error) {
		known := slices.ContainsFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
		if !known && (ref.Kind == platform.AssetAgent || t.definitionOwners(false).Metadata(ref)) {
			registered, err := t.ownedAssetDefinitions(t.definitions)
			if err != nil {
				return false, err
			}
			known = slices.ContainsFunc(registered, func(d platform.Definition) bool { return d.Ref == ref })
		}
		if ref.Kind == platform.AssetFlow {
			known = t.Processes != nil && strings.HasPrefix(ref.Name, ref.App+".") && t.Processes.HasPublishedFlow(ref.Name)
		}
		return known, nil
	})
	if err != nil {
		return err
	}
	return t.storeApplication(app, a)
}

func (t *Tenant) storeApplication(app platform.Service, a platform.Application) error {
	id := app.Manifest().ID
	installed := a
	def := platform.Definition{Ref: platform.AssetRef{App: id, Kind: platform.AssetApp, Name: a.Name}, Source: "tenant", Version: "1",
		ContractVersion: 1, Application: &installed}
	def, err := t.describer().DescribeDefinition(def)
	if err != nil {
		return err
	}
	t.storeDefinition(slices.IndexFunc(t.definitions, func(x platform.Definition) bool { return x.Ref == def.Ref }), def)
	return nil
}

func (t *Tenant) checkPageQueries(p platform.Page) error {
	return t.pageBindings().CheckQueries(p)
}

func (t *Tenant) checkApplicationExtension(ref platform.AssetRef, a platform.Application) error {
	return releaseowner.CheckApplicationExtension(ref, a, t.definitions)
}
