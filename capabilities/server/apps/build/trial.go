package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"platformserver/platform"
)

// ApplicationDrafts follows the original release descriptors, substituting
// every reachable saved draft, including changes to installed dependencies.
// The application boundary is its declared resources, never all tenant drafts.
func (b *Build) ApplicationDrafts(id string) ([]JointDraftRef, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	index := map[platform.AssetRef]JointDraftRef{}
	graph := map[platform.AssetRef][]platform.AssetRef{}
	var root platform.AssetRef
	add := func(kind platform.AssetKind, id, name string) {
		index[platform.AssetRef{App: ID, Kind: kind, Name: name}] = JointDraftRef{Kind: kind, ID: id}
	}
	recordAssets := func(assets []platform.ReleaseAsset, err error, draft JointDraftRef) error {
		if err != nil {
			return err
		}
		for _, a := range assets {
			graph[a.Ref] = a.Requires
			if _, ok := index[a.Ref]; !ok {
				index[a.Ref] = draft
			}
		}
		return nil
	}
	for _, o := range objects {
		if !o.Archived {
			add(platform.AssetObject, o.ID, TypeOf(o.Name))
			add(platform.AssetPage, o.ID, o.Name)
			index[platform.AssetRef{App: ID, Kind: platform.AssetPage, Name: o.Name}] = JointDraftRef{Kind: platform.AssetObject, ID: o.ID}
			for _, a := range objectActions(o, Entity(o)) {
				index[platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: a.Schema}] = JointDraftRef{Kind: platform.AssetObject, ID: o.ID}
			}
		}
	}
	for _, p := range pages {
		if !p.Archived {
			add(platform.AssetPage, p.ID, p.Name)
		}
	}
	for _, a := range apps {
		if !a.Archived {
			add(platform.AssetApp, a.ID, a.Name)
			if a.ID == id {
				root = platform.AssetRef{App: ID, Kind: platform.AssetApp, Name: a.Name}
			}
		}
	}
	for _, p := range processes {
		if !p.Archived {
			add(platform.AssetFlow, p.ID, TypeOf(p.Name))
		}
	}
	if root.Name == "" {
		return nil, fmt.Errorf("Application draft not found")
	}
	// These original owner entities supply only identity. Their own draft
	// compiler supplies dependencies when the traversal reaches that asset.
	caller := b.host.Automation(platform.Caller{}, ID)
	for _, entity := range b.Manifest().Entities {
		ref, err := draftAssetRef(entity.Type, "")
		if err != nil || recordKind(ref.Kind) {
			continue
		}
		rows, err := readDefinitionInventoryOf(caller, reflect.TypeOf(entity.Model))
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			var head struct {
				ID, Name string
				Archived bool
			}
			if err := json.Unmarshal(platform.Raw(row), &head); err != nil {
				return nil, err
			}
			if !head.Archived && head.Name != "" {
				rowKind := ref.Kind
				if entity.Type == TestPlanType {
					rowKind = testPlanKind(row.(TestPlan))
				}
				add(rowKind, head.ID, head.Name)
			}
		}
	}

	all := make([]JointDraftRef, 0, len(objects))
	for _, o := range objects {
		if !o.Archived {
			all = append(all, JointDraftRef{Kind: platform.AssetObject, ID: o.ID})
		}
	}
	savedEntities, savedMode := b.jointEntities, b.jointMode
	b.jointEntities = b.jointObjectEntities(objects, all)
	b.jointMode = true
	defer func() { b.jointEntities, b.jointMode = savedEntities, savedMode }()
	seen := map[platform.AssetRef]bool{}
	selected := map[JointDraftRef]bool{}
	queue := []platform.AssetRef{root}
	out := []JointDraftRef{}
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if seen[ref] {
			continue
		}
		seen[ref] = true
		draft, ok := index[ref]
		if !ok {
			continue
		} // immutable native dependency
		if !selected[draft] {
			if len(out) >= 128 {
				return nil, fmt.Errorf("Application trial exceeds 128 saved drafts")
			}
			selected[draft] = true
			out = append(out, draft)
			var assets []platform.ReleaseAsset
			var err error
			switch draft.Kind {
			case platform.AssetObject:
				i := slices.IndexFunc(objects, func(o Object) bool { return o.ID == draft.ID })
				o := objects[i]
				o.Published = published(o)
				assets, err = releaseAssets([]Object{o}, nil, nil, nil, b.Manifest().Version)
			case platform.AssetPage:
				i := slices.IndexFunc(pages, func(p Page) bool { return p.ID == draft.ID })
				p := pages[i]
				p.Published = published(p)
				assets, err = releaseAssets(nil, []Page{p}, nil, nil, b.Manifest().Version)
			case platform.AssetApp:
				i := slices.IndexFunc(apps, func(a Application) bool { return a.ID == draft.ID })
				a := apps[i]
				a.Published = published(a)
				assets, err = releaseAssets(nil, nil, []Application{a}, nil, b.Manifest().Version)
			case platform.AssetFlow:
				i := slices.IndexFunc(processes, func(p Process) bool { return p.ID == draft.ID })
				p := processes[i]
				p.Version++
				p.Published = published(p)
				p.Versions = append(slices.Clone(p.Versions), p.Published)
				p.State = "published"
				assets, err = releaseAssets(nil, nil, nil, []Process{p}, b.Manifest().Version)
			default:
				declarations := b.jointDeclarations
				if draft.Kind == platform.AssetEvaluation {
					plan, found := platform.Get[TestPlan](caller, draft.ID)
					if !found || plan.Archived {
						return nil, fmt.Errorf("saved evaluation plan is unavailable")
					}
					_, functions, _, functionRef, _, problem := b.functionDraftAssets(string(plan.Function))
					if problem != nil {
						return nil, problem
					}
					function, found := findReleaseAsset(functions, functionRef)
					if !found {
						return nil, fmt.Errorf("Draft asset is unavailable")
					}
					b.jointDeclarations = maps.Clone(declarations)
					if b.jointDeclarations == nil {
						b.jointDeclarations = map[platform.AssetRef]platform.ReleaseAsset{}
					}
					b.jointDeclarations[functionRef] = function
				}
				_, after, _, next, _, e := b.singleDraftAssets(draft.Kind, draft.ID)
				b.jointDeclarations = declarations
				err = e
				if err == nil {
					asset, ok := findReleaseAsset(after, next)
					if !ok {
						return nil, fmt.Errorf("Draft asset is unavailable")
					}
					assets = []platform.ReleaseAsset{asset}
				}
			}
			if err = recordAssets(assets, err, draft); err != nil {
				return nil, err
			}
		}
		// Generated actions/pages can lead back to the same object draft.
		if deps, ok := graph[ref]; ok {
			queue = append(queue, deps...)
		} else if draft.Kind == platform.AssetObject {
			queue = append(queue, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: TypeOf(objects[slices.IndexFunc(objects, func(o Object) bool { return o.ID == draft.ID })].Name)})
		}
	}
	return out, nil
}
