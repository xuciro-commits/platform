package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	pageowner "platformserver/studio/pages"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"platformserver/kernel/records"
	"platformserver/platform"
)

// Objects a tenant defines are installed into the running host:
// the record store keeps their records, the host routes their generated
// actions to the app that owns them, and the asset registry offers the object,
// its actions and its page. From then on they are ordinary entity types — the
// same reads, forms, search, aggregates, journal and replay as an app's own.

// Install declares an entity an app composed at runtime, with the actions
// generated for it and the page people open it through. Installed again, the
// type takes its new fields and its records come with them (D3). The app has
// already taught its own ledger the classes and schemas.
func (t *Tenant) Install(app platform.App, e platform.Entity, actions []platform.Action, pages ...platform.Page) error {
	id := app.Manifest().ID
	if other := t.owner["action:"+actions[0].Schema]; other != nil && (other.Manifest().ID != id || other.AcceptedLedger() != app.AcceptedLedger()) {
		return fmt.Errorf("%s: another app owns %s", e.Type, actions[0].Schema)
	}
	info, err := platform.Describe(id, e, func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	if !t.installingSet {
		if err := t.checkObjectProperties(info); err != nil {
			return err
		}
		if err := t.checkRecordSetDeclarations(info); err != nil {
			return err
		}
		if err := platform.CheckConstraints(info, t.entity, true); err != nil {
			return err
		}
	}
	if err := t.records.install(info); err != nil {
		return err
	}
	for _, action := range actions {
		if action.Target != e.Type {
			return fmt.Errorf("%s: action %s targets %s", e.Type, action.Schema, action.Target)
		}
		owner := t.app(id)
		if owner == nil {
			owner = app
		}
		t.owner["action:"+action.Schema] = owner
	}
	for name, owner := range t.owner {
		if owner.Manifest().ID == id && strings.HasPrefix(name, "read:protocol:"+e.Type+":") {
			delete(t.owner, name)
		}
	}
	for _, protocol := range e.Protocols {
		for _, name := range protocol.Reads {
			t.owner["read:"+name] = t.app(id)
		}
	}
	kept := slices.DeleteFunc(t.definitions, func(def platform.Definition) bool {
		return def.Ref.App == id && def.Source == "tenant" && def.Action != nil && def.Action.Target == info.Type && !slices.ContainsFunc(actions, func(action platform.Action) bool { return action.Schema == def.Ref.Name })
	})
	kept = slices.DeleteFunc(kept, func(def platform.Definition) bool {
		return def.Ref.App == id && def.Protocol != nil && slices.ContainsFunc(def.Requires, func(ref platform.AssetRef) bool {
			return ref.Kind == platform.AssetAction && strings.HasPrefix(ref.Name, e.Type+".")
		})
	})
	if len(kept) != len(t.definitions) {
		t.setDefinitions(kept)
	}
	return t.installDefinitions(id, info, actions, pages, e.Protocols...)
}

// decodeRowImage reads one saved row into the current declaration. A type
// declared again may name fewer fields than the row carries: the ones it
// dropped are left out and the rest keep their value, so a re-declared type
// never refuses the rows it already holds.
func decodeRowImage(info platform.EntityInfo, raw json.RawMessage, projection *records.Projection, release, format string) (json.RawMessage, error) {
	decode, err := records.DecodeFormattedImage(info, raw, projection, release, format)
	if err == nil {
		return decode, nil
	}
	pruned, pruneErr := records.PruneImage(info, raw)
	if pruneErr != nil {
		return nil, err
	}
	return records.DecodeFormattedImage(info, pruned, projection, release, format)
}

// install declares a type, or declares it again: the rows are carried into the
// new Go type through their JSON, so a record keeps the value of every field
// that is still there.
func (s *recordStore) install(info platform.EntityInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if other := s.byGo[info.Go]; other != nil && other.info.Type != info.Type {
		return fmt.Errorf("%s: another type is already this Go type", info.Type)
	}
	et := &entityType{store: s, info: info, rows: records.Rows{}, knowledge: records.KnowledgeBearing(info)}
	if before := s.types[info.Type]; before != nil {
		if before.info.App != info.App {
			return fmt.Errorf("%s is declared by %s", info.Type, before.info.App)
		}
		if before.info.Versioning != nil && !reflect.DeepEqual(before.info.Versioning, info.Versioning) || len(before.info.VersionPins) > 0 && !reflect.DeepEqual(before.info.VersionPins, info.VersionPins) {
			return fmt.Errorf("published business versioning and pin bindings are retained")
		}
		rows := before.rows
		if rows.Sourced() && s.current.Backend() != nil && !before.retainedProjection {
			// Migrated SQL bytes may already have the target shape while this
			// private installation still catalogs the source declaration. Read
			// them using the target, then perform the same strict conversion.
			rows = rows.Rebind(s.current.Source(info.Type, &info))
		}
		entries := make([]records.Entry[*records.Row], 0, rows.Len())
		for id, r := range rows.All() {
			raw, err := r.Image()
			if err != nil {
				return err
			}
			v := reflect.New(info.Go).Elem()
			decode, err := decodeRowImage(info, raw, r.Projection, s.current.Release(), r.Format)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(decode, v.Addr().Interface()); err != nil {
				return err
			}
			platform.ApplyFieldAliases(info, v)
			records.EmptyLists(v)
			installed := &records.Row{Value: v, Format: r.Format, HistoryParent: r.HistoryParent, HistoryHead: r.HistoryHead, History: r.History, LoadHistory: r.LoadHistory, HistoryCount: r.HistoryCount, Projection: r.Projection, Extraction: r.Extraction}
			installed.RetainOriginal(raw)
			entries = append(entries, records.Entry[*records.Row]{Key: id, Value: installed})
		}
		index, err := records.From(entries)
		if err != nil {
			return err
		}
		et.setRows(index)
		delete(s.byGo, before.info.Go)
	} else if s.current.Backend() != nil && s.current.View() != nil {
		// An owner-only SQL checkpoint restores dynamic declarations after native
		// types. Their accepted rows already belong to the same fixed read view.
		et.rows = records.SourceRows(s.current.Source(info.Type, nil))
	}
	s.types[info.Type], s.byGo[info.Go] = et, et
	s.generation++
	return nil
}

// retainedDefinitionVersions leaves the original history untouched; callers
// publish the current version only after their installation checks succeed.
func retainedDefinitionVersions[T any](history map[string]T, previousVersion string, previous *T, version string, current T) (map[string]T, bool) {
	versions := maps.Clone(history)
	if versions == nil {
		versions = map[string]T{}
	}
	if previous != nil {
		versions[previousVersion] = *previous
	}
	if prior, ok := versions[version]; ok && string(platform.Raw(prior)) != string(platform.Raw(current)) {
		return nil, false
	}
	return versions, true
}

// storeDefinition updates the existing discovery registry; callers own
// declaration validation, version retention and installation side effects.
// Every mutation — replace, insert, or reorder — moves the definitions
// generation so member-scoped definition caches never serve stale versions.
func (t *Tenant) storeDefinition(index int, def platform.Definition) {
	if index >= 0 {
		t.definitions[index] = def
	} else {
		t.definitions = append(t.definitions, def)
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	t.setDefinitions(t.definitions)
}

// installDefinitions puts the installed object, its actions and its pages in
// the asset registry, so code and builder discover one set of assets.
func (t *Tenant) installDefinitions(app string, info platform.EntityInfo, actions []platform.Action, pages []platform.Page, protocols ...platform.Provision) error {
	var describeErr error
	put := func(def platform.Definition) {
		if describeErr != nil {
			return
		}
		def, describeErr = t.describeDefinition(def)
		if describeErr != nil {
			return
		}
		// A definition read again replaces the one installed before it: a
		// published object takes its new fields. The generation moves with it,
		// so every cached catalog, entity view and encoded answer is rebuilt
		// rather than serving the declaration this one replaced.
		if i := slices.IndexFunc(t.definitions, func(x platform.Definition) bool { return x.Ref == def.Ref }); i >= 0 {
			next := slices.Clone(t.definitions)
			next[i] = def
			t.setDefinitions(next)
			return
		}
		t.setDefinitions(append(t.definitions, def))
	}
	object := platform.AssetRef{App: app, Kind: platform.AssetObject, Name: info.Type}
	entity := info
	put(platform.Definition{Ref: object, Source: "tenant", Version: "1", ContractVersion: 1, Entity: &entity})
	for _, action := range actions {
		a := action
		put(platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetAction, Name: a.Schema}, Source: "tenant", Version: "1",
			ContractVersion: 1, Action: &a})
	}
	for _, page := range pages {
		p := page
		put(platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetPage, Name: p.Name}, Source: "tenant", Version: "1",
			ContractVersion: 1, Page: &p})
	}
	for _, provision := range protocols {
		protocol := platform.ProtocolReleaseDescriptor{Name: provision.Protocol.Name, Version: provision.Protocol.Version, Actions: slices.Clone(provision.Protocol.Actions), Reads: slices.Clone(provision.Protocol.Reads), ActionBindings: map[string]platform.AssetRef{}, ActionRoles: map[string][]string{}, ReadBindings: map[string]platform.AssetRef{}}
		for _, action := range protocol.Actions {
			mapped := provision.Actions[action.Schema]
			protocol.ActionBindings[action.Schema] = platform.AssetRef{App: app, Kind: platform.AssetAction, Name: mapped}
			for _, actual := range actions {
				if actual.Schema == mapped {
					protocol.ActionRoles[action.Schema] = slices.Clone(actual.Roles)
				}
			}
		}
		for _, read := range protocol.Reads {
			protocol.ReadBindings[read] = object
		}
		put(platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetProtocol, Name: protocol.Name}, Source: "tenant", Version: "1", ContractVersion: protocol.Version, Protocol: &protocol})
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return describeErr
}

// reinstall asks every app that installs its own definitions to install them
// again: after a restore, before the records of the types they define (D4).
func (t *Tenant) reinstall() error {
	// Restore the complete saved model before checking reciprocal references,
	// just as an accepted release installs one complete definition set.
	before := t.installingSet
	t.installingSet = true
	defer func() { t.installingSet = before }()
	if err := t.registerOwnedAssetMetadata(); err != nil {
		return err
	}
	for _, a := range t.apps {
		x, ok := a.(interface{ Reinstall() error })
		if !ok {
			continue
		}
		if err := x.Reinstall(); err != nil {
			return fmt.Errorf("tenant %s: %s: %v", t.ID, a.Manifest().ID, err)
		}
	}
	if err := t.registerOwnedAssetMetadata(); err != nil {
		return err
	}
	t.installingSet = before
	if t.systemDefinitionState.source.Load() != nil {
		t.systemDefinitionState.applied = ""
		if err := t.applySystemDefinitionProjection(); err != nil {
			return err
		}
	}
	return t.validateAssetSet()
}

// InstallPage offers a page an app composed at runtime: it must name an object
// this tenant has, fields that object declares and actions about it, so the
// workspace can always open what the registry offers (#132). The
// member's own reads and catalog still decide what they see on it.
func (t *Tenant) InstallPage(app platform.App, p platform.Page) error {
	return t.installPage(app, p, true)
}

func (t *Tenant) installPage(app platform.App, p platform.Page, targets bool, engines ...platform.RuleEngine) error {
	canonical := p
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	p = safe
	if err := p.CheckSafety(); err != nil {
		return err
	}
	for _, trigger := range p.ActionTriggers {
		refs := slices.Clone(trigger.Actions)
		if trigger.InlineEdit != nil {
			refs = append(refs, trigger.InlineEdit.Action)
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					refs = append(refs, effect.Action.Ref)
				}
			}
		}
		for _, ref := range refs {
			if owner := t.owner["action:"+ref.Name]; owner != nil {
				if action, ok := owner.Manifest().Actions.Action(ref.Name); ok && platform.StaticControlAction(action) {
					return fmt.Errorf("page %s cannot bind static control action %s", p.Name, ref.Name)
				}
			}
		}
	}
	if err := p.CheckModelBindings(t.entity, t.modelCriteria, engines...); err != nil {
		return err
	}
	if err := pageowner.CheckModelQueries(p, t.definitions, t.entity); err != nil {
		return err
	}
	if err := t.checkPageQueries(p); err != nil {
		return err
	}
	if iface := p.InputSchema.Interface; iface != nil {
		for _, ports := range []map[string]platform.PagePort{iface.Inputs, iface.Outputs} {
			for id, port := range ports {
				if port.Object != nil {
					info, ok := t.entity(port.Object.Name)
					if !ok || info.App != port.Object.App {
						return fmt.Errorf("page port %s has an unknown object", id)
					}
				}
			}
		}
	}
	info, known := t.entity(p.Object.Name)
	if p.Name == "" || !known {
		return fmt.Errorf("page %s object %s is unavailable", p.Name, p.Object.Name)
	}
	if err := t.checkSections(p, info); err != nil {
		return err
	}
	id := app.Manifest().ID
	if targets {
		for _, d := range t.definitions {
			if d.Application != nil && d.Ref.App == id && slices.Contains(d.Application.Pages, p.Name) {
				if err := d.Application.CheckPageVariables(p); err != nil {
					return err
				}
			}
		}
		if err := pageowner.CheckNavigation(p, id, t.definitions); err != nil {
			return err
		}
	}
	return t.installDefinitions(id, info, nil, []platform.Page{canonical})
}

var selectionName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// checkSections holds a composed page to what this tenant has: a widget it
// knows, an object, the fields, actions and measures that object declares.
// What the registry offers must open.
func (t *Tenant) checkSections(p platform.Page, page platform.EntityInfo) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	if err := pageowner.CheckSelections(safe, t.entity, func(name string) (platform.Action, bool) {
		if owner := t.owner["action:"+name]; owner != nil {
			return owner.Manifest().Actions.Action(name)
		}
		return platform.Action{}, false
	}); err != nil {
		return err
	}
	if err := safe.CheckSafety(); err != nil {
		return err
	}
	if err := safe.CheckRecordPorts(); err != nil {
		return err
	}
	for _, binding := range safe.DataBindings {
		if err := safe.CheckDataBinding(binding, t.entity, t.modelCriteria, t.services.Rules); err != nil {
			return fmt.Errorf("page %s binding %s: %w", p.Name, binding.ID, err)
		}
		if binding.Query != nil {
			query := pageowner.QuerySourceDefinition(t.definitions, binding.Query)
			if query == nil || query.Query == nil || query.Query.Object != binding.Object.Name {
				return fmt.Errorf("page binding query owner or version is unavailable")
			}
		}
		for _, resource := range binding.Resources {
			if resource.Asset == nil {
				continue
			}
			ref := resource.Asset.Ref
			if resource.Kind == "service" {
				switch ref.Kind {
				case platform.AssetObject:
					info, ok := t.entity(ref.Name)
					if !ok || info.App != ref.App {
						return fmt.Errorf("page service owner is unavailable")
					}
				case platform.AssetAction:
					owner := t.owner["action:"+ref.Name]
					if owner == nil || owner.Manifest().ID != ref.App {
						return fmt.Errorf("page service action owner is unavailable")
					}
				}
			} else if !pageowner.ExplorationAssetAvailable(t.definitions, *resource.Asset) {
				return fmt.Errorf("page resource version is unavailable")
			}
		}
	}
	for _, trigger := range safe.ActionTriggers {
		object := p.Object.Name
		for _, binding := range safe.DataBindings {
			if binding.ID == trigger.Binding {
				if binding.Object.Name != "" {
					object = binding.Object.Name
				}
				if binding.Model != nil {
					object = binding.Model.Type
				}
			}
		}
		for _, action := range trigger.Actions {
			if err := t.checkAction(p.Name, action.Name, object); err != nil {
				return err
			}
		}
		if trigger.InlineEdit != nil {
			if err := t.checkAction(p.Name, trigger.InlineEdit.Action.Name, object); err != nil {
				return err
			}
			owner := t.owner["action:"+trigger.InlineEdit.Action.Name]
			action, _ := owner.Manifest().Actions.Action(trigger.InlineEdit.Action.Name)
			info, _ := t.entity(object)
			section := platform.Section{InlineEdit: trigger.InlineEdit}
			if err := section.CheckTableEdit(info, action); err != nil {
				return err
			}
		}
		if len(trigger.Defaults) > 0 {
			if len(trigger.Actions) != 1 {
				return fmt.Errorf("action parameters require one exact action")
			}
			owner := t.owner["action:"+trigger.Actions[0].Name]
			if owner == nil {
				return fmt.Errorf("action parameter owner is unavailable")
			}
			action, _ := owner.Manifest().Actions.Action(trigger.Actions[0].Name)
			info, _ := t.entity(object)
			for _, mapping := range trigger.Defaults {
				field, found := info.Field(mapping.Field)
				parameter := slices.IndexFunc(action.Payload, func(value platform.Field) bool { return value.Name == mapping.Parameter })
				if !found || parameter < 0 || !platform.ActionParameterCompatible(field, action.Payload[parameter]) {
					return fmt.Errorf("action parameter field type does not match its original signature")
				}
			}
		}
		if trigger.Operation != nil {
			info, ok := t.entity(object)
			if !ok {
				return fmt.Errorf("page operation input object is unavailable")
			}
			if err := (pageowner.Bindings{Operation: t.pageOperation}).CheckOperation(trigger, info); err != nil {
				return err
			}
		}
		if trigger.Function != nil {
			owner, ok := t.app(trigger.Function.Ref.App).(platform.RetainedFunctionAssets)
			if !ok {
				return fmt.Errorf("page function owner is unavailable")
			}
			asset, err := owner.FunctionReleaseAsset(trigger.Function.Ref.Name, trigger.Function.SourceVersion)
			if err != nil {
				return err
			}
			var function platform.AIFunction
			if json.Unmarshal(asset.Body, &function) != nil || function.Object != object {
				return fmt.Errorf("page function input type is unavailable")
			}
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					if err := t.checkAction(p.Name, effect.Action.Ref.Name, object); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (t *Tenant) checkAction(page, schema, object string) error {
	owner := t.owner["action:"+schema]
	action, ok := platform.Action{}, false
	if owner != nil {
		action, ok = owner.Manifest().Actions.Action(schema)
	}
	if !ok || action.Target != object {
		return fmt.Errorf("page %s: no action %s about %s", page, schema, object)
	}
	return nil
}

// entity is an entity type's declaration, as the app that composes over it sees it.
func (t *Tenant) entity(typ string) (platform.EntityInfo, bool) {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	et := t.records.types[typ]
	if et == nil {
		return platform.EntityInfo{}, false
	}
	return et.info, true
}

// InstallApplication offers an application a tenant handed to its people: a
// name over pages that are already installed. It grants nothing —
// the registry offers each page to whoever may read what it shows, and an
// application to whoever may open one of its pages.
func (t *Tenant) InstallApplication(app platform.App, a platform.Application) error {
	id := app.Manifest().ID
	if err := t.checkApplicationExtension(platform.AssetRef{App: id, Kind: platform.AssetApp, Name: a.Name}, a); err != nil {
		return err
	}
	if a.Name == "" || a.Title == "" {
		return fmt.Errorf("application %q: it needs a name and a title", a.Name)
	}
	if len(a.Pages) == 0 && len(a.Resources) == 0 {
		return fmt.Errorf("application %s: it needs pages or declared resources", a.Name)
	}
	if a.Icon != "" && !platform.IconName(a.Icon) {
		return fmt.Errorf("application %s: %q is not an icon name", a.Name, a.Icon)
	}
	for _, page := range a.Pages {
		ref := platform.AssetRef{App: id, Kind: platform.AssetPage, Name: page}
		pageDefinition := slices.IndexFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
		if pageDefinition >= 0 {
			if err := a.CheckPageVariables(*t.definitions[pageDefinition].Page); err != nil {
				return err
			}
		}
		if pageDefinition < 0 {
			return fmt.Errorf("application %s: no page %s", a.Name, page)
		}
	}
	if err := a.CheckVariables(); err != nil {
		return err
	}
	if err := t.checkPageQueries(a.QueryPage()); err != nil {
		return err
	}
	if err := a.CheckResources(); err != nil {
		return fmt.Errorf("application %s: %w", a.Name, err)
	}
	for _, ref := range a.Resources {
		known := slices.ContainsFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
		if !known && (ref.Kind == platform.AssetAgent || t.ownedMetadataKind(ref)) {
			registered, err := t.ownedAssetDefinitions(t.definitions)
			if err != nil {
				return err
			}
			known = slices.ContainsFunc(registered, func(d platform.Definition) bool { return d.Ref == ref })
		}
		if ref.Kind == platform.AssetFlow {
			known = t.procs != nil && strings.HasPrefix(ref.Name, ref.App+".") && t.procs.HasPublishedFlow(ref.Name)
		}
		if !known {
			return fmt.Errorf("application %s: no published resource %s", a.Name, ref)
		}
	}
	if err := a.CheckHeader(); err != nil {
		return err
	}
	if err := a.CheckGroups(); err != nil {
		return fmt.Errorf("application %s: %v", a.Name, err)
	}
	installed := a
	def := platform.Definition{Ref: platform.AssetRef{App: id, Kind: platform.AssetApp, Name: a.Name}, Source: "tenant", Version: "1",
		ContractVersion: 1, Application: &installed}
	def, err := t.describeDefinition(def)
	if err != nil {
		return err
	}
	t.storeDefinition(slices.IndexFunc(t.definitions, func(x platform.Definition) bool { return x.Ref == def.Ref }), def)
	return nil
}

func (t *Tenant) checkApplicationExtension(ref platform.AssetRef, a platform.Application) error {
	if err := a.CheckExtension(); err != nil {
		return err
	}
	if a.Extends == nil {
		return nil
	}
	if *a.Extends == ref {
		return fmt.Errorf("an application cannot extend itself")
	}
	if !slices.ContainsFunc(t.definitions, func(definition platform.Definition) bool {
		return definition.Ref == *a.Extends && definition.Source == "code" && definition.Application != nil
	}) {
		return fmt.Errorf("application %s can only extend an installed code application", ref)
	}
	entry := a.Entry
	if entry == "" {
		entry = a.Extends.App
	}
	for _, other := range t.definitions {
		if other.Ref == ref || other.Application == nil || other.Application.Extends == nil {
			continue
		}
		otherEntry := other.Application.Entry
		if otherEntry == "" {
			otherEntry = other.Application.Extends.App
		}
		if otherEntry != entry {
			continue
		}
		for _, replacement := range a.Replacements {
			if slices.ContainsFunc(other.Application.Replacements, func(previous platform.ApplicationPageReplacement) bool { return previous.View == replacement.View }) {
				return fmt.Errorf("application view %s/%s is already replaced by %s", entry, replacement.View, other.Ref)
			}
		}
	}
	return nil
}

func (t *Tenant) checkPageQueries(p platform.Page) error {
	bindings := pageowner.Bindings{Entity: t.entity, Criteria: t.modelCriteria, Rules: t.services.Rules,
		Operation: t.pageOperation, Definitions: t.definitions, Interface: t.bindInterfaceQuery}
	return bindings.CheckQueries(p)
}
