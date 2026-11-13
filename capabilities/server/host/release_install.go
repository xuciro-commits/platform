package host

import (
	"fmt"
	"platformserver/kernel/records"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	releaseowner "platformserver/services/devops/releases"
	"slices"
)

func (t *Tenant) stageReleaseInstallationLocked(installations []platform.ReleaseInstallation, validate bool, upgrade ...bool) (*Tenant, error) {
	draft, err := t.assetInstallationView(t.Projection)
	if err != nil {
		return nil, err
	}
	registry, err := draft.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	draft.installingSet = true
	err = releaseowner.StageReleaseInstallation(registry, installations, draft.Projection,
		platform.AssetApplyContext{Validate: validate, Upgrade: len(upgrade) > 0 && upgrade[0], Private: true}, draft.registerInstalledAsset)
	if err != nil {
		return nil, err
	}
	draft.installingSet = false
	var engines []platform.RuleEngine
	if validate {
		engines = append(engines, t.services.Rules)
	}
	if err := draft.validateAssetSet(engines...); err != nil {
		return nil, err
	}
	return draft, nil
}

func (t *Tenant) assetInstallationView(recordView *records.Store, includeArchived ...bool) (*Tenant, error) {
	draft := (hostView{t: t}).installationDraft()
	if len(includeArchived) > 0 {
		draft.includeArchivedDefinitions = includeArchived[0]
	}
	draft.Projection = recordView.ForkRecords()
	draft.console.Restore(t.console.SnapshotState())
	draft.Secrets = t.Secrets
	apps := slices.Clone(t.apps)
	if t.procs != nil {
		draft.procs = t.procs.InstallationDraft()
		if app, ok := draft.procs.(platform.App); ok {
			(hostView{t: draft, app: app}).bindOwner(app)
		}
	}
	for i, app := range apps {
		if t.assets[app.Manifest().ID] == nil {
			continue
		}
		fork, err := app.Fork(app.AcceptedLedger())
		if err != nil || fork == nil {
			return nil, fmt.Errorf("asset owner %s cannot fork: %v", app.Manifest().ID, err)
		}
		apps[i] = fork
		(hostView{t: draft, app: fork}).bindOwner(fork)
	}
	draft.ownerSet = draft.with(apps)
	if err := draft.registerOwnedAssetMetadata(); err != nil {
		return nil, err
	}
	return draft, nil
}

func (t *Tenant) validateAssetStorage(draft *Tenant, upgrade, allowContraction bool) error {
	return releaseowner.CheckStorage(releaseowner.StoredShapes(t.Projection, draft.Projection), t.definitions, draft.definitions, upgrade, allowContraction)
}
func (t *Tenant) validateAssetSet(engines ...platform.RuleEngine) error {
	return releaseowner.ValidateSet(releaseowner.ValidationContext{
		Entities: releaseowner.StoredEntities(t.Projection), Definitions: t.definitions, Lookup: t.Entity, Properties: t.checkObjectProperties, RecordSets: t.checkRecordSetDeclarations,
		Link: func(app string, link platform.LinkType) error {
			return (hostView{t: t, app: t.App(app)}).ValidateInstallLinkType(link)
		},
		Application: t.checkApplicationExtension, PageQueries: t.checkPageQueries,
		Navigation: func(page platform.Page, app string) error { return pageowner.CheckNavigation(page, app, t.definitions) },
	}, engines...)
}

func (t *Tenant) checkExtensionSet() error {
	return releaseowner.CheckExtensions(releaseowner.StoredEntities(t.Projection), t.Entity)
}
func (t *Tenant) releaseExtensionReview(candidate platform.ReleaseCandidate) ([]releaseowner.ReleaseExtensionReview, error) {
	return releaseowner.ReviewExtensions(candidate, t.apps, releaseowner.StoredEntities(t.Projection), t.Entity, (hostView{t: t}).EntitySource)
}
