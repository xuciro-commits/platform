package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

type releaseExecution struct {
	*Build
	catalog *platform.Catalog
}

func (b *releaseExecution) Manifest() platform.Manifest {
	m := b.Build.Manifest()
	m.Actions = b.catalog
	return m
}

func (b *Build) retainedCodeObject(asset platform.ReleaseAsset) (bool, error) {
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
		return false, nil
	}
	manifest := b.Manifest()
	native := slices.ContainsFunc(manifest.Entities, func(entity platform.Entity) bool {
		_, controlled := b.installed[entity.Type]
		return !controlled && entity.Type == asset.Ref.Name
	})
	if !native {
		return false, nil
	}
	if b.host == nil {
		return false, fmt.Errorf("immutable code asset %s has no original validator", asset.Ref)
	}
	if err := b.host.ValidateCodeAsset(asset); err != nil {
		return false, err
	}
	return true, nil
}

func (b *Build) ForkReleaseExecution(assets []platform.ReleaseAsset) (platform.App, error) {
	app, err := b.ForkDecision()
	if err != nil {
		return nil, err
	}
	bound := app.(*Build)
	bound.installed = map[string]platform.Entity{}
	bound.queries = map[string]Query{}
	bound.interfaces = map[string]InterfaceRecord{}
	bound.jointEntities = map[string]platform.EntityInfo{}
	bound.jointMode = true
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetQuery {
			query, err := queryKind.fromAsset(bound, asset)
			if err != nil {
				return nil, err
			}
			bound.queries[query.Name] = query
		}
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetInterface {
			shape, err := interfaceKind.fromAsset(bound, asset)
			if err != nil {
				return nil, err
			}
			bound.interfaces[shape.Name] = shape
		}
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetAgent {
			agent, err := decodeAgentAsset(asset)
			if err != nil {
				return nil, err
			}
			bound.agents[agent.Name] = agent
		}
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		code, err := b.retainedCodeObject(asset)
		if err != nil {
			return nil, err
		}
		if code {
			continue
		}
		var object Object
		if asset.SourceVersion != ValueDefinitionVersion || json.Unmarshal(asset.Body, &object) != nil || TypeOf(object.Name) != asset.Ref.Name {
			return nil, fmt.Errorf("unsupported retained object descriptor %s", asset.Ref)
		}
		info, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		if problem := checkObjectSchemas(object); problem != nil {
			return nil, problem
		}
		if err != nil {
			return nil, err
		}
		bound.jointEntities[info.Type] = info
	}
	var actions []platform.Action
	for _, asset := range assets {
		if asset.Ref.App != ID {
			continue
		}
		if asset.Ref.Kind == platform.AssetObject {
			code, err := b.retainedCodeObject(asset)
			if err != nil {
				return nil, err
			}
			if code {
				continue
			}
			var object Object
			if err := json.Unmarshal(asset.Body, &object); err != nil {
				return nil, err
			}
			if err := mapDispatchIntegrations(&object, b.data.IntegrationID); err != nil {
				return nil, err
			}
			entity := bound.entity(object)
			bound.installed[entity.Type] = entity
			actions = append(actions, objectActions(object, entity)...)
		}
	}
	return &releaseExecution{Build: bound, catalog: b.ledger.Catalog.WithActions(actions)}, nil
}

var _ platform.ReleaseExecutionOwner = (*Build)(nil)

func (b *Build) checkSystemDefinitionWrite(s *pb.Submission) *kernel.Error {
	typ, id := s.GetTarget().GetType(), s.GetTarget().GetId()
	schema := s.GetSchema().GetName()
	if schema != typ+".create" && schema != typ+".edit" && schema != typ+".archive" && !slices.Contains(b.AcceptedPublicationSchemas(), schema) {
		return nil
	}
	_, err := draftAssetRef(typ, "")
	if err != nil {
		return nil
	}
	var proposed struct{ Name string }
	_ = json.Unmarshal(s.GetPayload(), &proposed)
	names := []string{proposed.Name}
	if record, ok := b.host.Record(typ + "/" + id); ok {
		var current struct{ Name string }
		_ = json.Unmarshal(platform.Raw(record), &current)
		names = append(names, current.Name)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		original, _ := draftAssetRef(typ, name)
		if b.host.SystemDefinitionReadOnly(original) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System definition {name} is read-only in this tenant; edit it in Platform", original.String())
		}
	}
	return nil
}

func (b *Build) ApplicationDrafts(id string) ([]JointDraftRef, error) {
	return b.applicationDrafts(id, nil)
}

func (b *Build) applicationDrafts(id string, selectDrafts map[JointDraftRef]bool) ([]JointDraftRef, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.automation.Inventory()
	if err != nil {
		return nil, err
	}
	index := map[platform.AssetRef]JointDraftRef{}
	graph := map[platform.AssetRef][]platform.AssetRef{}
	if selectDrafts != nil {
		installed, err := b.ReleaseAssets()
		if err != nil {
			return nil, err
		}
		for _, asset := range installed {
			graph[asset.Ref] = asset.Requires
		}
	}
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
	caller := b.host.Automation(platform.Caller{}, ID)
	integrations := map[JointDraftRef]any{}
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
				if dataowner.IsIntegration(rowKind) {
					integrations[JointDraftRef{Kind: rowKind, ID: head.ID}] = row
				}
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
		}
		if selectDrafts != nil && !selectDrafts[draft] {
			queue = append(queue, graph[ref]...)
			continue
		}
		if !selected[draft] {
			if len(out) >= JointDraftLimit {
				return nil, fmt.Errorf("Application trial exceeds %d saved drafts", JointDraftLimit)
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
				assets, err = b.releaseAssets([]Object{o}, nil, nil, nil, b.Manifest().Version)
			case platform.AssetPage:
				i := slices.IndexFunc(pages, func(p Page) bool { return p.ID == draft.ID })
				p := pages[i]
				p.Published = published(p)
				assets, err = b.releaseAssets(nil, []Page{p}, nil, nil, b.Manifest().Version)
			case platform.AssetApp:
				i := slices.IndexFunc(apps, func(a Application) bool { return a.ID == draft.ID })
				a := apps[i]
				a.Published = published(a)
				assets, err = b.releaseAssets(nil, nil, []Application{a}, nil, b.Manifest().Version)
			case platform.AssetFlow:
				i := slices.IndexFunc(processes, func(p Process) bool { return p.ID == draft.ID })
				p := processes[i]
				asset, problem := b.automation.DraftAsset(p, b.Manifest().Version)
				assets, err = []platform.ReleaseAsset{asset}, problem
			case platform.AssetAgent:
				agent, found := platform.Get[Agent](caller, draft.ID)
				if !found || agent.Archived {
					return nil, fmt.Errorf("saved agent draft is unavailable")
				}
				agent.Version++
				asset, problem := agentReleaseAsset(agent, b.Manifest().Version)
				assets, err = []platform.ReleaseAsset{asset}, problem
			default:
				if record, ok := integrations[draft]; ok {
					_, version, _, _ := dataowner.IntegrationMetadata(record)
					asset, problem := b.data.Asset(draft.Kind, record, version+1)
					assets, err = []platform.ReleaseAsset{asset}, problem
					break
				}
				declarations := b.jointDeclarations
				if draft.Kind == platform.AssetEvaluation {
					plan, found := platform.Get[TestPlan](caller, draft.ID)
					if !found || plan.Archived {
						return nil, fmt.Errorf("saved evaluation plan is unavailable")
					}
					_, functions, _, functionRef, _, problem := b.functions.DraftAssets(string(plan.Function))
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
		if deps, ok := graph[ref]; ok {
			queue = append(queue, deps...)
		} else if draft.Kind == platform.AssetObject {
			queue = append(queue, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: TypeOf(objects[slices.IndexFunc(objects, func(o Object) bool { return o.ID == draft.ID })].Name)})
		}
	}
	return out, nil
}
