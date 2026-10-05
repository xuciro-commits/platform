package build

import (
	"fmt"
	"reflect"
	"slices"

	"platformserver/platform"
)

// JointDraftRef names one saved draft in a joint candidate (ADR-0048 D1).
type JointDraftRef struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}

// JointDraftAssets is one candidate image built from several saved drafts at
// once: what is installed today, and what the tenant would run if this exact
// selection were saved and activated.
type JointDraftAssets struct {
	Before []platform.ReleaseAsset `json:"-"`
	After  []platform.ReleaseAsset `json:"-"`
	Priors []platform.AssetRef     `json:"-"`
	Nexts  []platform.AssetRef     `json:"-"`
}

// recordKind reports whether a draft is substituted in the shared definition
// inventories (objects, pages, applications, flows) rather than in its own
// owner's asset snapshot.
func recordKind(kind platform.AssetKind) bool {
	switch kind {
	case platform.AssetObject, platform.AssetPage, platform.AssetApp, platform.AssetFlow:
		return true
	}
	return false
}

// lookupEntity resolves an entity type with the joint candidate's draft objects
// in view. Outside a joint build it is exactly the host's installed registry.
func (b *Build) lookupEntity(typ string) (platform.EntityInfo, bool) {
	if info, ok := b.jointEntities[typ]; ok {
		return info, true
	}
	if b.host == nil {
		return platform.EntityInfo{}, false
	}
	return b.host.Entity(typ)
}

// jointObjectEntities declares what each selected draft object would be
// installed as, using the same descriptor the publisher installs.
func (b *Build) jointObjectEntities(objects []Object, drafts []JointDraftRef) map[string]platform.EntityInfo {
	joint := map[string]platform.EntityInfo{}
	for _, draft := range drafts {
		if draft.Kind != platform.AssetObject {
			continue
		}
		i := slices.IndexFunc(objects, func(o Object) bool { return o.ID == draft.ID && !o.Archived })
		if i < 0 {
			continue
		}
		record := objects[i]
		info, err := platform.Describe(ID, Entity(record), func(reflect.Type) string { return "" })
		if err != nil {
			continue // the draft's own check reports why it cannot be published
		}
		joint[info.Type] = info
	}
	return joint
}

// DraftReleaseAssetsMulti substitutes every selected saved draft in one
// candidate image. Simple kinds keep their owner's existing single-draft
// checks; record kinds are substituted through one shared inventory so a page
// or flow may reference an object that is itself only a draft here.
func (b *Build) DraftReleaseAssetsMulti(drafts []JointDraftRef) (JointDraftAssets, error) {
	var result JointDraftAssets
	if len(drafts) == 0 {
		return result, fmt.Errorf("select at least one saved draft")
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
	// The joint candidate's own object drafts answer entity lookups, so a page,
	// flow or reference may name an object that is not installed yet.
	restore, restoreMode := b.jointEntities, b.jointMode
	if len(drafts) > 1 {
		b.jointMode = true
	}
	b.jointEntities = b.jointObjectEntities(objects, drafts)
	defer func() { b.jointEntities, b.jointMode = restore, restoreMode }()

	inv := draftInventories{objects: objects, pages: pages, apps: apps, processes: processes}
	overrides := map[platform.AssetRef]platform.ReleaseAsset{}
	removed := map[platform.AssetRef]bool{}
	for _, draft := range drafts {
		if !recordKind(draft.Kind) {
			// The owner's own single-draft path validates and substitutes it.
			_, after, prior, next, hadPrior, err := b.singleDraftAssets(draft.Kind, draft.ID)
			if err != nil {
				return result, err
			}
			asset, ok := findReleaseAsset(after, next)
			if !ok {
				return result, fmt.Errorf("draft %s did not produce %s", draft.ID, next)
			}
			overrides[next] = asset
			if hadPrior && prior != next {
				removed[prior] = true
			}
			if hadPrior {
				result.Priors = append(result.Priors, prior)
			}
			result.Nexts = append(result.Nexts, next)
			continue
		}
		prior, next, hadPrior, err := b.applyRecordDraft(&inv, draft.Kind, draft.ID)
		// A refused draft still names the asset it would have replaced: the
		// review can say what is installed now beside why the draft failed.
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
	if err != nil {
		return result, err
	}
	after := recordAssets
	// releaseAssets rebuilds records and their generated actions together; the
	// installed snapshot contributes the owners' remaining assets, with the
	// selected drafts substituted.
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

// draftInventories are the builder's four record families as one candidate
// sees them: installed records with the selected drafts substituted in.
type draftInventories struct {
	objects   []Object
	pages     []Page
	apps      []Application
	processes []Process
}

// applyRecordDraft substitutes one saved record draft into the shared
// inventories. Validation runs against the joint candidate's entities, and the
// host's own install checks re-run when the candidate is installed; the
// builder-level checks that need only the candidate's image run here.
func (b *Build) applyRecordDraft(inv *draftInventories, kind platform.AssetKind, id string) (prior, next platform.AssetRef, hadPrior bool, err error) {
	joint := b.jointMode
	switch kind {
	case platform.AssetFlow:
		i := slices.IndexFunc(inv.processes, func(p Process) bool { return p.ID == id })
		if i < 0 {
			return prior, next, false, fmt.Errorf("draft process %q not found", id)
		}
		record := inv.processes[i]
		if was, ok := wasPublished[Process](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(was.Name)}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(record.Name)}
		if problem := b.checkFlow(record); problem != nil {
			err = fmt.Errorf("%s", problem.Message)
		} else if record.Version >= 64 {
			err = fmt.Errorf("a process may retain at most 64 published versions")
		} else if b.host.Processes() == nil {
			err = fmt.Errorf("this tenant runs no processes")
		} else {
			record.Version++
			err = b.host.Processes().Validate(b, b.flowOf(record))
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
		if was, ok := wasPublished[Object](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(was.Name)}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(record.Name)}
		if err = b.check(record, record.ID); err == nil {
			err = b.validateObjectInstallation(record)
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
		if was, ok := wasPublished[Page](record.Published); ok {
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
		if was, ok := wasPublished[Application](record.Published); ok {
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

// singleDraftAssets is the owner's original one-draft comparison, unchanged for
// simple kinds and routed through the shared record substitution otherwise.
func (b *Build) singleDraftAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	if kind == platform.AssetPropertyType {
		return b.propertyTypeDraftAssets(id)
	}
	if kind == platform.AssetLinkType {
		return b.linkTypeDraftAssets(id)
	}
	if kind == platform.AssetQuery {
		return b.queryDraftAssets(id)
	}
	if kind == platform.AssetCompute {
		return b.CodeDraftAssets(id)
	}
	if kind == platform.AssetFunction {
		return b.functionDraftAssets(id)
	}
	assets, err := b.DraftReleaseAssetsMulti([]JointDraftRef{{Kind: kind, ID: id}})
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

// ReferencedDrafts lists the saved record drafts a chosen draft depends on and
// that are not installed yet, so a builder can select them for one joint
// candidate instead of publishing dependencies first (ADR-0048 D1).
//
// Records only — objects, pages, applications and workflows. A draft whose
// non-record dependency (an AI function, query or code function) is still a
// draft is reported by that owner's own check, and the builder selects it by
// hand. A dependency that is already published needs no entry: the candidate
// closes over its installed version.
func (b *Build) ReferencedDrafts(kind platform.AssetKind, id string) ([]JointDraftRef, error) {
	if id == "" {
		return nil, fmt.Errorf("a saved draft is required")
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
	for len(queue) > 0 && len(result) < 64 {
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
				continue // the owner that owns that draft reports its own gap
			}
			for _, name := range names {
				ref, ok := find(want, name)
				if !ok || seen[ref] {
					continue
				}
				seen[ref] = true
				result = append(result, ref)
				queue = append(queue, ref)
			}
		}
	}
	return result, nil
}
