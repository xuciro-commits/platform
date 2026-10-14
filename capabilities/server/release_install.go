package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/host"
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
)

type releaseInstallation struct {
	build.ReleasePublication
	Before string             `json:"before"`
	Ref    *platform.AssetRef `json:"ref,omitempty"`
	Record string             `json:"record,omitempty"`
}

func (t *Tenant) installationPredecessor(record string) (string, error) {
	typ, id, ok := strings.Cut(record, "/")
	if !ok {
		return "", fmt.Errorf("invalid installation record %s", record)
	}
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	et := t.records.types[typ]
	if et == nil || et.rows.get(id) == nil {
		return "", fmt.Errorf("release metadata needs its predecessor %s", record)
	}
	image, err := et.rows.get(id).image()
	if err != nil {
		return "", err
	}
	return canonicalDigest(image)
}

func (t *Tenant) installationPlan(registry *platform.AssetRegistry, installation releaseInstallation) (platform.AssetInstallation, error) {
	app := build.ID
	if installation.Ref != nil {
		app = installation.Ref.App
	}
	plan, err := registry.DecodeAssetInstallation(app, installation.Schema, installation.Image)
	if err != nil {
		return plan, err
	}
	if installation.Ref != nil && (*installation.Ref != plan.Ref || installation.Record != plan.Record) {
		return plan, fmt.Errorf("release installation identity differs")
	}
	return plan, nil
}

// The owner performs every kind-specific install into this isolated runtime.
// Historical images and new plans share this machinery, never a second switch.
func (t *Tenant) stageReleaseInstallationLocked(installations []releaseInstallation, validate bool, upgrade ...bool) (*Tenant, error) {
	draft, err := t.assetInstallationView(t.records)
	if err != nil {
		return nil, err
	}
	registry, err := draft.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	for _, installation := range installations {
		plan, err := draft.installationPlan(registry, installation)
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
		et := draft.records.types[typ]
		value := reflect.New(et.info.Go).Elem()
		if err := json.Unmarshal(plan.Image, value.Addr().Interface()); err != nil {
			return nil, err
		}
		installed := *et.rows.get(id)
		installed.value, installed.original = value, nil
		installed.retainOriginal(plan.Image)
		et.setRow(id, &installed)
		draft.records.writes[plan.Record] = true
		if err := draft.registerInstalledAsset(plan); err != nil {
			return nil, err
		}

	}
	if err := draft.validateAssetSet(); err != nil {
		return nil, err
	}
	return draft, nil
}

func (t *Tenant) assetInstallationView(records *recordStore) (*Tenant, error) {
	draft := (hostView{t: t}).installationDraft()
	draft.records = records.forkRecords()
	var control tenantState
	t.console.snapshot(&control)
	draft.console.restore(&control)
	draft.Secrets = t.Secrets
	draft.apps = slices.Clone(t.apps)
	if t.procs != nil {
		draft.procs = t.procs.InstallationDraft()
		if app, ok := draft.procs.(platform.App); ok {
			host.Attach(app, hostView{t: draft, app: app})
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
		host.Attach(fork, hostView{t: draft, app: fork})
	}
	if err := draft.registerOwnedAssetMetadata(); err != nil {
		return nil, err
	}
	for key, app := range draft.owner {
		draft.owner[key] = draft.app(app.Manifest().ID)
	}
	return draft, nil
}

func (t *Tenant) validateAssetStorage(draft *Tenant, upgrade bool, reviewed ...platform.MigrationPlan) error {
	declared := func(typ, action string) bool {
		for _, plan := range reviewed {
			for _, step := range plan.Steps {
				if action == "" && slices.Contains(step.Types, typ) || action != "" && step.Kind == "retire-action" && step.Action == action && slices.Contains(step.Types, typ) {
					return true
				}
			}
		}
		return false
	}
	for typ, old := range t.records.types {
		next := draft.records.types[typ]
		if next == nil || old.rows.len() == 0 {
			continue
		}
		if !sameStoredFields(old.info, next.info) && !declared(typ, "") && (!upgrade || optionalStoredAddition(old.info, next.info) == nil && !t.migratedStorageLocked(old.info, next.info)) {
			return fmt.Errorf("object %s has records; changing their storage shape needs a supported upgrade plan", typ)
		}
	}
	for _, definition := range t.definitions {
		if definition.Action != nil && definition.Source != "code" && !slices.ContainsFunc(draft.definitions, func(next platform.Definition) bool { return next.Ref == definition.Ref }) {
			action := definition.Ref.Name
			if definition.Action.Automation {
				for _, suffix := range []string{platform.ApprovalHeld, platform.ApprovalRejected, platform.ApprovalReturned} {
					action = strings.TrimSuffix(action, suffix)
				}
			}
			if !declared(definition.Action.Target, action) && !t.completedActionRetirement(action) {
				return fmt.Errorf("retiring action %s needs an upgrade plan", definition.Ref.Name)
			}
		}
	}
	return nil
}

func (t *Tenant) validateAssetSet() error {
	for _, et := range t.records.sortedTypes() {
		if err := t.checkRecordSetDeclarations(et.info); err != nil {
			return err
		}
		if err := platform.CheckConstraints(et.info, t.entity, true); err != nil {
			return err
		}
	}
	pages := map[platform.AssetRef]platform.Page{}
	for _, definition := range t.definitions {
		if definition.LinkType != nil {
			view := hostView{t: t, app: t.app(definition.Ref.App)}
			if err := view.ValidateInstallLinkType(*definition.LinkType); err != nil {
				return err
			}
			for _, link := range definition.LinkVersions {
				if err := view.ValidateInstallLinkType(link); err != nil {
					return err
				}
			}
		}
		if definition.Page != nil {
			pages[definition.Ref] = *definition.Page
		}
	}
	for _, definition := range t.definitions {
		if definition.Application != nil {
			if err := t.checkPageQueries(definition.Application.QueryPage()); err != nil {
				return err
			}
			for _, name := range definition.Application.Pages {
				ref := platform.AssetRef{App: definition.Ref.App, Kind: platform.AssetPage, Name: name}
				page, ok := pages[ref]
				if !ok {
					return fmt.Errorf("application page %s is unavailable", ref)
				}
				if err := definition.Application.CheckPageVariables(page); err != nil {
					return err
				}
			}
		}
		if definition.Page != nil {
			if err := t.checkPageNavigation(*definition.Page, definition.Ref.App); err != nil {
				return err
			}
		}
	}
	return nil
}

func sameStoredFields(a, b platform.EntityInfo) bool {
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for _, old := range a.Fields {
		next, ok := b.Field(old.Name)
		if !ok || old.Type != next.Type || old.Ref != next.Ref || old.Required != next.Required || !slices.Equal(old.Choices, next.Choices) {
			return false
		}
	}
	return true
}
