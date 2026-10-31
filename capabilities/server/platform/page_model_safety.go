package platform

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func (p Page) Safety() (Page, error) {
	if p.Format == 3 {
		if p.retainedVersion == "" {
			version, err := PageContentVersion(p)
			if err != nil {
				return Page{}, err
			}
			p.retainedVersion = version
		}
		return p, nil
	}
	if p.Format != 0 {
		return Page{}, fmt.Errorf("unsupported page format %d", p.Format)
	}
	if p.Document != nil && (p.Document.FormatVersion != 2 || !SupportsPageUIProfile(p.Document.UIProfile)) {
		return Page{}, fmt.Errorf("unsupported retained page profile")
	}
	original := p
	p.Format = 3
	p.DataBindings = []PageDataBinding{}
	p.ActionTriggers = []PageActionTrigger{{ID: "page", Binding: "page", Actions: original.Actions}}
	p.InputSchema = PageInputSchema{Selections: original.Selections}
	p.ScopeVisibility = PageScopeVisibility{Root: "page", Owners: map[string]string{}, Scopes: []PageScope{{ID: "page", Kind: "page"}}}
	if original.Document != nil {
		d := original.Document
		p.ScopeVisibility.Scopes = nil
		p.InputSchema.Variables, p.InputSchema.Queries, p.InputSchema.Interface = d.Variables, d.Queries, d.Interface
		p.InputSchema.profile = d.UIProfile
		parent := map[string]string{}
		for id, node := range d.Nodes {
			for _, child := range node.Children {
				parent[child] = id
			}
		}
		for id, overlay := range d.Overlays {
			parent[overlay.Root] = id
		}
		for _, unused := range d.UnusedWidgets {
			parent[unused.Node] = unused.Parent
		}
		p.ScopeVisibility.Root = d.Root
		for _, id := range slices.Sorted(maps.Keys(d.Nodes)) {
			node := d.Nodes[id]
			scope := PageScope{ID: id, Parent: parent[id], Kind: "page", Binding: node.Section, Bindings: map[string]string{"valueVariable": node.ValueVariable, "activeVariable": node.ActiveVariable, "visibleWhen": node.VisibleWhen, "enabledWhen": node.EnabledWhen}}
			if node.Loop != nil {
				scope.Kind = "loop"
				scope.MaxIterations = node.Loop.Limit
				scope.Bindings["collection"], scope.Bindings["itemVariable"] = node.Loop.Collection, node.Loop.ItemVariable
			}
			for _, unused := range d.UnusedWidgets {
				if unused.Node == id {
					scope.Inactive = true
				}
			}
			p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, scope)
			if node.Section != "" {
				p.ScopeVisibility.Owners[node.Section] = id
			}
		}
		for _, id := range slices.Sorted(maps.Keys(d.Overlays)) {
			overlay := d.Overlays[id]
			p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, PageScope{ID: id, Kind: "overlay", Bindings: map[string]string{"root": overlay.Root, "openVariable": overlay.OpenVariable}})
		}
		for i, event := range d.Events {
			p.ActionTriggers = append(p.ActionTriggers, PageActionTrigger{ID: fmt.Sprintf("event:%s:%d", event.Source, i), Binding: event.Source, Events: []PageEventBinding{event}})
		}
	}
	visualSections := []map[string]any{}
	sections := original.Sections
	if len(sections) == 0 {
		sections = []Section{{ID: "list", Widget: "table", Fields: original.ListFields, Object: original.Object}, {ID: "detail", Widget: "detail", Fields: original.DetailFields, Object: original.Object}}
	}
	for index, s := range sections {
		id := s.ID
		if id == "" {
			id = fmt.Sprintf("section-%d", index+1)
		}
		d := PageDataBinding{ID: id, Owner: p.ScopeVisibility.Owners[id], Kind: "records", Object: s.ModelObject(original), Model: s.Model, Ports: map[string]string{}, FieldSlots: map[string][]FieldRef{}}
		if original.Document == nil {
			scopeID := "binding:" + id
			p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, PageScope{ID: scopeID, Parent: p.ScopeVisibility.Root, Kind: "page", Binding: id})
			p.ScopeVisibility.Owners[id] = scopeID
			d.Owner = scopeID
		}
		if original.Document != nil && s.CollectionVariable != "" {
			variable := original.Document.Variables[s.CollectionVariable]
			if variable.Source != nil && variable.Source.Kind == "plan" {
				d.Limit = original.Document.Queries[variable.Source.Query].Limit
			}
		}
		d.Ports["widget"] = s.Widget
		if s.RecordVariable != "" || WidgetReadsSelection(s.Widget) {
			d.Kind = "record"
		}

		if s.Query.Name != "" {
			d.Query = &AssetBinding{Ref: s.Query}
		}
		for _, name := range s.Fields {
			d.Fields = append(d.Fields, legacyFieldRef(name))
		}
		bytes, _ := json.Marshal(s)
		if err := checkLegacyPageProfile(original.Document, s, bytes); err != nil {
			return Page{}, err
		}
		var wire map[string]any
		_ = json.Unmarshal(bytes, &wire)
		if composite, ok := wire["composite"]; ok {
			var value struct {
				Bindings map[string]ModelViewBinding
				Value    *Expr
				Acyclic  bool
			}
			if err := json.Unmarshal(Raw(composite), &value); err != nil {
				return Page{}, err
			}
			d.Kind = "composite"
			d.Models = value.Bindings
			d.Value = value.Value
			d.Acyclic = value.Acyclic
		}
		for name, value := range s.variablePorts() {
			d.Ports[name] = value
		}
		for _, key := range []string{"selection", "parentSelection", "relation", "filterSearchVariable", "collectionOutputVariable", "measure"} {
			if value, ok := wire[key].(string); ok && value != "" {
				d.Ports[key] = value
			}
		}
		if contract := pageWidget(s.Widget); contract != nil && (contract.SelectionMode == "read" || contract.SelectionMode == "write") {
			d.Ports["selectionMode"] = contract.SelectionMode
		}
		if slices.Contains([]string{"table", "chart", "metric", "form"}, s.Widget) {
			d.Ports["parentable"] = "true"
		}
		if s.Widget == "form" {
			d.Ports["creates"] = "true"
		}
		for _, path := range modelScalarSlots {
			if _, already := d.FieldSlots[path]; already {
				continue
			}
			for _, value := range legacyPagePathValues(wire, strings.Split(path, ".")) {
				text, ok := value.(string)
				if !ok || text == "" || path == "measure" && text == "count" {
					continue
				}
				field := FieldRef{Name: text}
				if left, right, ok := strings.Cut(text, ":"); ok {
					if path == "measure" {
						field.Name, field.Transform = right, left
					} else {
						field.Name, field.Transform = left, right
					}
				}
				d.FieldSlots[path] = []FieldRef{field}
			}
		}
		for _, path := range []string{"map.latitudeField", "map.longitudeField", "map.labelField", "map.colorField", "avatar.detailFields", "collectionBuilder.fields", "detailPresentation.barcode", "tableColumns.field", "scene.sampleTimeField", "scene.sampleAssetField", "scene.mappings.field"} {
			fields := []FieldRef{}
			for _, value := range legacyPagePathValues(wire, strings.Split(path, ".")) {
				switch v := value.(type) {
				case string:
					if v != "" {
						fields = append(fields, FieldRef{Name: v})
					}
				case []any:
					for _, name := range v {
						if text, ok := name.(string); ok && text != "" {
							fields = append(fields, FieldRef{Name: text})
						}
					}
				}
			}
			if len(fields) > 0 {
				d.FieldSlots[path] = fields
			}
		}
		if contract := pageWidget(s.Widget); contract != nil {
			for _, role := range contract.FieldRoles {
				for _, field := range legacyPagePathValues(wire, strings.Split(role.Path, ".")) {
					if text, ok := field.(string); ok && text != "" {
						d.FieldSlots[role.Path] = append(d.FieldSlots[role.Path], FieldRef{Name: text})
					}
				}
			}
		}
		for index, facet := range s.Facets {
			d.Resources = append(d.Resources, PageResourceBinding{ID: fmt.Sprintf("facet:%d", index), Kind: "filter", Source: facet.Kind, Fields: []FieldRef{{Name: facet.Field}}, Outputs: map[string]string{"value": facet.Variable}})
		}
		for _, path := range []string{"avatar.contextVariable", "avatar.contextCollectionVariable", "ai.questionVariable"} {
			for _, value := range legacyPagePathValues(wire, strings.Split(path, ".")) {
				if text, ok := value.(string); ok && text != "" {
					d.Ports[path] = text
				}
			}
		}
		resources, err := legacyPageResources(bytes, s.Widget)
		if err != nil {
			return Page{}, err
		}
		d.Resources = append(d.Resources, resources...)
		if d.Model == nil && d.Models == nil && d.Object.Name != "" {
			d.Model = &ModelBinding{Type: d.Object.Name, Fields: slices.Clone(d.Fields)}
		}
		for slot, fields := range d.FieldSlots {
			for i, field := range fields {
				if strings.Contains(field.Name, ".") {
					field.Path = strings.Split(field.Name, ".")
					field.Name = ""
				}
				fields[i] = field
			}
			d.FieldSlots[slot] = fields
		}

		if s.Model == nil && d.Models == nil && s.Object.Name == "" && s.Query.Name == "" && len(s.Fields) == 0 && s.CollectionVariable == "" && s.RecordVariable == "" && s.Selection == "" && len(d.FieldSlots) == 0 {
			d.Object = AssetRef{}
			d.Model = nil
			d.Kind = "presentation"
		}
		p.DataBindings = append(p.DataBindings, d)
		defaults := s.ActionDefaults
		var actionTable struct{ Parameters []PageActionParameter }
		if value, ok := wire["actionTable"]; ok {
			if err := json.Unmarshal(Raw(value), &actionTable); err != nil {
				return Page{}, err
			}
			defaults = actionTable.Parameters
		}
		p.ActionTriggers = append(p.ActionTriggers, PageActionTrigger{ID: id, Binding: id, Owner: d.Owner, Actions: s.Actions, Function: s.Function, Operation: s.Operation, Inputs: s.Inputs, Defaults: defaults, InlineEdit: s.InlineEdit})
		paths := append(append([]string{}, pageCompatibility.Section.BindingPaths...), pageCompatibility.Section.ResourcePaths...)
		paths = append(paths, pageCompatibility.ScalarFieldPaths...)
		for path := range d.FieldSlots {
			paths = append(paths, path)
		}
		for _, path := range paths {
			stripLegacyPagePath(wire, strings.Split(path, "."))
		}
		for key := range d.Ports {
			stripLegacyPagePath(wire, strings.Split(key, "."))
		}
		wire["id"], wire["binding"], wire["triggers"] = id, id, []string{id}
		visualSections = append(visualSections, wire)
	}
	var visualDocument any
	if original.Document != nil {
		raw, _ := json.Marshal(original.Document)
		var doc map[string]any
		_ = json.Unmarshal(raw, &doc)
		for _, key := range []string{"variables", "queries", "interface", "events"} {
			delete(doc, key)
		}
		if nodes, ok := doc["nodes"].(map[string]any); ok {
			for _, node := range nodes {
				for _, path := range pageCompatibility.Scope.BindingPaths {
					stripLegacyPagePath(node, strings.Split(path, "."))
				}
			}
		}
		if overlays, ok := doc["overlays"].(map[string]any); ok {
			for _, overlay := range overlays {
				for _, path := range pageCompatibility.Scope.OverlayPaths {
					stripLegacyPagePath(overlay, strings.Split(path, "."))
				}
			}
		}
		visualDocument = doc
	}
	body := map[string]any{"sections": visualSections, "document": visualDocument}
	if original.Layout == "list-detail" && len(original.Sections) == 0 {
		body["layout"] = original.Layout
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Page{}, err
	}
	p.WidgetSpec = PageWidgetSpec{Format: "platform.page.web.v1", Body: raw}
	if version, err := PageContentVersion(original); err == nil {
		p.retainedVersion = version
	}
	p.Sections, p.Document, p.Selections = nil, nil, nil
	p.ListFields, p.DetailFields, p.Actions = nil, nil, nil
	p.Layout = ""
	return p, nil
}

func (p Page) WithSafetyProjection(projected Page) Page {
	result := projected
	result.WidgetSpec = p.WidgetSpec
	result.retainedVersion = p.retainedVersion
	return result
}

func (p Page) VisibleSafety() Page {
	result := p
	result.DataBindings = append([]PageDataBinding{}, p.DataBindings...)
	result.InputSchema.Variables = maps.Clone(p.InputSchema.Variables)
	result.InputSchema.Queries = maps.Clone(p.InputSchema.Queries)
	scopes := map[string]PageScope{}
	for _, s := range p.ScopeVisibility.Scopes {
		scopes[s.ID] = s
	}
	for changed := true; changed; {
		changed = false
		allowed := map[string]bool{}
		for _, b := range result.DataBindings {
			allowed[b.ID] = true
		}
		for id, v := range result.InputSchema.Variables {
			missing := false
			if src := v.Source; src != nil {
				missing = src.Section != "" && !allowed[src.Section]
				if src.Variable != "" && src.Kind != "application" {
					_, ok := result.InputSchema.Variables[src.Variable]
					missing = missing || !ok
				}
				if src.Query != "" {
					_, ok := result.InputSchema.Queries[src.Query]
					missing = missing || !ok
				}
				if src.Compute != nil {
					_, ok := result.InputSchema.Variables[src.Compute.RecordVariable]
					missing = missing || !ok
				}
			}
			if v.Scope == "loop-item" {
				scope, ok := scopes[v.Owner]
				_, input := result.InputSchema.Variables[scope.Bindings["collection"]]
				missing = missing || !ok || !input
			}
			if v.Expression != nil {
				for _, arg := range v.Expression.Args {
					if arg.Variable != "" {
						_, ok := result.InputSchema.Variables[arg.Variable]
						missing = missing || !ok
					}
				}
			}
			var reads func(Expr)
			reads = func(e Expr) {
				if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") {
					_, ok := result.InputSchema.Variables[e.Ref]
					missing = missing || !ok
				}
				if e.Agg != nil {
					_, ok := result.InputSchema.Variables[e.Agg.Source]
					missing = missing || !ok
				}
				for _, a := range e.Args {
					reads(a)
				}
				if e.In != nil {
					reads(*e.In)
				}
			}
			if v.Rule != nil {
				reads(*v.Rule)
			}
			if missing {
				delete(result.InputSchema.Variables, id)
				changed = true
			}
		}
		for id, q := range result.InputSchema.Queries {
			missing := q.MissingSetInput(result.InputSchema.Queries)
			for _, v := range q.Variables() {
				_, ok := result.InputSchema.Variables[v]
				missing = missing || !ok
			}
			if missing {
				delete(result.InputSchema.Queries, id)
				changed = true
			}
		}
		bindings := result.DataBindings[:0]
		for _, b := range result.DataBindings {
			valid := true
			for key, id := range b.Ports {
				if strings.HasSuffix(key, "Variable") && id != "" {
					_, ok := result.InputSchema.Variables[id]
					valid = valid && ok
				}
			}
			if valid {
				bindings = append(bindings, b)
			} else {
				changed = true
			}
		}
		result.DataBindings = bindings
	}
	allowed := map[string]bool{}
	for _, b := range result.DataBindings {
		allowed[b.ID] = true
	}
	kept := map[string]bool{p.ScopeVisibility.Root: true}
	for _, s := range p.ScopeVisibility.Scopes {
		valid := s.Binding == "" || allowed[s.Binding]
		for _, key := range []string{"valueVariable", "activeVariable", "visibleWhen", "enabledWhen", "collection", "itemVariable", "openVariable"} {
			if id := s.Bindings[key]; id != "" {
				_, ok := result.InputSchema.Variables[id]
				valid = valid && ok
			}
		}
		if valid {
			kept[s.ID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for id := range kept {
			if parent := scopes[id].Parent; parent != "" && !kept[parent] {
				delete(kept, id)
				changed = true
			}
		}
	}
	result.ScopeVisibility.Scopes = nil
	result.ScopeVisibility.Owners = map[string]string{}
	for _, s := range p.ScopeVisibility.Scopes {
		if kept[s.ID] {
			result.ScopeVisibility.Scopes = append(result.ScopeVisibility.Scopes, s)
			if s.Binding != "" {
				result.ScopeVisibility.Owners[s.Binding] = s.ID
			}
		}
	}
	result.ActionTriggers = nil
	for _, trigger := range p.ActionTriggers {
		if trigger.Binding != "page" && !allowed[trigger.Binding] && !kept[trigger.Binding] {
			continue
		}
		copy := trigger
		copy.Events = nil
		for _, event := range trigger.Events {
			valid := true
			for _, id := range append(event.Variables(), event.Reads()...) {
				_, ok := result.InputSchema.Variables[id]
				valid = valid && ok
			}
			if valid {
				copy.Events = append(copy.Events, event)
			}
		}
		result.ActionTriggers = append(result.ActionTriggers, copy)
	}
	if iface := p.InputSchema.Interface; iface != nil {
		copy := *iface
		copy.Inputs = maps.Clone(iface.Inputs)
		copy.Outputs = maps.Clone(iface.Outputs)
		for _, ports := range []map[string]PagePort{copy.Inputs, copy.Outputs} {
			for id, port := range ports {
				if _, ok := result.InputSchema.Variables[port.Variable]; !ok {
					delete(ports, id)
				}
			}
		}
		result.InputSchema.Interface = &copy
	}
	return p.WithSafetyProjection(result)
}

func (p Page) CheckSafety() error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, binding := range safe.DataBindings {
		if !pageNodeID.MatchString(binding.ID) || ids[binding.ID] {
			return fmt.Errorf("page data bindings need unique stable IDs")
		}
		ids[binding.ID] = true
	}
	scopes := map[string]bool{}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if !slices.Contains([]string{"page", "application", "overlay", "loop"}, scope.Kind) {
			return fmt.Errorf("unknown page scope kind")
		}
		if !pageNodeID.MatchString(scope.ID) || scopes[scope.ID] {
			return fmt.Errorf("page scopes need unique stable IDs")
		}
		scopes[scope.ID] = true
		if scope.Binding != "" && !ids[scope.Binding] {
			return fmt.Errorf("page scope binding is unavailable")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if scope.Parent != "" && !scopes[scope.Parent] {
			return fmt.Errorf("page scope parent is unavailable")
		}
		if scope.ID == safe.ScopeVisibility.Root && scope.Parent != "" {
			return fmt.Errorf("the page root scope has no parent")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		seen := map[string]bool{}
		owner := scope.ID
		for owner != "" {
			if seen[owner] {
				return fmt.Errorf("page scope graph contains a cycle")
			}
			seen[owner] = true
			next := ""
			for _, parent := range safe.ScopeVisibility.Scopes {
				if parent.ID == owner {
					next = parent.Parent
					break
				}
			}
			owner = next
		}
		if scope.Kind == "loop" && (scope.MaxIterations < 1 || scope.MaxIterations > pageWidgets.Runtime.Loop.MaxItems || safe.InputSchema.Variables[scope.Bindings["collection"]].Type != "object-set" || safe.InputSchema.Variables[scope.Bindings["itemVariable"]].Type != "record") {
			return fmt.Errorf("page loop needs its bounded typed collection and item")
		}
	}
	for _, binding := range safe.DataBindings {
		owner := safe.ScopeVisibility.Owners[binding.ID]
		if owner == "" {
			owner = binding.Owner
		}
		for _, id := range pageBindingVariables(binding) {
			if id == "" {
				continue
			}
			variable, ok := safe.InputSchema.Variables[id]
			if !ok {
				return fmt.Errorf("page binding variable %s is unavailable", id)
			}
			if variable.Scope == "overlay" && safe.scopeOwner(owner, "overlay") != variable.Owner || variable.Scope == "loop-item" && safe.scopeOwner(owner, "loop") != variable.Owner {
				return fmt.Errorf("page binding variable escapes its original owner")
			}
		}
		if err := safe.checkWidgetPorts(binding, owner); err != nil {
			return err
		}
	}
	triggers := map[string]bool{}
	for _, trigger := range safe.ActionTriggers {
		if trigger.ID == "" || triggers[trigger.ID] || trigger.Binding != "page" && !ids[trigger.Binding] && !scopes[trigger.Binding] {
			return fmt.Errorf("page trigger needs a unique ID and original owner")
		}
		triggers[trigger.ID] = true
	}
	if err := safe.checkEnvelope(); err != nil {
		return err
	}
	if err := safe.InputSchema.checkProfile(); err != nil {
		return err
	}
	if err := safe.InputSchema.CheckVariables(); err != nil {
		return err
	}
	if err := safe.checkQueries("page"); err != nil {
		return err
	}
	if err := safe.InputSchema.checkInterface(); err != nil {
		return err
	}
	if safe.Object.Name != "" {
		if err := safe.CheckRecordPorts(); err != nil {
			return err
		}
	}
	return safe.checkEvents()
}
