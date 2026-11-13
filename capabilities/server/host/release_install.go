package host

import (
	"fmt"
	"platformserver/kernel/records"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	releaseowner "platformserver/services/devops/releases"
	"slices"
	"time"
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
	err = releaseowner.StageInstallations(registry, installations, platform.AssetApplyContext{Validate: validate, Upgrade: len(upgrade) > 0 && upgrade[0], Private: true},
		func(plan platform.AssetInstallation, installation platform.ReleaseInstallation) error {
			before, err := draft.Projection.RowDigest(plan.Record)
			if err != nil || before != installation.Before {
				return fmt.Errorf("release metadata predecessor differs for %s: %v", plan.Record, err)
			}
			return nil
		},
		func(plan platform.AssetInstallation) error {
			if err := draft.Projection.InstallImage(plan.Record, plan.Image); err != nil {
				return err
			}
			return draft.registerInstalledAsset(plan)
		})
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
	return releaseowner.CheckStorage(t.storedShapes(draft), t.definitions, draft.definitions, upgrade, allowContraction)
}
func (t *Tenant) storedShapes(after *Tenant) []releaseowner.StoredShape {
	shapes := []releaseowner.StoredShape{}
	for typ, old := range t.Projection.Types() {
		if next := after.Projection.Type(typ); next != nil {
			shapes = append(shapes, releaseowner.StoredShape{Before: old.Info(), After: next.Info(), Records: old.Rows().Len()})
		}
	}
	return shapes
}
func (t *Tenant) validateAssetSet(engines ...platform.RuleEngine) error {
	return releaseowner.ValidateSet(releaseowner.ValidationContext{
		Entities: t.releaseStoredShapes(), Definitions: t.definitions, Lookup: t.Entity, Properties: t.checkObjectProperties, RecordSets: t.checkRecordSetDeclarations,
		Link: func(app string, link platform.LinkType) error {
			return (hostView{t: t, app: t.App(app)}).ValidateInstallLinkType(link)
		},
		Application: t.checkApplicationExtension, PageQueries: t.checkPageQueries,
		Navigation: func(page platform.Page, app string) error { return pageowner.CheckNavigation(page, app, t.definitions) },
	}, engines...)
}

func (t *Tenant) releaseStoredShapes() []platform.EntityInfo {
	entities := []platform.EntityInfo{}
	for _, et := range t.Projection.SortedTypes() {
		entities = append(entities, et.Info())
	}
	return entities
}
func (t *Tenant) checkExtensionSet() error {
	return releaseowner.CheckExtensions(t.releaseStoredShapes(), t.Entity)
}
func (t *Tenant) releaseExtensionReview(candidate platform.ReleaseCandidate) ([]releaseowner.ReleaseExtensionReview, error) {
	shapes, err := releaseowner.AssetShapes(t.apps, candidate.Assets)
	if err != nil {
		return nil, err
	}
	return releaseowner.ExtensionReview(candidate, t.releaseStoredShapes(), shapes, t.Entity, (hostView{t: t}).EntitySource)
}

func (t *Tenant) releaseUpgradePlanLocked(candidate platform.ReleaseCandidate, members ...platform.Member) (*releaseowner.ReleaseUpgradePlan, error) {
	var member platform.Member
	if len(members) > 0 {
		member = members[0]
	}
	installations, bootstrap, err := t.prepareReleaseLifecycleLocked(candidate, member, time.Now().UTC(), true)
	if err != nil {
		return nil, err
	}
	records := t.Projection
	if len(bootstrap) > 0 {
		prepared, err := t.prepareAcceptedBatch(t.App("build").AcceptedLedger(), bootstrap)
		if err != nil || prepared == nil {
			return nil, fmt.Errorf("upgrade bootstrap cannot prepare: %v", err)
		}
		records = prepared.Records
	}
	view, err := t.assetInstallationView(records)
	if err != nil {
		return nil, err
	}
	trial, err := view.stageReleaseInstallationLocked(installations, true, true)
	if err != nil {
		return nil, err
	}
	return releaseowner.UpgradePlan(candidate.ID, t.releases.Active(), t.storedShapes(trial))
}
