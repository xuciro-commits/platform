package build

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"platformserver/platform"
)

func (b *Build) DraftReleaseAssetsMulti(drafts []JointDraftRef, member platform.Member) (JointDraftAssets, error) {
	var result JointDraftAssets
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
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return result, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return result, err
	}
	before, err := b.ReleaseAssets()
	if err != nil {
		return result, err
	}
	restore, restoreMode, restoreDeclarations := b.jointEntities, b.jointMode, b.jointDeclarations
	if len(drafts) > 1 {
		b.jointMode = true
	}
	b.jointEntities = b.jointObjectEntities(objects, drafts)
	b.jointDeclarations = maps.Clone(b.jointDeclarations)
	if b.jointDeclarations == nil {
		b.jointDeclarations = map[platform.AssetRef]platform.ReleaseAsset{}
	}
	defer func() { b.jointEntities, b.jointMode, b.jointDeclarations = restore, restoreMode, restoreDeclarations }()

	inv := draftInventories{objects: objects, pages: pages, apps: apps, processes: processes}
	overrides := map[platform.AssetRef]platform.ReleaseAsset{}
	removed := map[platform.AssetRef]bool{}
	declarations := slices.Clone(drafts)
	priority := func(kind platform.AssetKind) int {
		switch kind {
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
	for _, draft := range declarations {
		if !recordKind(draft.Kind) {
			_, after, prior, next, hadPrior, err := b.singleDraftAssets(draft.Kind, draft.ID)
			if err != nil {
				return result, err
			}
			asset, ok := findReleaseAsset(after, next)
			if !ok {
				return result, fmt.Errorf("draft %s did not produce %s", draft.ID, next)
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
	for _, draft := range drafts {
		if !recordKind(draft.Kind) {
			continue
		}
		prior, next, hadPrior, err := b.applyRecordDraft(&inv, draft.Kind, draft.ID, member)
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
			return result, err
		}
	}
	recordAssets, err := releaseAssets(inv.objects, inv.pages, inv.apps, inv.processes, b.Manifest().Version)
	if err == nil {
		recordAssets, err = b.bindConfirmationAssets(recordAssets)
	}
	if err == nil {
		recordAssets, err = b.bindDispatchAssets(recordAssets)
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
	result.Before, result.After = before, after
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
	objects   []Object
	pages     []Page
	apps      []Application
	processes []Process
}

func (b *Build) applyRecordDraft(inv *draftInventories, kind platform.AssetKind, id string, member platform.Member) (prior, next platform.AssetRef, hadPrior bool, err error) {
	joint := b.jointMode
	switch kind {
	case platform.AssetFlow:
		i := slices.IndexFunc(inv.processes, func(p Process) bool { return p.ID == id })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft process %q not found", id)
		}
		record := inv.processes[i]
		if was, ok := ReadPublication[Process](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(was.Name)}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(record.Name)}
		source := b.installed[record.Object]
		models := []platform.Entity{}
		for _, object := range inv.objects {
			if _, selected := b.jointEntities[TypeOf(object.Name)]; !selected || object.Archived {
				continue
			}
			model := Entity(object)
			models = append(models, model)
			if model.Type == record.Object {
				source = model
			}
		}
		if problem := b.checkFlow(record, source, models...); problem != nil {
			err = fmt.Errorf("%s", problem.Message)
		} else if record.Version >= 64 {
			err = fmt.Errorf("a process may retain at most 64 published versions")
		} else if b.host.BuildProcesses() == nil {
			err = fmt.Errorf("this tenant runs no processes")
		} else {
			record.Version++
			if record.Every != "" || record.Continuous != nil && record.Continuous.Intake != nil {
				record.Scheduler = member.ID
			} else {
				record.Scheduler = ""
			}
			if !joint {
				err = b.host.BuildProcesses().Validate(b, b.flowOf(record))
			}
			if err == nil {
				record.Published = published(record)
				record.Versions = append(slices.Clone(record.Versions), record.Published)
				record.State = "published"
				inv.processes[i] = record
			}
		}
	case platform.AssetObject:
		i := slices.IndexFunc(inv.objects, func(o Object) bool { return o.ID == id && !o.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft object %q not found", id)
		}
		record := inv.objects[i]
		if was, ok := ReadPublication[Object](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(was.Name)}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(record.Name)}
		if err = b.captureExtensionSource(&record); err != nil {
			return prior, next, hadPrior, err
		}
		if err = b.captureObjectSchema(&record); err != nil {
			return prior, next, hadPrior, err
		}
		if err = b.Check(record, record.ID, b.host.Caller(platform.Caller{}, member, ID).Rules()); err == nil {
			err = b.validateObjectInstallation(b.host.Caller(platform.Caller{}, member, ID), record)
		}
		if err == nil {
			inv.objects[i].Published = published(record)
		}
	case platform.AssetPage:
		i := slices.IndexFunc(inv.pages, func(p Page) bool { return p.ID == id && !p.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft page %q not found", id)
		}
		record := inv.pages[i]
		if was, ok := ReadPublication[Page](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: was.Name}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: record.Name}
		if err = b.checkPage(record); err == nil && !joint {
			err = b.host.ValidateInstallPage(descriptor(record))
		}
		if err == nil {
			err = retainPagePublication(&record)
			inv.pages[i] = record
		}
	case platform.AssetApp:
		i := slices.IndexFunc(inv.apps, func(a Application) bool { return a.ID == id && !a.Archived })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft application %q not found", id)
		}
		record := inv.apps[i]
		if was, ok := ReadPublication[Application](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: was.Name}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: record.Name}
		if err = b.checkApplication(record); err == nil && !joint {
			err = b.host.ValidateInstallApplication(applicationDescriptor(record))
		}
		if err == nil {
			inv.apps[i].Published = published(record)
		}
	default:
		err = fmt.Errorf("draft kind %q cannot be previewed", kind)
	}
	return prior, next, hadPrior, err
}

func (b *Build) singleDraftAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	if ValueIntegrationKind(kind) {
		return b.integrationDraftAssets(kind, id)
	}
	if kind == platform.AssetEvaluation || kind == platform.AssetTestPlan {
		return b.testPlanDraftAssets(id)
	}
	if kind == platform.AssetSampleSet {
		return b.sampleSetDraftAssets(id)
	}
	if k := definitionKindOf(kind); k != nil {
		return k.draftAssets(b, id)
	}
	if kind == platform.AssetCompute {
		if _, ok := platform.Get[Table](b.host.Automation(platform.Caller{}, ID), id); ok {
			return b.tableDraftAssets(id)
		}
		return b.CodeDraftAssets(id)
	}
	if kind == platform.AssetAlert {
		return b.alertDraftAssets(id)
	}
	if kind == platform.AssetFunction {
		return functionKind.draftAssets(b, id)
	}
	if kind == platform.AssetAgent {
		return b.agentDraftAssets(id)
	}
	if !recordKind(kind) {
		return nil, nil, prior, next, false, fmt.Errorf("draft kind %q cannot be previewed", kind)
	}
	assets, err := b.DraftReleaseAssetsMulti([]JointDraftRef{{Kind: kind, ID: id}}, platform.Member{})
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
	if !recordKind(kind) {
		return nil, fmt.Errorf("%s drafts have no record dependencies to select", kind)
	}
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	find := func(want platform.AssetKind, name string) (JointDraftRef, bool) {
		switch want {
		case platform.AssetObject:
			for _, o := range objects {
				if o.Published == "" && (o.Name == name || TypeOf(o.Name) == name) {
					return JointDraftRef{Kind: want, ID: o.ID}, true
				}
			}
		case platform.AssetPage:
			for _, p := range pages {
				if p.Published == "" && (p.Name == name || TypeOf(p.Name) == name) {
					return JointDraftRef{Kind: want, ID: p.ID}, true
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
		case platform.AssetObject:
			i := slices.IndexFunc(objects, func(o Object) bool { return o.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved object draft %s", next.ID)
			}
			for _, field := range objects[i].Fields {
				if field.Type == "reference" && field.Ref != "" {
					dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], field.Ref)
				}
			}
		case platform.AssetPage:
			i := slices.IndexFunc(pages, func(p Page) bool { return p.ID == next.ID })
			if i < 0 {
				return nil, fmt.Errorf("no saved page draft %s", next.ID)
			}
			if pages[i].Object != "" {
				dependencies[platform.AssetObject] = append(dependencies[platform.AssetObject], pages[i].Object)
			}
		case platform.AssetApp:
			i := slices.IndexFunc(apps, func(a Application) bool { return a.ID == next.ID })
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
			i := slices.IndexFunc(processes, func(p Process) bool { return p.ID == next.ID })
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

func (b *Build) DraftReleaseAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return b.singleDraftAssets(kind, id)
}

type JointDraftAssets struct {
	Before []platform.ReleaseAsset `json:"-"`
	After  []platform.ReleaseAsset `json:"-"`
	Priors []platform.AssetRef     `json:"-"`
	Nexts  []platform.AssetRef     `json:"-"`
}

const JointDraftLimit = 128

type JointDraftRef struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}

type DraftChange struct {
	Ref        platform.AssetRef  `json:"ref"`
	Type       string             `json:"type"`
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	Variant    string             `json:"variant,omitempty"`
	Published  bool               `json:"published"`
	Changed    platform.Stamp     `json:"changed"`
	Owner      string             `json:"owner"`
	Fields     []DraftChangeField `json:"fields"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type DraftChangeField struct {
	Name   string          `json:"name"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

func CompatibleObjectExpansion(previous, next Object) bool {
	if previous.Name != next.Name || len(next.Fields) <= len(previous.Fields) || len(previous.States) != len(next.States) || len(previous.Access) != len(next.Access) {
		return false
	}
	if !reflect.DeepEqual(previous.Constraints, next.Constraints) {
		return false
	}
	for i, state := range previous.States {
		if !reflect.DeepEqual(state, next.States[i]) {
			return false
		}
	}
	for i, access := range previous.Access {
		if !reflect.DeepEqual(access, next.Access[i]) {
			return false
		}
	}
	equal := func(a, b any) bool {
		x, xe := json.Marshal(a)
		y, ye := json.Marshal(b)
		return xe == nil && ye == nil && string(x) == string(y)
	}
	for _, field := range previous.Fields {
		index := slices.IndexFunc(next.Fields, func(other Field) bool { return other.Name == field.Name })
		if index < 0 || !equal(field, next.Fields[index]) {
			return false
		}
	}
	for _, action := range previous.Actions {
		index := slices.IndexFunc(next.Actions, func(other Action) bool { return other.Name == action.Name })
		if index < 0 || !equal(action, next.Actions[index]) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.ValueType != nil || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}
