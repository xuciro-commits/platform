package pages

import (
	"fmt"
	"maps"
	"platformserver/platform"
	"regexp"
	"slices"
	"strings"
)

// DefinitionProjection receives already authorized declarations. Retained
// reads and the original query compiler remain captured by the host; this
// owner cannot submit, query arbitrary records or reach a tenant.
type DefinitionProjection struct {
	Member      platform.Member
	Entities    map[string]platform.EntityInfo
	Actions     map[string]platform.Action
	Definitions []platform.Definition
	Criteria    func(platform.AssetRef) (*platform.RuleCriteria, bool)
	Function    func(platform.AssetBinding) (platform.AIFunction, bool)
	Operation   func(*platform.AssetBinding) (platform.Operation, bool)
	CheckQuery  func(platform.NamedQuery, platform.EntityInfo) error
}

// page projects only the four typed safety declarations. Presentation bytes
// retain their original identity and can never add a read, action or source.
func (projection DefinitionProjection) Page(def platform.Definition) (platform.Definition, bool) {
	m, entities, actions := projection.Member, projection.Entities, projection.Actions
	if m.Roles[def.Ref.App] == "" || def.Page == nil {
		return def, false
	}
	page, err := def.Page.Safety()
	if err != nil {
		return def, false
	}
	digest, err := platform.PageContentVersion(page)
	if err != nil {
		return def, false
	}
	def.ContentVersion = digest
	lookup := func(typ string) (platform.EntityInfo, bool) { info, ok := entities[typ]; return info, ok }
	page = page.ResolveBindings(lookup)
	visible := page
	actionVisible := func(ref platform.AssetRef) bool {
		action, ok := actions[ref.Name]
		return ok && !platform.StaticControlAction(action)
	}
	fieldVisible := func(info platform.EntityInfo, ref platform.FieldRef) bool {
		_, err := platform.RecordPathField(info.Type, ref.Segments(), lookup)
		return err == nil
	}
	visible.DataBindings = make([]platform.PageDataBinding, 0, len(page.DataBindings))
	bindingIDs := map[string]bool{}
	for _, binding := range page.DataBindings {
		info, ok := entities[binding.Object.Name]
		if binding.Object.Name != "" && (!ok || info.App != binding.Object.App) {
			continue
		}
		if binding.Model != nil {
			model := *binding.Model
			var ok bool
			info, ok = lookup(model.Type)
			if !ok {
				continue
			}
			model.Fields = slices.DeleteFunc(slices.Clone(model.Fields), func(ref platform.FieldRef) bool { return !fieldVisible(info, ref) })
			binding.Model = &model
		}

		binding.Models = maps.Clone(binding.Models)
		hiddenModel := false
		for port, view := range binding.Models {
			modelInfo, ok := entities[view.Model.Type]
			if !ok {
				hiddenModel = true
				break
			}
			view.Model.Fields = slices.DeleteFunc(slices.Clone(view.Model.Fields), func(ref platform.FieldRef) bool { return !fieldVisible(modelInfo, ref) })
			view.Roles = maps.Clone(view.Roles)
			for role, field := range view.Roles {
				if !fieldVisible(modelInfo, field) {
					hiddenModel = true
					break
				}
				view.Roles[role] = field
			}
			if hiddenModel {
				break
			}
			binding.Models[port] = view
		}
		if hiddenModel {
			continue
		}
		binding.Fields = slices.DeleteFunc(slices.Clone(binding.Fields), func(ref platform.FieldRef) bool { return !fieldVisible(info, ref) })
		binding.FieldSlots = maps.Clone(binding.FieldSlots)
		for slot, fields := range binding.FieldSlots {
			filtered := slices.DeleteFunc(slices.Clone(fields), func(ref platform.FieldRef) bool { return !fieldVisible(info, ref) })
			if len(filtered) == 0 {
				delete(binding.FieldSlots, slot)
			} else {
				binding.FieldSlots[slot] = filtered
			}
		}
		if binding.Query != nil {
			named := false
			for _, definition := range projection.Definitions {
				if definition.Ref != binding.Query.Ref {
					continue
				}
				selected := definition.QuerySourceVersion(binding.Query.SourceVersion)
				if selected == nil {
					continue
				}
				if selected.Query != nil {
					source, ok := entities[selected.Query.Object]
					named = ok && projection.CheckQuery(*selected.Query, source) == nil
				}
				if selected.LinkType != nil {
					parent, pok := entities[selected.LinkType.Parent.Name]
					child, cok := entities[selected.LinkType.Child.Name]
					named = pok && cok && selected.LinkType.CheckSchema(parent, child) == nil
				}
			}
			if !named {
				continue
			}
		}
		binding.Resources = slices.DeleteFunc(slices.Clone(binding.Resources), func(resource platform.PageResourceBinding) bool {
			if resource.Object != nil {
				source, ok := entities[resource.Object.Name]
				if !ok || source.App != resource.Object.App {
					return true
				}
				if slices.ContainsFunc(resource.Fields, func(field platform.FieldRef) bool { return !fieldVisible(source, field) }) {
					return true
				}
			}
			return false
		})
		if page.CheckDataBinding(binding, lookup, projection.Criteria) != nil {
			continue
		}
		visible.DataBindings = append(visible.DataBindings, binding)
		bindingIDs[binding.ID] = true
	}
	visible.ActionTriggers = make([]platform.PageActionTrigger, 0, len(page.ActionTriggers))
	for _, trigger := range page.ActionTriggers {
		if trigger.Binding != "" && trigger.Binding != "page" && !bindingIDs[trigger.Binding] {
			continue
		}
		trigger.Actions = slices.DeleteFunc(slices.Clone(trigger.Actions), func(ref platform.AssetRef) bool { return !actionVisible(ref) })
		// An explicit empty action set remains empty; omitted actions remain nil.
		info := platform.EntityInfo{}
		for _, binding := range visible.DataBindings {
			if binding.ID == trigger.Binding {
				info = entities[binding.Object.Name]
				break
			}
		}
		if trigger.InlineEdit != nil {
			edit := *trigger.InlineEdit
			if !actionVisible(edit.Action) {
				continue
			}
			edit.Fields = slices.DeleteFunc(slices.Clone(edit.Fields), func(name string) bool {
				field, ok := info.Field(name)
				return !ok || field.ReadOnly || !field.WritesAny(m.RolesIn(info.App))
			})
			if len(edit.Fields) == 0 {
				continue
			}
			trigger.InlineEdit = &edit
		}
		trigger.Events = slices.DeleteFunc(slices.Clone(trigger.Events), func(event platform.PageEventBinding) bool {
			return slices.ContainsFunc(event.Effects, func(effect platform.PageEffect) bool {
				return effect.Action != nil && !actionVisible(effect.Action.Ref)
			})
		})
		if trigger.Function != nil {
			function, ok := projection.Function(*trigger.Function)
			if !ok || !m.May(trigger.Function.Ref.App, function.Name, function.Roles).Allow {
				continue
			}
			source, ok := entities[function.Object]
			if !ok || slices.ContainsFunc(function.Fields, func(name string) bool {
				field, ok := source.Field(name)
				return !ok || !field.ReadsAny(m.RolesIn(source.App))
			}) {
				continue
			}
		}
		if trigger.Operation != nil {
			operation, ok := projection.Operation(trigger.Operation)
			if !ok || !m.May(trigger.Operation.Ref.App, operation.Name, operation.Roles).Allow {
				continue
			}
		}
		if slices.ContainsFunc(slices.Collect(maps.Values(trigger.Inputs)), func(input platform.Binding) bool {
			if input.Source != "subject" {
				return false
			}
			_, err := platform.RecordPathField(info.Type, input.Path, lookup)
			return err != nil
		}) {
			continue
		}
		visible.ActionTriggers = append(visible.ActionTriggers, trigger)
	}
	visible.InputSchema = page.InputSchema
	visible.InputSchema.Selections = slices.DeleteFunc(slices.Clone(page.InputSchema.Selections), func(selection platform.SelectionVariable) bool {
		info, ok := entities[selection.Object.Name]
		return !ok || info.App != selection.Object.App
	})
	visible.InputSchema.Queries = maps.Clone(page.InputSchema.Queries)
	for id, query := range visible.InputSchema.Queries {
		if query.Interface != "" {
			if VisibleInterfaceQuery(page, query, entities, projection.Definitions) == nil {
				delete(visible.InputSchema.Queries, id)
			}
			continue
		}
		info, ok := entities[query.Object.Name]
		if !ok || info.App != query.Object.App {
			delete(visible.InputSchema.Queries, id)
			continue
		}
		if query.Query != nil {
			found := false
			for _, definition := range projection.Definitions {
				if definition.Ref == query.Query.Ref {
					if selected := definition.QuerySourceVersion(query.Query.SourceVersion); selected != nil && selected.Query != nil {
						found = projection.CheckQuery(*selected.Query, info) == nil
					}
				}
			}
			if !found {
				delete(visible.InputSchema.Queries, id)
			}
		}
	}
	visible.InputSchema.Variables = maps.Clone(page.InputSchema.Variables)
	for id, variable := range visible.InputSchema.Variables {
		source := variable.Source
		if source == nil {
			continue
		}
		if source.Object != nil {
			if info, ok := entities[source.Object.Name]; !ok || info.App != source.Object.App {
				delete(visible.InputSchema.Variables, id)
				continue
			}
		}
		if source.Query != "" {
			if _, ok := visible.InputSchema.Queries[source.Query]; !ok {
				delete(visible.InputSchema.Variables, id)
				continue
			}
		}
		if source.Compute != nil {
			operation, ok := projection.Operation(&source.Compute.Operation)
			if !ok || !m.May(source.Compute.Operation.Ref.App, operation.Name, operation.Roles).Allow {
				delete(visible.InputSchema.Variables, id)
			}
		}
	}
	visible = page.WithSafetyProjection(visible.VisibleSafety())
	def.Page = &visible
	return def, true
}

func (b Bindings) CheckOperation(s platform.PageActionTrigger, page platform.EntityInfo) error {
	op, _, err := b.Operation(s.Operation)
	if err != nil {
		return err
	}
	if len(s.Inputs) == 0 {
		return nil // operator supplies the complete typed value
	}
	if op.Input.Type != "object" || len(s.Inputs) > 64 {
		return fmt.Errorf("field bindings need an object input schema")
	}
	for _, name := range op.Input.Required {
		if _, exists := s.Inputs[name]; !exists {
			return fmt.Errorf("input %s needs a binding", name)
		}
	}
	for name, binding := range s.Inputs {
		field, exists := op.Input.Properties[name]
		if !exists || binding.Check() != nil || binding.Source != "literal" && binding.Source != "input" && binding.Source != "subject" {
			return fmt.Errorf("input %s needs a supported typed binding", name)
		}
		if binding.Source == "literal" && field.Validate(binding.Value, 64<<10) != nil {
			return fmt.Errorf("input %s does not match its schema", name)
		}
		if binding.Source == "subject" && len(binding.Path) > 0 {
			if _, exists := page.Field(binding.Path[0]); !exists {
				return fmt.Errorf("input %s references an unknown source field", name)
			}
		}
	}
	return nil
}

var selectionName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// CheckSelections holds record selections, parent relations and create forms
// to what the page's objects declare, from the safety contract alone: which
// binding supplies a selection, which follows a parent, and what a create form
// still has to ask for. The widgets' look never takes part.
func CheckSelections(p platform.Page, entity func(string) (platform.EntityInfo, bool), action func(string) (platform.Action, bool)) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	selections := map[string]string{}
	for _, selection := range safe.InputSchema.Selections {
		info, ok := entity(selection.Object.Name)
		switch {
		case !selectionName.MatchString(selection.Name) || selections[selection.Name] != "":
			return fmt.Errorf("page %s selections need unique lowercase names", p.Name)
		case !ok || info.App != selection.Object.App || selection.Object.Kind != platform.AssetObject:
			return fmt.Errorf("page %s selection %s needs a known object", p.Name, selection.Name)
		}
		selections[selection.Name] = selection.Object.Name
	}
	supplies := func(selection, object string) bool {
		return slices.ContainsFunc(safe.DataBindings, func(b platform.PageDataBinding) bool {
			return b.Ports["selectionMode"] == "write" && b.Ports["selection"] == selection && b.ObjectRef(safe).Name == object
		})
	}
	references := func(info platform.EntityInfo, parent string) bool {
		return slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Type == "reference" && f.Ref == parent })
	}
	parentBindings := map[string]string{}
	for _, b := range safe.DataBindings {
		ref := b.ObjectRef(safe)
		info, ok := entity(ref.Name)
		if !ok {
			continue // the object's owner is checked with the binding itself
		}
		info = platform.ModelFields(info, b.PrimaryModel(), entity)
		where := fmt.Sprintf("page %s, binding %s", p.Name, b.ID)
		selection, parentSelection, relation := b.Ports["selection"], b.Ports["parentSelection"], b.Ports["relation"]
		parentType := safe.Object.Name
		if parentSelection != "" {
			if parentType = selections[parentSelection]; parentType == "" {
				return fmt.Errorf("%s: parent selection %q is not declared", where, parentSelection)
			}
		}
		if selection != "" {
			if selections[selection] != info.Type {
				return fmt.Errorf("%s: selection %q does not hold %s records", where, selection, info.Type)
			}
			if b.Ports["selectionMode"] == "" {
				return fmt.Errorf("%s: this widget does not use a record selection", where)
			}
			if !supplies(selection, info.Type) {
				return fmt.Errorf("%s: add a table that supplies selection %q", where, selection)
			}
		}
		if parentSelection != "" {
			if b.Ports["parentable"] == "" || !references(info, parentType) || b.Ports["creates"] != "" && relation == "" {
				return fmt.Errorf("%s: parent selection needs a related table, chart, metric or bound form", where)
			}
			if !supplies(parentSelection, parentType) {
				return fmt.Errorf("%s: add a table that supplies parent selection %q", where, parentSelection)
			}
		}
		if b.Ports["selectionMode"] == "write" && (info.Type != safe.Object.Name || parentSelection != "") && references(info, parentType) {
			key, parent := "object:"+info.Type, "object:"+parentType
			if selection != "" {
				key = "selection:" + selection
			}
			if parentSelection != "" {
				parent = "selection:" + parentSelection
			}
			if previous := parentBindings[key]; previous != "" && previous != parent {
				return fmt.Errorf("%s: record selection has conflicting parent bindings", where)
			}
			parentBindings[key] = parent
		}
		boundParent := ""
		if relation != "" {
			if b.Ports["parentable"] == "" {
				return fmt.Errorf("%s: only a table, chart, metric or form follows a relation", where)
			}
			at := slices.IndexFunc(info.Fields, func(f platform.FieldInfo) bool {
				return f.Type == "reference" && f.Ref == parentType && f.Inverse == relation
			})
			if at < 0 {
				return fmt.Errorf("%s: %s declares no relation %q from %s", where, info.Type, relation, parentType)
			}
			if b.Ports["creates"] != "" {
				if info.Fields[at].ReadOnly {
					return fmt.Errorf("%s: parent reference %s is read only", where, info.Fields[at].Name)
				}
				if !supplies(parentSelection, parentType) {
					return fmt.Errorf("%s: add a table that selects a parent record", where)
				}
				boundParent = info.Fields[at].Name
			}
		}
		if b.Ports["creates"] != "" {
			if err := checkCreateForm(safe, b, info, where, parentType, boundParent, relation, entity, action); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkCreateForm: a form makes a new record through the object's own create
// action, so it asks for everything that action needs, except a parent
// reference supplied by its explicitly declared relation.
func checkCreateForm(p platform.Page, b platform.PageDataBinding, info platform.EntityInfo, where, parentType, boundParent, relation string, entity func(string) (platform.EntityInfo, bool), action func(string) (platform.Action, bool)) error {
	if create, ok := action(info.Type + ".create"); !ok || create.Target != info.Type {
		return fmt.Errorf("%s: %s cannot be created here", where, info.Type)
	}
	var inputs map[string]platform.Binding
	for _, trigger := range p.ActionTriggers {
		if trigger.Binding == b.ID {
			inputs = trigger.Inputs
		}
	}
	names := make([]string, 0, len(b.Fields))
	for _, ref := range b.Fields {
		names = append(names, ref.Name)
	}
	if len(names) == 0 && boundParent == "" && len(inputs) == 0 {
		return fmt.Errorf("%s: no fields to fill in", where)
	}
	for _, name := range names {
		if _, ok := info.Field(strings.Split(name, ":")[0]); !ok {
			return fmt.Errorf("%s: %s has no field %s", where, info.Type, name)
		}
	}
	if len(inputs) > 64 {
		return fmt.Errorf("%s: form inputs exceed their bound", where)
	}
	for name, binding := range inputs {
		field, found := info.Field(name)
		if !found || field.ReadOnly || name == boundParent || binding.Check() != nil {
			return fmt.Errorf("%s: form input %s needs a writable declared field", where, name)
		}
		switch binding.Source {
		case "literal":
			if err := platform.CapabilityFieldSchema(field.Type, field.Title, field.Choices).Validate(binding.Value, 64<<10); err != nil {
				return fmt.Errorf("%s: form input %s: %w", where, name, err)
			}
		case "subject":
			if relation == "" {
				return fmt.Errorf("%s: form record inputs need a declared parent relation", where)
			}
			source, err := platform.RecordPathField(parentType, binding.Path, entity)
			if err != nil {
				return fmt.Errorf("%s: form input %s: %w", where, name, err)
			}
			if source.Type != field.Type && !(source.Type == "integer" && field.Type == "decimal") || source.Type == "reference" && source.Ref != field.Ref {
				return fmt.Errorf("%s: form input %s has incompatible source type %s", where, name, source.Type)
			}
		default:
			return fmt.Errorf("%s: form input %s uses a constant or declared record path", where, name)
		}
	}
	for _, f := range info.Fields {
		_, bound := inputs[f.Name]
		if f.Required && !f.ReadOnly && f.Name != boundParent && !bound && !slices.Contains(names, f.Name) {
			return fmt.Errorf("%s: %s needs %s, which the form does not ask for", where, info.Type, f.Name)
		}
	}
	return nil
}
