package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

// ReleaseAssets returns the builder's installed, unfiltered definitions. The
// member-facing Definitions projection must never be used to calculate a
// release identity: it omits rules and fields the reader cannot see.
func (b *Build) ReleaseAssets() ([]platform.ReleaseAsset, error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, err
	}
	return releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
}

// FlowReleaseAsset reads a retained native version, not the current draft or
// latest installed version. The host closes its bindings through all owners.
func (b *Build) FlowReleaseAsset(name string, version int) (platform.ReleaseAsset, error) {
	processes, err := b.processInventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range processes {
		if record.Published == "" {
			continue
		}
		image, _ := json.Marshal(record)
		if _, err := processImage(image); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range record.Versions {
			p, ok := wasPublished[Process](raw)
			if ok && p.Name == name && p.Version == version {
				return processReleaseAsset(p, b.Manifest().Version)
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("process %s version %d is not retained", name, version)
}

func (b *Build) releaseInventory() ([]Object, []Page, []Application, error) {
	if b.host == nil {
		return nil, nil, nil, fmt.Errorf("builder has no host")
	}
	c := b.host.Automation(platform.Caller{}, ID)
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder objects: %w", err)
	}
	pages, err := readDefinitionInventory[Page](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder pages: %w", err)
	}
	apps, err := readDefinitionInventory[Application](c)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read builder applications: %w", err)
	}
	return objects, pages, apps, nil
}

// The host caps one read page at 500; the existing release bound is 1000.
// Read the complete bounded inventory or fail, never silently omit assets.
func readDefinitionInventory[T any](c platform.Caller) ([]T, error) {
	var all []T
	for {
		rows, count, err := platform.Find[T](c, platform.Query{Limit: 500, Offset: len(all), Sort: []string{"id"}})
		if err != nil {
			return nil, err
		}
		if count > 1000 {
			return nil, fmt.Errorf("builder release inventory exceeds 1000 definitions of one kind")
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("builder definition inventory is incomplete")
		}
	}
}

// DraftReleaseAssets substitutes precisely one saved draft in the owner's
// inventory. It does not install or publish it. Both snapshots are returned
// so the host can compare identical code dependencies under one tenant lock.
// Only the authenticated builder-facing host path may call this method.
func (b *Build) DraftReleaseAssets(kind platform.AssetKind, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	objects, pages, apps, err := b.releaseInventory()
	if err != nil {
		return nil, nil, prior, next, false, err
	}
	processes, err := b.processInventory()
	if err != nil {
		return nil, nil, prior, next, false, err
	}
	before, err = releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
	if err != nil {
		return nil, nil, prior, next, false, err
	}
	if id == "" {
		return before, nil, prior, next, false, fmt.Errorf("draft record id is empty")
	}
	switch kind {
	case platform.AssetFlow:
		i := slices.IndexFunc(processes, func(p Process) bool { return p.ID == id })
		if i < 0 {
			return before, nil, prior, next, false, fmt.Errorf("draft process %q not found", id)
		}
		record := processes[i]
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
			err = b.host.Processes().Validate(b, flowOf(record))
			if err == nil {
				record.Published = published(record)
				record.Versions = append(slices.Clone(record.Versions), record.Published)
				record.State = "published"
				processes[i] = record
			}
		}
	case platform.AssetObject:
		i := slices.IndexFunc(objects, func(o Object) bool { return o.ID == id && !o.Archived })
		if i < 0 {
			return before, nil, prior, next, false, fmt.Errorf("draft object %q not found", id)
		}
		record := objects[i]
		if was, ok := wasPublished[Object](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(was.Name)}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: TypeOf(record.Name)}
		if err = b.check(record, record.ID); err == nil {
			err = b.validateObjectInstallation(record)
		}
		if err == nil {
			objects[i].Published = published(record)
		}
	case platform.AssetPage:
		i := slices.IndexFunc(pages, func(p Page) bool { return p.ID == id && !p.Archived })
		if i < 0 {
			return before, nil, prior, next, false, fmt.Errorf("draft page %q not found", id)
		}
		record := pages[i]
		if was, ok := wasPublished[Page](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: was.Name}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: record.Name}
		if err = b.checkPage(record); err == nil {
			err = b.host.ValidateInstallPage(descriptor(record))
		}
		if err == nil {
			pages[i].Published = published(record)
		}
	case platform.AssetApp:
		i := slices.IndexFunc(apps, func(a Application) bool { return a.ID == id && !a.Archived })
		if i < 0 {
			return before, nil, prior, next, false, fmt.Errorf("draft application %q not found", id)
		}
		record := apps[i]
		if was, ok := wasPublished[Application](record.Published); ok {
			prior = platform.AssetRef{App: ID, Kind: kind, Name: was.Name}
			hadPrior = true
		}
		next = platform.AssetRef{App: ID, Kind: kind, Name: record.Name}
		if err = b.checkApplication(record); err == nil {
			err = b.host.ValidateInstallApplication(applicationDescriptor(record))
		}
		if err == nil {
			apps[i].Published = published(record)
		}
	default:
		err = fmt.Errorf("draft kind %q cannot be previewed", kind)
	}
	if err != nil {
		return before, nil, prior, next, hadPrior, err
	}
	after, err = releaseAssets(objects, pages, apps, processes, b.Manifest().Version)
	return before, after, prior, next, hadPrior, err
}

// PublishedPage reports whether the generated page of an object was
// intentionally replaced by an explicit installed page of the same name.
func (b *Build) PublishedPage(name string) (bool, error) {
	_, pages, _, err := b.releaseInventory()
	if err != nil {
		return false, err
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		saved, ok := wasPublished[Page](record.Published)
		if !ok {
			return false, fmt.Errorf("invalid published page %s", record.ID)
		}
		if saved.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (b *Build) validateObjectInstallation(record Object) error {
	if processes := b.host.Processes(); processes != nil {
		current, exists := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
		if previous, ok := wasPublished[Object](current.Published); exists && ok {
			oldBody, err := definitionBody(previous, TypeOf(previous.Name))
			if err != nil {
				return err
			}
			newBody, err := definitionBody(record, TypeOf(record.Name))
			if err != nil {
				return err
			}
			if string(oldBody) != string(newBody) {
				running, err := processes.HasRunningDependency(TypeOf(previous.Name))
				if err != nil {
					return err
				}
				if running {
					return fmt.Errorf("finish the running processes before changing their object definition")
				}
			}
		}
	}
	entity := Entity(record)
	processes, err := b.processInventory()
	if err != nil {
		return err
	}
	current, _ := platform.Get[Object](b.host.Automation(platform.Caller{}, ID), record.ID)
	previous, _ := wasPublished[Object](current.Published)
	for _, process := range processes {
		if process.Published == "" {
			continue
		}
		raw, _ := json.Marshal(process)
		installed, err := processImage(raw)
		if err != nil {
			return err
		}
		if installed.Object != entity.Type && installed.Object != TypeOf(previous.Name) {
			continue
		}
		if problem := b.checkFlowOn(installed, entity); problem != nil {
			return fmt.Errorf("%s", problem.Message)
		}
	}
	pages, generated, err := b.objectInstallationPages(record)
	if err != nil {
		return err
	}
	return b.host.ValidateInstallDependents(entity, platform.EntityActions(entity), generated, pages...)
}

// An explicitly published page owns its name after it replaces an object's
// generated page. Updating the object must not silently overwrite that page.
func (b *Build) objectInstallationPages(record Object) ([]platform.Page, string, error) {
	explicit, err := b.PublishedPage(record.Name)
	if err != nil {
		return nil, "", err
	}
	if explicit {
		return nil, "", nil
	}
	return []platform.Page{page(record)}, record.Name, nil
}

// releaseAssets uses the saved publication, never a later mutable draft. The
// object body retains every declarative field (including access, conditions
// and approvals), excluding only record identity/stamps and editor state.
// An action depends on that complete object, so changing a rule changes its
// closed release even when the action's display metadata stays the same.
func releaseAssets(objects []Object, pages []Page, apps []Application, processes []Process, sourceVersion string) ([]platform.ReleaseAsset, error) {
	var assets []platform.ReleaseAsset
	index := map[platform.AssetRef]int{}
	add := func(asset platform.ReleaseAsset, replacesGeneratedPage bool) error {
		if i, exists := index[asset.Ref]; exists {
			if replacesGeneratedPage && asset.Ref.Kind == platform.AssetPage {
				assets[i] = asset
				return nil
			}
			return fmt.Errorf("builder release asset %s is declared twice", asset.Ref)
		}
		index[asset.Ref] = len(assets)
		assets = append(assets, asset)
		return nil
	}
	for _, record := range objects {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Object
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published object %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published object %s belongs to another record", record.ID)
		}
		ref := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: TypeOf(saved.Name)}
		body, err := definitionBody(saved, ref.Name)
		if err != nil {
			return nil, err
		}
		var requires []platform.AssetRef
		for _, field := range saved.Fields {
			if field.Ref != "" && field.Ref != ref.Name {
				app, _, ok := strings.Cut(field.Ref, ".")
				if !ok {
					return nil, fmt.Errorf("object %s field %s has invalid reference %s", ref, field.Name, field.Ref)
				}
				requires = append(requires, platform.AssetRef{App: app, Kind: platform.AssetObject, Name: field.Ref})
			}
		}
		for _, action := range saved.Actions {
			for _, create := range action.Creates {
				requires = append(requires, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: create.Object})
			}
		}
		slices.SortFunc(requires, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
		requires = slices.Compact(requires)
		if err := add(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion,
			Requires: requires, Body: body}, false); err != nil {
			return nil, err
		}
		for _, action := range platform.EntityActions(Entity(saved)) {
			actionBody, err := json.Marshal(struct {
				Schema string          `json:"schema"`
				Target string          `json:"target"`
				Roles  []string        `json:"roles"`
				Action platform.Action `json:"action"`
			}{Schema: action.Schema, Target: action.Target, Roles: action.Roles, Action: action})
			if err != nil {
				return nil, fmt.Errorf("encode action %s: %w", action.Schema, err)
			}
			if err := add(platform.ReleaseAsset{
				Ref:             platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: action.Schema},
				ContractVersion: 1, SourceVersion: sourceVersion, Requires: []platform.AssetRef{ref}, Body: actionBody,
			}, false); err != nil {
				return nil, err
			}
		}
		pageAsset, err := platform.PageReleaseAsset(ID, sourceVersion, page(saved))
		if err != nil {
			return nil, err
		}
		if err := add(pageAsset, false); err != nil {
			return nil, err
		}
	}
	for _, record := range pages {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Page
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published page %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published page %s belongs to another record", record.ID)
		}
		asset, err := platform.PageReleaseAsset(ID, sourceVersion, descriptor(saved))
		if err != nil {
			return nil, err
		}
		if err := add(asset, true); err != nil {
			return nil, err
		}
	}
	for _, record := range apps {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Application
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return nil, fmt.Errorf("invalid published application %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return nil, fmt.Errorf("published application %s belongs to another record", record.ID)
		}
		asset, err := platform.ApplicationReleaseAsset(ID, sourceVersion, applicationDescriptor(saved))
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	for _, record := range processes {
		if record.Published == "" {
			continue
		}
		image, _ := json.Marshal(record)
		saved, err := processImage(image)
		if err != nil {
			return nil, err
		}
		asset, err := processReleaseAsset(saved, sourceVersion)
		if err != nil {
			return nil, err
		}
		if err := add(asset, false); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(assets, func(a, b platform.ReleaseAsset) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return assets, nil
}

func processReleaseAsset(saved Process, sourceVersion string) (platform.ReleaseAsset, error) {
	fl := flowOf(saved)
	ref := platform.AssetRef{App: ID, Kind: platform.AssetFlow, Name: TypeOf(fl.Name)}
	subject := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: fl.Subject}
	var actions []platform.AssetRef
	for _, step := range fl.Steps {
		if step.Act != nil {
			actions = append(actions, platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: step.Act.Action})
		}
	}
	slices.SortFunc(actions, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	actions = slices.Compact(actions)
	definition := json.RawMessage(published(saved))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(definition, &fields); err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "version"} {
		delete(fields, key)
	}
	definition, err := json.Marshal(fields)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	body, err := json.Marshal(platform.FlowReleaseDescriptor{Name: ref.Name, Subject: subject, Actions: actions, Definition: definition})
	return platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion,
		Requires: append([]platform.AssetRef{subject}, actions...), Body: body}, err
}

// ProcessFromReleaseAsset reconstructs the bounded owner definition, checking
// that its compiled bindings are exactly the ones in the release envelope.
// A sandbox assigns its own native version ordinal; that counter is not the
// semantic content identity or a dependency version.
func ProcessFromReleaseAsset(asset platform.ReleaseAsset) (Process, error) {
	var envelope platform.FlowReleaseDescriptor
	var p Process
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetFlow || asset.ContractVersion != 1 {
		return p, fmt.Errorf("unsupported process release asset %s", asset.Ref)
	}
	if err := json.Unmarshal(asset.Body, &envelope); err != nil {
		return p, err
	}
	if err := json.Unmarshal(envelope.Definition, &p); err != nil {
		return p, err
	}
	expected, err := processReleaseAsset(p, asset.SourceVersion)
	if err != nil {
		return p, err
	}
	var got, want any
	if json.Unmarshal(asset.Body, &got) != nil || json.Unmarshal(expected.Body, &want) != nil || expected.Ref != asset.Ref || !reflect.DeepEqual(got, want) {
		return p, fmt.Errorf("process %s definition and compiled bindings differ", asset.Ref)
	}
	p.ID, p.Version, p.State = asset.Ref.Name, 1, "published"
	p.Published = published(p)
	p.Versions = []string{p.Published}
	return p, nil
}

func definitionBody(value Object, typ string) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "installed", "published"} {
		delete(body, key)
	}
	body["type"], err = json.Marshal(typ)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}
