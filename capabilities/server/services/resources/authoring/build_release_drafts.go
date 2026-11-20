package authoring

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"platformserver/kernel/protocol"
	"platformserver/services/ontology"
	"slices"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/publication"
)

func (b *Build) DraftReleaseAssetsMulti(drafts []JointDraftRef, member platform.Member, review bool) (result JointDraftAssets, err error) {
	if len(drafts) == 0 {
		return result, fmt.Errorf("select at least one saved draft")
	}
	if len(drafts) > JointDraftLimit {
		return result, fmt.Errorf("a joint candidate accepts at most %d drafts", JointDraftLimit)
	}
	seen := map[platform.AssetRef]bool{}
	for _, draft := range drafts {
		ref := platform.AssetRef{App: ID, Kind: draft.Kind, Name: draft.ID}
		if draft.ID == "" || seen[ref] {
			return result, fmt.Errorf("a draft must be named once")
		}
		seen[ref] = true
	}
	checkIndexes := make(map[JointDraftRef]int, len(drafts))
	for _, draft := range drafts {
		checkIndexes[draft] = len(result.Checks)
		result.Checks = append(result.Checks, JointDraftCheck{Kind: draft.Kind, ID: draft.ID})
	}
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return result, err
	}
	processes, err := b.automation.Inventory()
	if err != nil {
		return result, err
	}
	before, err := b.ReleaseAssets()
	if err != nil {
		return result, err
	}
	result.Before = before
	restore, restoreMode, restoreDeclarations := b.jointEntities, b.jointMode, b.jointDeclarations
	if len(drafts) > 1 {
		b.jointMode = true
	}
	b.jointEntities = b.jointObjectEntities(objects, drafts)
	b.jointDeclarations = maps.Clone(b.jointDeclarations)
	if b.jointDeclarations == nil {
		b.jointDeclarations = map[platform.AssetRef]platform.ReleaseAsset{}
	}
	defer func() {
		if review {
			for i := range result.Checks {
				check := &result.Checks[i]
				change, found, problem := b.DraftChangeOf(member, check.Kind, check.ID)
				if found {
					check.Change = &change
					if change.Diagnostic != "" {
						problem = fmt.Errorf("%s", change.Diagnostic)
					}
				}
				if problem != nil && !slices.Contains(check.Issues, problem.Error()) {
					check.Issues = append(check.Issues, problem.Error())
					err = errors.Join(err, problem)
				}
			}
		}
		b.jointEntities, b.jointMode, b.jointDeclarations = restore, restoreMode, restoreDeclarations
	}()

	objectDrafts := ontology.ObjectDraftPublication(objects, ontology.ObjectDraftEnvironment{
		Host: b.host, Authority: b.authority, BindActions: b.bindObjectActions,
		CaptureExtension: b.captureExtensionSource, CaptureSchema: b.captureObjectSchema,
		Check: b.Check, Validate: b.validateObjectInstallation,
	})
	draftHooks := slices.Clone(b.publications)
	for i := range draftHooks {
		if slices.Contains(draftHooks[i].Kinds, platform.AssetObject) {
			draftHooks[i].Closure = objectDrafts.Closure
		}
	}
	inv := draftInventories{objects: objects, pages: pages, apps: apps, processes: processes}
	overrides := map[platform.AssetRef]platform.ReleaseAsset{}
	removed := map[platform.AssetRef]bool{}
	declarations := slices.Clone(drafts)
	priority := func(kind platform.AssetKind) int {
		switch kind {
		case platform.AssetAction:
			return 3
		case platform.AssetEvaluation:
			return 2
		case platform.AssetAgent:
			return 1
		}
		return 0
	}
	slices.SortStableFunc(declarations, func(a, b JointDraftRef) int {
		return cmp.Compare(priority(a.Kind), priority(b.Kind))
	})
	var problems []error
	for _, draft := range declarations {
		if !recordKind(draft.Kind) {
			_, after, prior, next, hadPrior, err := b.singleDraftAssets(draft.Kind, draft.ID)
			check := &result.Checks[checkIndexes[draft]]
			check.Checked = true
			if err != nil {
				check.Issues = append(check.Issues, err.Error())
				problems = append(problems, err)
				continue
			}
			asset, ok := findReleaseAsset(after, next)
			if !ok {
				err = fmt.Errorf("draft %s did not produce %s", draft.ID, next)
				check.Issues = append(check.Issues, err.Error())
				problems = append(problems, err)
				continue
			}
			overrides[next] = asset
			b.jointDeclarations[next] = asset
			if hadPrior && prior != next {
				removed[prior] = true
			}
			if hadPrior {
				result.Priors = append(result.Priors, prior)
			}
			result.Nexts = append(result.Nexts, next)
			continue
		}
	}
	for _, draft := range declarations {
		if !recordKind(draft.Kind) {
			continue
		}
		prior, next, hadPrior, err := b.applyRecordDraft(&inv, draftHooks, draft.Kind, draft.ID, member)
		check := &result.Checks[checkIndexes[draft]]
		check.Checked = true
		if hadPrior {
			result.Priors = append(result.Priors, prior)
			if prior != next {
				removed[prior] = true
			}
		}
		if next != (platform.AssetRef{}) {
			result.Nexts = append(result.Nexts, next)
		}
		if err != nil {
			check.Issues = append(check.Issues, err.Error())
			problems = append(problems, err)
		}
	}
	if len(problems) > 0 {
		return result, errors.Join(problems...)
	}
	recordAssets, err := b.releaseAssets(inv.objects, inv.pages, inv.apps, inv.processes, b.Manifest().Version)
	if err == nil {
		recordAssets, err = b.data.BindReleaseAssets(recordAssets)
	}
	if err != nil {
		return result, err
	}
	after := recordAssets
	rebuilt := map[platform.AssetRef]bool{}
	for _, asset := range recordAssets {
		rebuilt[asset.Ref] = true
	}
	for _, asset := range before {
		if substitute, ok := overrides[asset.Ref]; ok {
			after = append(after, substitute)
			delete(overrides, asset.Ref)
			continue
		}
		if removed[asset.Ref] || rebuilt[asset.Ref] {
			continue
		}
		if asset.Ref.Kind == platform.AssetAction {
			var action struct {
				Target string `json:"target"`
			}
			if json.Unmarshal(asset.Body, &action) != nil {
				return result, fmt.Errorf("installed action %s has no valid target", asset.Ref)
			}
			if rebuilt[platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetObject, Name: action.Target}] {
				continue
			}
		}
		after = append(after, asset)
	}
	for _, substitute := range overrides {
		after = append(after, substitute)
	}
	result.After = after
	return result, nil
}

func findReleaseAsset(assets []platform.ReleaseAsset, ref platform.AssetRef) (platform.ReleaseAsset, bool) {
	i := slices.IndexFunc(assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return platform.ReleaseAsset{}, false
	}
	return assets[i], true
}

type draftInventories struct {
	objects   []ontology.Object
	pages     []protocol.Page
	apps      []protocol.Application
	processes []protocol.Process
}

func (b *Build) applyRecordDraft(inv *draftInventories, table []publication.Hooks, kind platform.AssetKind, id string, member platform.Member) (prior, next platform.AssetRef, hadPrior bool, err error) {
	if hooks, found := publication.Kind(table, kind); found {
		closure, err := hooks.Closure(b.host.Automation(platform.Caller{}, ID), id, member)
		if err != nil {
			return closure.Prior, closure.Next, closure.HasPrior, err
		}
		for _, dependency := range closure.Requires {
			_, _, _, err = b.applyRecordDraft(inv, table, dependency.Kind, dependency.ID, member)
			if err != nil {
				break
			}
		}
		return closure.Prior, closure.Next, closure.HasPrior, err
	}
	joint := b.jointMode
	switch kind {
	case platform.AssetFlow:
		i := slices.IndexFunc(inv.processes, func(p protocol.Process) bool { return p.ID == id })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft process %q not found", id)
		}
		record := inv.processes[i]
		source := b.installed[record.Object]
		models := []platform.RecordType{}
		for _, object := range inv.objects {
			if _, selected := b.jointEntities[object.Type()]; !selected || object.Archived {
				continue
			}
			model := Entity(object)
			models = append(models, model)
			if model.Type == record.Object {
				source = model
			}
		}
		var prepared protocol.Process
		prepared, prior, next, hadPrior, err = b.automation.CheckDraft(platform.Caller{}, record, source, models, member, !joint)
		if err == nil {
			inv.processes[i] = prepared
		}
	case platform.AssetPage:
		i := slices.IndexFunc(inv.pages, func(p protocol.Page) bool { return p.ID == id && !p.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft page %q not found", id)
		}
		var record protocol.Page
		record, prior, next, hadPrior, err = b.apps.CheckPageDraft(inv.pages[i], !joint)
		if err == nil {
			inv.pages[i] = record
		}
	case platform.AssetApp:
		i := slices.IndexFunc(inv.apps, func(a protocol.Application) bool { return a.ID == id && !a.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft application %q not found", id)
		}
		var record protocol.Application
		record, prior, next, hadPrior, err = b.apps.CheckApplicationDraft(inv.apps[i], !joint)
		if err == nil {
			inv.apps[i] = record
		}
	default:
		err = fmt.Errorf("draft kind %q cannot be previewed", kind)
	}
	return prior, next, hadPrior, err
}

func (b *Build) singleDraftAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	if dataowner.IsIntegration(kind) {
		before, err = b.ReleaseAssets()
		if err != nil {
			return
		}
		after, prior, next, hadPrior, err = b.data.DraftAssets(before, kind, id)
		return
	}
	if k := definitionKindOf(kind); k != nil {
		return k.draftAssets(b, id)
	}
	if kind == platform.AssetFunction {
		return b.functions.DraftAssets(id, b.ReleaseAssets)
	}
	if hooks, found := publication.Kind(b.publications, kind); found && hooks.VersionedDraft != nil {
		closure, err := hooks.VersionedDraft(id, b.ReleaseAssets)
		return closure.Before, closure.After, closure.Prior, closure.Next, closure.HasPrior, err
	}
	if !recordKind(kind) {
		return nil, nil, prior, next, false, fmt.Errorf("draft kind %q cannot be previewed", kind)
	}
	assets, err := b.DraftReleaseAssetsMulti([]JointDraftRef{{Kind: kind, ID: id}}, platform.Member{}, false)
	if err != nil {
		return nil, nil, prior, next, false, err
	}
	if len(assets.Priors) > 0 {
		prior, hadPrior = assets.Priors[0], true
	}
	if len(assets.Nexts) > 0 {
		next = assets.Nexts[0]
	}
	return assets.Before, assets.After, prior, next, hadPrior, nil
}

func (b *Build) ReferencedDrafts(kind platform.AssetKind, id, owner string) ([]JointDraftRef, error) {
	if id == "" {
		return nil, fmt.Errorf("a saved draft is required")
	}
	if kind == platform.AssetInterface {
		return nil, nil
	}
	if kind == platform.AssetApp {
		changes, err := b.SavedDraftChanges(b.host.Automation(platform.Caller{}, ID), "", "", "")
		if err != nil {
			return nil, err
		}
		selected := map[JointDraftRef]bool{{Kind: kind, ID: id}: true}
		for _, change := range changes {
			if owner == "" || change.Owner == owner || !change.Published {
				selected[JointDraftRef{Kind: change.Ref.Kind, ID: change.ID}] = true
			}
		}
		drafts, err := b.applicationDrafts(id, selected)
		return slices.DeleteFunc(drafts, func(draft JointDraftRef) bool { return draft.Kind == kind && draft.ID == id }), err
	}
	if !recordKind(kind) && kind != platform.AssetObjectTypeGroup {
		return nil, fmt.Errorf("%s drafts have no record dependencies to select", kind)
	}
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.automation.Inventory()
	if err != nil {
		return nil, err
	}
	find := func(want platform.AssetKind, name string) (JointDraftRef, bool) {
		switch want {
		case platform.AssetObject:
			for _, o := range objects {
				if o.Published == "" && (o.Name == name || o.Type() == name) {
					return JointDraftRef{Kind: want, ID: o.ID}, true
				}
			}
		case platform.AssetPage:
			for _, p := range pages {
				if p.Published == "" && (p.Name == name || TypeOf(p.Name) == name) {
					return JointDraftRef{Kind: want, ID: p.ID}, true
				}
			}
		case platform.AssetApp:
			for _, app := range apps {
				if app.Published == "" && app.Name == name {
					return JointDraftRef{Kind: want, ID: app.ID}, true
				}
			}
		case platform.AssetFlow:
			for _, p := range processes {
				if p.Published == "" && (p.Name == name || TypeOf(p.Name) == name) {
					return JointDraftRef{Kind: want, ID: p.ID}, true
				}
			}
		}
		return JointDraftRef{}, false
	}
	var result []JointDraftRef
	seen := map[JointDraftRef]bool{{Kind: kind, ID: id}: true}
	queue := []JointDraftRef{{Kind: kind, ID: id}}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		dependencies := map[platform.AssetKind][]string{}
		switch next.Kind {
		case platform.AssetObjectTypeGroup:
			group, ok := platform.Get[ontology.ObjectTypeGroup](b.host.Automation(platform.Caller{}, ID), next.ID)
			if !ok || group.Archived {
				return nil, fmt.Errorf("no saved object type group draft %s", next.ID)
			}
			for _, member := range group.Members {
				if member.App == ID {
					dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], member.Name)
				}
			}
		case platform.AssetObject:
			i := slices.IndexFunc(objects, func(o ontology.Object) bool { return o.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved object draft %s", next.ID)
			}
			if view := objects[i].ObjectView; view != nil {
				for _, ref := range []platform.AssetRef{view.Application, view.Page} {
					if ref.App == ID {
						dependencies[ref.Kind] = append(dependencies[ref.Kind], ref.Name)
					}
				}
			}
			for _, field := range objects[i].Fields {
				if field.Type == "reference" && field.Ref != "" {
					dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], field.Ref)
				}
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p protocol.Page) bool { return p.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved page draft %s", next.ID)
			}
			if pages[i].Object != "" {
				dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], pages[i].Object)
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a protocol.Application) bool { return a.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved application draft %s", next.ID)
			}
			for _, name := range apps[i].Pages {
				dependencies[platform.AssetPage] = append(dependencies[platform.AssetPage], name)
			}
			for _, resource := range apps[i].Resources {
				dependencies[resource.Kind] = append(dependencies[resource.Kind], resource.Name)
			}
		case platform.AssetFlow:
			i := slices.IndexFunc(processes, func(p protocol.Process) bool { return p.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved workflow draft %s", next.ID)
			}
			if processes[i].Object != "" {
				dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], processes[i].Object)
			}
		}
		for want, names := range dependencies {
			if !recordKind(want) {
				continue
			}
			for _, name := range names {
				ref, ok := find(want, name)
				if !ok || seen[ref] {
					continue
				}
				if len(seen) >= JointDraftLimit {
					return nil, fmt.Errorf("a joint candidate accepts at most %d drafts", JointDraftLimit)
				}
				seen[ref] = true
				result = append(result, ref)
				queue = append(queue, ref)
			}
		}
	}
	return result, nil
}

type JointDraftAssets = publication.JointDraftAssets
type JointDraftCheck = publication.JointDraftCheck
type DraftChange = publication.DraftChange
type DraftChangeField = publication.DraftChangeField

const JointDraftLimit = publication.DraftLimit

type JointDraftRef struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}
