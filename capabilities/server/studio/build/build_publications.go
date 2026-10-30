package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
)

func (b *Build) SavedDraftChanges(c platform.Caller, application, typ, id string) ([]DraftChange, error) {
	out := []DraftChange{}
	held := map[platform.AssetRef]bool{}
	if application != "" {
		app, ok := platform.Get[Application](c, application)
		if !ok || app.Archived {
			return nil, fmt.Errorf("application draft is unavailable")
		}
		for _, ref := range app.Resources {
			held[ref] = true
		}
		for _, name := range app.Pages {
			held[platform.AssetRef{App: ID, Kind: platform.AssetPage, Name: name}] = true
		}
	}
	for _, entity := range b.Manifest().Entities {
		if typ != "" && entity.Type != typ {
			continue
		}
		if _, err := draftAssetRef(entity.Type, ""); err != nil {
			continue
		}
		info, ok := b.host.Entity(entity.Type)
		if !ok {
			return nil, fmt.Errorf("draft declaration %s is unavailable", entity.Type)
		}
		rows, err := readDefinitionInventoryOf(c, reflect.TypeOf(entity.Model))
		if err != nil {
			return nil, fmt.Errorf("read %s drafts: %w", entity.Type, err)
		}
		for _, record := range rows {
			var meta struct {
				platform.Record
				Name, Title, Kind, Published string
				Evaluation                   []json.RawMessage
			}
			if err := json.Unmarshal(platform.Raw(record), &meta); err != nil {
				return nil, err
			}
			if meta.Archived || id != "" && meta.ID != id {
				continue
			}
			ref, _ := draftAssetRef(entity.Type, meta.Name)
			if entity.Type == TestPlanType && len(meta.Evaluation) > 0 {
				ref.Kind = platform.AssetEvaluation
			}
			if application != "" && !(entity.Type == AppType && meta.ID == application) && !held[ref] {
				var original struct {
					Name string
					Ref  platform.AssetRef
				}
				_ = json.Unmarshal([]byte(meta.Published), &original)
				prior := original.Ref
				if prior.Kind == "" {
					if original.Name == "" {
						continue
					}
					prior, _ = draftAssetRef(entity.Type, original.Name)
				}
				if !held[prior] {
					continue
				}
			}
			change := DraftChange{Ref: ref, Type: entity.Type, ID: meta.ID, Title: meta.Title, Variant: meta.Kind, Published: meta.Published != "", Changed: meta.Changed, Fields: []DraftChangeField{}}
			before, after, portable, err := b.draftComparison(record, meta.Published, info)
			if err != nil {
				change.Diagnostic = err.Error()
			} else {
				var names []string
				if portable {
					for name := range before {
						names = append(names, name)
					}
					for name := range after {
						if !slices.Contains(names, name) {
							names = append(names, name)
						}
					}
					slices.Sort(names)
				} else {
					for _, field := range info.Fields {
						if !field.ReadOnly {
							names = append(names, field.Name)
						}
					}
				}
				for _, name := range names {
					if bytes.Equal(before[name], after[name]) {
						continue
					}
					field := DraftChangeField{Name: name}
					if typ != "" && id != "" {
						field.Before, field.After = before[name], after[name]
					}
					change.Fields = append(change.Fields, field)
				}
			}
			if !change.Published || len(change.Fields) > 0 || change.Diagnostic != "" {
				out = append(out, change)
			}
		}
	}
	slices.SortFunc(out, func(a, b DraftChange) int { return strings.Compare(a.Type+"/"+a.ID, b.Type+"/"+b.ID) })
	return out, nil
}

func (b *Build) draftComparison(record any, published string, info platform.EntityInfo) (before, after map[string]json.RawMessage, portable bool, err error) {
	current := platform.Raw(record)
	previous := json.RawMessage(`{}`)
	if published != "" {
		var frozen platform.ReleaseAsset
		if json.Unmarshal([]byte(published), &frozen) == nil && frozen.Ref.Kind != "" && len(frozen.Body) > 0 {
			portable = true
			previous = frozen.Body
			var asset platform.ReleaseAsset
			switch r := record.(type) {
			case Alert:
				asset, err = alertReleaseAsset(r, max(1, r.DefinitionVersion))
			case SampleSetRecord:
				asset, err = sampleSetAsset(r, max(1, r.DefinitionVersion))
			case TestPlan:
				asset, err = b.testPlanAsset(r, max(1, r.DefinitionVersion))
			default:
				_, ordinal, _, _ := integrationMetadata(record)
				asset, err = b.integrationAsset(frozen.Ref.Kind, record, max(1, ordinal))
			}
			current = asset.Body
		} else {
			original := reflect.New(info.Go).Interface()
			err = json.Unmarshal([]byte(published), original)
			previous = platform.Raw(original)
		}
	}
	if err != nil {
		return
	}
	canonicalFields := func(raw json.RawMessage) (map[string]json.RawMessage, error) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		for name, value := range fields {
			var parsed any
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.UseNumber()
			if err := decoder.Decode(&parsed); err != nil {
				return nil, err
			}
			fields[name] = platform.Raw(parsed)
		}
		return fields, nil
	}
	before, err = canonicalFields(previous)
	if err == nil {
		after, err = canonicalFields(current)
	}
	return
}

func (*Build) AcceptedPublicationSchemas() []string {
	return []string{SchemaPublish, SchemaRelease, SchemaHandOver, SchemaProcess, SchemaPropertyType, SchemaLinkType, SchemaQuery, SchemaFunction, SchemaCodePublish, SchemaAgentPublish, SchemaTablePublish, SchemaAlertPublish, SchemaEvaluationSetPublish}
}

func (b *Build) publicationImage(schema string, image []byte) (platform.Entity, []platform.Action, []platform.Page, *platform.Application, error) {
	switch schema {
	case SchemaEvaluationSetPublish:
		_, _, err := b.testPlanImage(image)
		return b.testPlanEntity(), nil, nil, nil, err
	case SchemaAgentPublish:
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		if _, ok := ReadPublication[Agent](record.Published); !ok {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted agent has no published definition")
		}
		return b.agentEntity(), nil, nil, nil, nil
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := ReadPublication[Object](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name || record.Installed != TypeOf(published.Name) {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted object has no matching published definition")
		}
		if published.ExtensionSource != nil {
			if err := published.ExtensionSource.Check(published.Extends); err != nil {
				return platform.Entity{}, nil, nil, nil, err
			}
		}
		entity := b.entity(published)
		portable, err := b.dispatchSchemaObject(published)
		if err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		if err := checkObjectSchemas(portable); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		actions := objectActions(published, entity)
		return entity, actions, []platform.Page{CompileObjectPage(published)}, nil, nil
	case SchemaRelease:
		var record Page
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := ReadPublication[Page](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted page has no published definition")
		}
		return platform.Entity{}, nil, []platform.Page{descriptor(published)}, nil, nil
	case SchemaHandOver:
		var record Application
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := ReadPublication[Application](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted application has no published definition")
		}
		app := applicationDescriptor(published)
		return platform.Entity{}, nil, nil, &app, nil
	}
	return platform.Entity{}, nil, nil, nil, fmt.Errorf("not an accepted builder publication: %s", schema)
}

func (b *Build) ValidateAcceptedPublication(schema string, image []byte) error {
	if schema == SchemaAlertPublish {
		_, asset, err := alertImage(image)
		if err != nil {
			return err
		}
		alert, _, err := alertFromAsset(asset)
		if err != nil {
			return err
		}
		return b.checkAlert(alert)
	}
	if schema == SchemaEvaluationSetPublish {
		_, _, err := b.testPlanImage(image)
		return err
	}
	if schema == SchemaTablePublish {
		tb, err := tableImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallOperation(platform.Caller{Replaying: true}, tb.Definition())
	}
	if schema == SchemaAgentPublish {
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		agent, ok := ReadPublication[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		return b.checkAgent(agent)
	}

	if k := definitionKindOfSchema(schema); k != nil {
		return k.validateImage(b, image)
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallOperation(platform.Caller{Replaying: true}, code.Definition())
	}
	if schema == SchemaFunction {
		f, err := functionImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallFunction(f.Definition())
	}
	if schema == SchemaProcess {
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if b.host.BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return b.host.BuildProcesses().Validate(b, b.flowOf(p))
	}
	_, _, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := ReadPublication[Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		return b.validateObjectInstallation(platform.Caller{Replaying: true}, published)
	case SchemaRelease:
		return b.host.ValidateInstallPage(pages[0])
	case SchemaHandOver:
		return b.host.ValidateInstallApplication(*app)
	}
	return fmt.Errorf("unknown builder publication: %s", schema)
}

func (b *Build) ApplyAcceptedPublication(schema string, image []byte) error {
	if schema == SchemaAlertPublish {
		_, _, err := alertImage(image)
		return err
	}
	if schema == SchemaEvaluationSetPublish {
		_, _, err := b.testPlanImage(image)
		return err
	}
	if schema == SchemaTablePublish {
		tb, err := tableImage(image)
		if err != nil {
			return err
		}
		var record Table
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		tb.Versions = record.Versions
		return b.installTable(platform.Caller{Replaying: true}, tb)
	}
	if schema == SchemaAgentPublish {
		var record Agent
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		agent, ok := ReadPublication[Agent](record.Published)
		if !ok {
			return fmt.Errorf("accepted agent has no published definition")
		}
		b.agents[agent.Name] = agent
		return nil
	}

	if k := definitionKindOfSchema(schema); k != nil {
		return k.applyImage(b, image)
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.installCode(platform.Caller{Replaying: true}, code)
	}
	if schema == SchemaFunction {
		f, err := functionImage(image)
		if err != nil {
			return err
		}
		return b.installFunction(platform.Caller{Replaying: true}, f)
	}
	if schema == SchemaProcess {
		p, err := ReadProcessImage(image)
		if err != nil {
			return err
		}
		if b.host.BuildProcesses() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return b.host.BuildProcesses().Install(b, b.flowOf(p))
	}
	entity, actions, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := ReadPublication[Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		pages, _, err = b.objectInstallationPages(published)
		if err != nil {
			return err
		}
		if err := b.ledger.Extend([]string{entity.Type}, actions); err != nil {
			return fmt.Errorf("extend accepted object: %w", err)
		}
		b.ledger.Catalog.ReplaceTarget(entity.Type, actions)
		if err := b.host.Install(platform.Caller{Replaying: true}, entity, actions, pages...); err != nil {
			return fmt.Errorf("install accepted object: %w", err)
		}
		b.installed[entity.Type] = entity
	case SchemaRelease:
		if err := b.host.InstallPage(platform.Caller{Replaying: true}, pages[0]); err != nil {
			return fmt.Errorf("install accepted page: %w", err)
		}
	case SchemaHandOver:
		if err := b.host.InstallApplication(platform.Caller{Replaying: true}, *app); err != nil {
			return fmt.Errorf("install accepted application: %w", err)
		}
	}
	return nil
}

func published[T any](definition T) string {
	switch value := any(definition).(type) {
	case Object:
		value.Published = ""
		definition = any(value).(T)
	case Page:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Application:
		value.Published = ""
		definition = any(value).(T)
	case PropertyType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case LinkType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Query:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Function:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Code:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Process:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	}
	raw, _ := json.Marshal(definition)
	return string(raw)
}

func (b *Build) Reinstall() error {
	if err := propertyKind.restore(b); err != nil {
		return err
	}

	objects, pages, applications, err := b.releaseInventory()
	if err != nil {
		return err
	}
	ordered, err := businessVersionInstallOrder(objects)
	if err != nil {
		return err
	}
	beforeMode := b.jointMode
	b.jointMode = true
	defer func() { b.jointMode = beforeMode }()
	for _, was := range ordered {
		if err := b.install(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("object %s: %v: %s", was.Name, err, err.Message)
		}
	}
	b.jointMode = beforeMode
	if err := linkKind.restore(b); err != nil {
		return err
	}
	if err := queryKind.restore(b); err != nil {
		return err
	}
	if err := restoreDefinitionPublications(b, b.functionInventory, SchemaFunction); err != nil {
		return err
	}

	agents, err := b.agentInventory()
	if err != nil {
		return err
	}
	for _, record := range agents {
		if a, ok := ReadPublication[Agent](record.Published); ok && !record.Archived {
			b.agents[a.Name] = a
		}
	}
	codes, err := b.codeInventory()
	if err != nil {
		return err
	}
	for _, record := range codes {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		code, err := codeImage(raw)
		if err != nil {
			return err
		}
		if err := b.installCode(platform.Caller{Replaying: true}, code); err != nil {
			return err
		}
	}
	for _, p := range pages {
		was, ok := ReadPublication[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.release(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("page %s: %v", p.Name, err)
		}
	}
	for _, p := range pages {
		was, ok := ReadPublication[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.host.ValidateInstallPage(descriptor(was)); err != nil {
			return fmt.Errorf("page %s navigation: %w", p.Name, err)
		}
	}
	if err := b.installProcesses(); err != nil {
		return err
	}
	if err := b.installTables(); err != nil {
		return err
	}
	for _, a := range applications {
		was, ok := ReadPublication[Application](a.Published)
		if !ok || a.Archived {
			continue
		}
		if err := b.hand(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("application %s: %v", a.Name, err)
		}
	}
	return nil
}

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
			if err := mapDispatchIntegrations(&object, b.integrationID); err != nil {
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
	processes, err := b.processInventory()
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
				if ValueIntegrationKind(rowKind) {
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
					_, version, _, _ := integrationMetadata(record)
					asset, problem := b.integrationAsset(draft.Kind, record, version+1)
					assets, err = []platform.ReleaseAsset{asset}, problem
					break
				}
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
		if deps, ok := graph[ref]; ok {
			queue = append(queue, deps...)
		} else if draft.Kind == platform.AssetObject {
			queue = append(queue, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: TypeOf(objects[slices.IndexFunc(objects, func(o Object) bool { return o.ID == draft.ID })].Name)})
		}
	}
	return out, nil
}
