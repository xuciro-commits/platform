package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/records"
	"platformserver/platform"
	pageowner "platformserver/studio/pages"
	releaseowner "platformserver/studio/releases"
	"reflect"
	"slices"
	"strings"
	"time"
)

func (t *Tenant) installationPredecessor(record string) (string, error) {
	typ, id, ok := strings.Cut(record, "/")
	if !ok {
		return "", fmt.Errorf("invalid installation record %s", record)
	}
	t.records.Lock()
	defer t.records.Unlock()
	et := t.records.Type(typ)
	if et == nil || et.Rows().Get(id) == nil {
		return "", fmt.Errorf("release metadata needs its predecessor %s", record)
	}
	image, err := et.Rows().Get(id).Image()
	if err != nil {
		return "", err
	}
	return platform.CanonicalDigest(image)
}

// The owner performs every kind-specific install into this isolated runtime.
// Historical images and new plans share this machinery, never a second switch.
func (t *Tenant) stageReleaseInstallationLocked(installations []platform.ReleaseInstallation, validate bool, upgrade ...bool) (*Tenant, error) {
	draft, err := t.assetInstallationView(t.records)
	if err != nil {
		return nil, err
	}
	registry, err := draft.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	draft.installingSet = true
	for _, installation := range installations {
		plan, err := releaseowner.InstallationPlan(registry, installation)
		if err != nil {
			return nil, err
		}
		before, err := draft.installationPredecessor(plan.Record)
		if err != nil || before != installation.Before {
			return nil, fmt.Errorf("release metadata predecessor differs for %s: %v", plan.Record, err)
		}
		if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{Validate: validate, Upgrade: len(upgrade) > 0 && upgrade[0], Private: true}); err != nil {
			return nil, err
		}
		typ, id, _ := strings.Cut(plan.Record, "/")
		et := draft.records.Type(typ)
		value := reflect.New(et.Info().Go).Elem()
		if err := json.Unmarshal(plan.Image, value.Addr().Interface()); err != nil {
			return nil, err
		}
		installed := *et.Rows().Get(id)
		installed.Value, installed.Original = value, nil
		installed.RetainOriginal(plan.Image)
		et.SetRow(id, &installed)
		draft.records.TrackWrite(plan.Record)
		if err := draft.registerInstalledAsset(plan); err != nil {
			return nil, err
		}

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

func (t *Tenant) assetInstallationView(recordView *records.Store) (*Tenant, error) {
	draft := (hostView{t: t}).installationDraft()
	draft.records = recordView.ForkRecords()
	draft.console.Restore(t.console.SnapshotState())
	draft.Secrets = t.Secrets
	draft.apps = slices.Clone(t.apps)
	if t.procs != nil {
		draft.procs = t.procs.InstallationDraft()
		if app, ok := draft.procs.(platform.App); ok {
			(hostView{t: draft, app: app}).bindOwner(app)
		}
	}
	for i, app := range draft.apps {
		if _, ok := app.(platform.AssetLifecycleOwner); !ok {
			continue
		}
		var fork platform.App
		var err error
		if owner, ok := app.(platform.DecisionForkApp); ok {
			fork, err = owner.ForkDecision()
		} else if owner, ok := app.(platform.AcceptedStateApp); ok {
			fork, err = owner.ForkAcceptedState()
		} else {
			return nil, fmt.Errorf("asset owner %s has no private installation fork", app.Manifest().ID)
		}
		if err != nil || fork == nil {
			return nil, fmt.Errorf("asset owner %s cannot fork: %v", app.Manifest().ID, err)
		}
		draft.apps[i] = fork
		(hostView{t: draft, app: fork}).bindOwner(fork)
	}
	if err := draft.registerOwnedAssetMetadata(); err != nil {
		return nil, err
	}
	for key, app := range draft.owner {
		draft.owner[key] = draft.app(app.Manifest().ID)
	}
	return draft, nil
}

func (t *Tenant) validateAssetStorage(draft *Tenant, upgrade bool) error {
	shapes := []releaseowner.StoredShape{}
	for typ, old := range t.records.Types() {
		if next := draft.records.Type(typ); next != nil {
			shapes = append(shapes, releaseowner.StoredShape{Before: old.Info(), After: next.Info(), Records: old.Rows().Len()})
		}
	}
	return releaseowner.CheckStorage(shapes, t.definitions, draft.definitions, upgrade)
}
func (t *Tenant) validateAssetSet(engines ...platform.RuleEngine) error {
	return releaseowner.ValidateSet(releaseowner.ValidationContext{
		Entities: t.releaseStoredShapes(), Definitions: t.definitions, Lookup: t.entity, Properties: t.checkObjectProperties, RecordSets: t.checkRecordSetDeclarations,
		Link: func(app string, link platform.LinkType) error {
			return (hostView{t: t, app: t.app(app)}).ValidateInstallLinkType(link)
		},
		Application: t.checkApplicationExtension, PageQueries: t.checkPageQueries,
		Navigation: func(page platform.Page, app string) error { return pageowner.CheckNavigation(page, app, t.definitions) },
	}, engines...)
}

func (t *Tenant) releaseStoredShapes() []platform.EntityInfo {
	entities := []platform.EntityInfo{}
	for _, et := range t.records.SortedTypes() {
		entities = append(entities, et.Info())
	}
	return entities
}
func (t *Tenant) checkExtensionSet() error {
	return releaseowner.CheckExtensions(t.releaseStoredShapes(), t.entity)
}
func (t *Tenant) releaseExtensionReview(candidate platform.ReleaseCandidate) ([]releaseowner.ReleaseExtensionReview, error) {
	shapes, err := releaseowner.AssetShapes(t.apps, candidate.Assets)
	if err != nil {
		return nil, err
	}
	return releaseowner.ExtensionReview(candidate, t.releaseStoredShapes(), shapes, t.entity, (hostView{t: t}).EntitySource)
}
func (t *Tenant) releaseAssetShapes(available []platform.ReleaseAsset) (map[string]platform.EntityInfo, error) {
	return releaseowner.AssetShapes(t.apps, available)
}

// The plan is derived from immutable candidate bytes and current storage;
// callers cannot supply migration instructions or replace its source binding.
func (t *Tenant) releaseUpgradePlanLocked(candidate platform.ReleaseCandidate, members ...platform.Member) (*releaseowner.ReleaseUpgradePlan, error) {
	var member platform.Member
	if len(members) > 0 {
		member = members[0]
	}
	installations, bootstrap, err := t.prepareReleaseLifecycleLocked(candidate, member, time.Now().UTC(), true)
	if err != nil {
		return nil, err
	}
	records := t.records
	if len(bootstrap) > 0 {
		prepared, err := t.prepareAcceptedBatch(t.app("build").AcceptedLedger(), bootstrap)
		if err != nil || prepared == nil {
			return nil, fmt.Errorf("upgrade bootstrap cannot prepare: %v", err)
		}
		records = prepared.records
	}
	view, err := t.assetInstallationView(records)
	if err != nil {
		return nil, err
	}
	trial, err := view.stageReleaseInstallationLocked(installations, true, true)
	if err != nil {
		return nil, err
	}
	shapes := []releaseowner.StoredShape{}
	for typ, old := range t.records.Types() {
		if next := trial.records.Type(typ); next != nil {
			shapes = append(shapes, releaseowner.StoredShape{Before: old.Info(), After: next.Info(), Records: old.Rows().Len()})
		}
	}
	return releaseowner.UpgradePlan(candidate.ID, t.releases.Active(), shapes)
}
