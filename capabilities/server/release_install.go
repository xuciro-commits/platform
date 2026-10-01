package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"platformserver/apps/build"
	"platformserver/platform"
)

type releaseInstallation struct {
	build.ReleasePublication
	Before string `json:"before"`
}

func (t *Tenant) prepareReleaseActivationLocked(id string, raw []byte) ([]releaseInstallation, error) {
	saved, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return nil, err
	}
	owner, ok := t.app(build.ID).(*build.Build)
	if !ok {
		return nil, fmt.Errorf("tenant has no builder")
	}
	publications, builderAssets, err := owner.PrepareReleasePublications(saved.Assets)
	if err != nil {
		return nil, err
	}
	available, err := t.releaseAssetsLocked(builderAssets, true)
	if err != nil {
		return nil, err
	}
	roots := make([]platform.AssetRef, 0, len(saved.Assets))
	for _, asset := range saved.Assets {
		roots = append(roots, asset.Ref)
	}
	resolved, err := t.candidateWithFunctions(roots, available, nil)
	if err != nil {
		return nil, fmt.Errorf("saved release cannot be installed with this runtime: %w", err)
	}
	if resolved.ID != id {
		added, removed, changed, _ := platform.CandidateDiff(saved, resolved)
		return nil, fmt.Errorf("saved release cannot be installed with this runtime: changed %v, missing %v, extra %v", changed, removed, added)
	}
	priority := func(schema string) int {
		switch schema {
		case build.SchemaPublish:
			return 0
		case build.SchemaRelease:
			return 1
		case build.SchemaCodePublish, build.SchemaFunction:
			return 1
		case build.SchemaProcess:
			return 2
		default:
			return 3
		}
	}
	slices.SortStableFunc(publications, func(a, b build.ReleasePublication) int { return priority(a.Schema) - priority(b.Schema) })
	installations := make([]releaseInstallation, 0, len(publications))
	for _, publication := range publications {
		typ, rowID, err := build.PublicationRecord(publication)
		if err != nil {
			return nil, err
		}
		t.records.mu.Lock()
		row := t.records.types[typ].rows[rowID]
		image, err := row.image()
		before, err := canonicalDigest(image)
		t.records.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if publication.Schema == build.SchemaPublish {
			if err := owner.ValidateAcceptedPublication(publication.Schema, publication.Image); err != nil {
				return nil, err
			}
		}
		installations = append(installations, releaseInstallation{publication, before})
	}
	if _, err := t.stageReleaseInstallationLocked(installations, true); err != nil {
		return nil, err
	}
	if err := t.pendingWorkFitsLocked(id, raw); err != nil {
		return nil, err
	}
	return installations, nil
}

// The same private installation machinery prepares a live activation and
// applies committed descriptor images. Replay does not run today's policies.
func (t *Tenant) stageReleaseInstallationLocked(installations []releaseInstallation, validate bool) (*Tenant, error) {
	owner := t.app(build.ID).(*build.Build)
	draft := (hostView{t: t, app: owner}).installationDraft()
	ledger := platform.NewLedger(t.ID, build.ID, platform.NewCatalog())
	for _, installation := range installations {
		typ, id, err := build.PublicationRecord(installation.ReleasePublication)
		if err != nil {
			return nil, err
		}
		et := draft.records.types[typ]
		if et == nil || et.rows[id] == nil {
			return nil, fmt.Errorf("release metadata needs its predecessor %s/%s", typ, id)
		}
		image, err := et.rows[id].image()
		if err != nil {
			return nil, err
		}
		before, err := canonicalDigest(image)
		if err != nil || before != installation.Before {
			return nil, fmt.Errorf("release metadata predecessor differs for %s/%s", typ, id)
		}
		value := reflect.New(et.info.Go).Elem()
		if err := json.Unmarshal(installation.Image, value.Addr().Interface()); err != nil {
			return nil, err
		}
		// Publication is derived metadata; the mutable draft and its revision
		// are preserved. Its history is not another generated edit decision.
		et.rows[id].value = value
		et.rows[id].original = nil
		et.rows[id].retainOriginal(installation.Image)
		draft.records.writes[typ+"/"+id] = true
		decl, err := owner.ReleaseDeclaration(installation.ReleasePublication)
		if err != nil {
			return nil, err
		}
		switch installation.Schema {
		case build.SchemaPublish:
			if validate {
				info, err := platform.Describe(build.ID, decl.Entity, func(reflect.Type) string { return "" })
				if err != nil {
					return nil, err
				}
				if old := draft.records.types[info.Type]; old != nil && len(old.rows) > 0 && !sameStoredFields(old.info, info) {
					return nil, fmt.Errorf("object %s has records; changing their storage shape needs an upgrade plan", info.Type)
				}
				for _, def := range t.definitions {
					if def.Ref.App == build.ID && def.Action != nil && def.Action.Target == info.Type &&
						!slices.ContainsFunc(decl.Actions, func(a platform.Action) bool { return a.Schema == def.Ref.Name }) {
						return nil, fmt.Errorf("retiring action %s needs an upgrade plan", def.Ref.Name)
					}
				}
			}
			if err := ledger.Extend([]string{decl.Entity.Type}, decl.Actions); err != nil {
				return nil, err
			}
			if err := draft.Install(owner, decl.Entity, decl.Actions, decl.Pages...); err != nil {
				return nil, err
			}
		case build.SchemaRelease:
			if err := draft.installPage(owner, decl.Pages[0], false); err != nil {
				return nil, err
			}
		case build.SchemaHandOver:
			if err := draft.InstallApplication(owner, *decl.Application); err != nil {
				return nil, err
			}
		case build.SchemaCodePublish:
			view := hostView{t: draft, app: owner}
			if err := view.InstallOperation(platform.Caller{Replaying: true}, *decl.Operation, decl.Version); err != nil {
				return nil, err
			}
		case build.SchemaFunction:
			view := hostView{t: draft, app: owner}
			if err := view.InstallFunction(platform.Caller{Replaying: true}, *decl.Function, decl.Version); err != nil {
				return nil, err
			}
		case build.SchemaProcess:
			if t.procs == nil {
				return nil, fmt.Errorf("flow runtime is unavailable")
			}
			if err := t.procs.Validate(owner, *decl.Flow); err != nil {
				return nil, err
			}
		}
	}
	for _, definition := range draft.definitions {
		if definition.Page != nil {
			if err := draft.checkPageNavigation(*definition.Page, definition.Ref.App); err != nil {
				return nil, err
			}
		}
	}
	return draft, nil
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
