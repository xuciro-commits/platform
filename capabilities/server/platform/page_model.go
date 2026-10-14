package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const PageModelProfile = "platform.page.v2.110"
const PageModelMaxFields = 64

// FieldRef names a declared model field. Slot identifies the existing widget
// port; Path traverses single-record references, never arbitrary JSON members.
type FieldRef struct {
	Name      string   `json:"name,omitempty"`
	Path      []string `json:"path,omitempty"`
	Slot      string   `json:"slot,omitempty"`
	Transform string   `json:"transform,omitempty"`
}

func (f FieldRef) Segments() []string {
	if len(f.Path) > 0 {
		return f.Path
	}
	return []string{f.Name}
}
func (f FieldRef) Key() string { return strings.Join(f.Segments(), ".") }

// ModelRelation fixes the original reference field and typed parent selection.
// Legacy relation guessing is resolved once when a draft is saved.
type ModelRelation struct {
	ParentType      string `json:"parentType"`
	ParentSelection string `json:"parentSelection,omitempty"`
	Field           string `json:"field"`
	Name            string `json:"name,omitempty"`
}

// ModelRecordSet consumes an existing owner declaration. Name addresses an
// EntityInfo.Set; Query retains the original named query rather than a new DSL.
type ModelRecordSet struct {
	RecordVariable string        `json:"recordVariable,omitempty"`
	Name           string        `json:"name,omitempty"`
	Query          *AssetBinding `json:"query,omitempty"`
}

// ModelRule references the original action's pure conditions. Inputs bind page
// variables; the server confirms record references through their owners.
type ModelRule struct {
	Action AssetRef          `json:"action"`
	Inputs map[string]string `json:"inputs,omitempty"`
}
type OptionsRef struct {
	Set  *ModelRecordSet `json:"set,omitempty"`
	Rule *ModelRule      `json:"rule,omitempty"`
}

// ModelBinding is the one business binding carried by an existing Section.
// Empty Fields means visible model defaults; the Inspector exposes this choice.
type ModelBinding struct {
	Type       string          `json:"type"`
	Fields     []FieldRef      `json:"fields,omitempty"`
	Relation   *ModelRelation  `json:"relation,omitempty"`
	Set        *ModelRecordSet `json:"set,omitempty"`
	Where      []Expr          `json:"where,omitempty"`
	Label      *Expr           `json:"label,omitempty"`
	Options    *OptionsRef     `json:"options,omitempty"`
	Visibility string          `json:"visibility,omitempty" enum:"hide,mark"`
}

func ModelObject(typ string) AssetRef {
	app, _, _ := strings.Cut(typ, ".")
	return AssetRef{App: app, Kind: AssetObject, Name: typ}
}
func (s Section) ModelObject(page Page) AssetRef {
	if model := s.PrimaryModel(); model != nil {
		return ModelObject(model.Type)
	}
	if s.Object.Name != "" {
		return s.Object
	}
	return page.Object
}

// These are field ports already owned by widget contracts. Presentation text,
// variable identities, action parameters and nested source models are excluded.
var modelScalarSlots = pageWidgets.Runtime.ModelBinding.FieldSlots

func ModelFieldSlot(slot string) bool {
	return slot == "" || slot == "fields" || slices.Contains(modelScalarSlots, slot)
}

func modelSlot(values map[string]any, slot string) (map[string]any, string) {
	parts := strings.Split(slot, ".")
	for _, part := range parts[:len(parts)-1] {
		child, ok := values[part].(map[string]any)
		if !ok {
			return nil, ""
		}
		values = child
	}
	return values, parts[len(parts)-1]
}

func setModelSlot(values map[string]any, slot string, value any) {
	parts := strings.Split(slot, ".")
	for _, part := range parts[:len(parts)-1] {
		next, ok := values[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			values[part] = next
		}
		values = next
	}
	values[parts[len(parts)-1]] = value
}

// EffectiveModelSection is a transient adapter to the existing widget ports.
// It never writes the adapted bare names back into the published definition.
func (p Page) EffectiveModelSection(s Section, lookup func(string) (EntityInfo, bool)) Section {
	if s.Composite != nil {
		if model := s.PrimaryModel(); model != nil {
			s.Object = ModelObject(model.Type)
		}
		return s
	}
	if s.Model == nil {
		return s
	}
	copy := s
	copy.Object = ModelObject(s.Model.Type)
	if p.SectionInterface(s) != "" {
		copy.Object = AssetRef{}
	}
	if relation := s.Model.Relation; relation != nil {
		copy.ParentSelection, copy.Relation = relation.ParentSelection, relation.Name
	}
	values := map[string]any{}
	_ = json.Unmarshal(Raw(copy), &values)
	fields := []string{}
	for _, ref := range s.Model.Fields {
		if ref.Slot == "" || ref.Slot == "fields" {
			fields = append(fields, ref.Key())
			continue
		}
		value := ref.Key()
		if ref.Transform != "" {
			if ref.Slot == "measure" {
				value = ref.Transform + ":" + value
			} else {
				value += ":" + ref.Transform
			}
		}
		setModelSlot(values, ref.Slot, value)
	}
	if len(fields) == 0 && lookup != nil && (s.Widget == "table" || s.Widget == "detail" || s.Widget == "form" || s.Widget == "filter") {
		if info, ok := lookup(s.Model.Type); ok {
			for _, f := range info.Fields {
				if f.Aside || s.Widget == "form" && f.ReadOnly || s.Widget == "filter" && !slices.Contains(Filterable, f.Type) {
					continue
				}
				fields = append(fields, f.Name)
			}
		}
	}
	// A closure's structural check has no installed metadata. Preserve the
	// original explicit edit ports here; the model-aware owner subsequently
	// resolves defaults and validates each real field and action.
	if len(fields) == 0 && lookup == nil && s.Widget == "table" && s.InlineEdit != nil {
		fields = slices.Clone(s.InlineEdit.Fields)
	}
	values["fields"] = fields
	if s.Widget == "record-picker" {
		if parent, key := modelSlot(values, "recordPicker.labelField"); parent == nil || parent[key] == nil || parent[key] == "" {
			label := "id"
			if lookup != nil {
				if info, ok := lookup(s.Model.Type); ok && info.Display != "" {
					label = info.Display
				}
			}
			setModelSlot(values, "recordPicker.labelField", label)
		}
	}
	if s.Widget == "status-tracker" && lookup != nil {
		if info, ok := lookup(s.Model.Type); ok && info.Lifecycle != nil {
			if parent, key := modelSlot(values, "statusTracker.field"); parent == nil || parent[key] == nil || parent[key] == "" {
				setModelSlot(values, "statusTracker.field", info.Lifecycle.Field)
			}
			if parent, _ := modelSlot(values, "statusTracker.stages"); parent != nil {
				if stages, ok := parent["stages"].([]any); !ok || len(stages) == 0 {
					states := []string{}
					for _, s := range info.Lifecycle.States {
						states = append(states, s.Name)
					}
					parent["stages"] = states
				}
			}
		}
	}
	_ = json.Unmarshal(Raw(values), &copy)
	return copy
}

// NormalizeModelBindings replaces authoring fields, while legacy immutable
// publications can still be projected without changing their bytes.
func (p *Page) NormalizeModelBindings(lookup func(string) (EntityInfo, bool)) error {
	if p.Document == nil {
		return nil
	}
	for i, s := range p.Sections {
		if s.Composite != nil {
			s.Object = AssetRef{}
			p.Sections[i] = s
			continue
		}
		modelWidget := false
		for _, widget := range pageWidgets.Widgets {
			if widget.ComponentID == s.Widget {
				modelWidget = slices.Contains(widget.BindingKinds, "object")
				break
			}
		}
		if !modelWidget && s.Model == nil {
			continue
		}
		if s.Model != nil {
			// The original widget editors can submit their port projection. It
			// is folded into the same binding before the accepted image is built.
			ports := s.Object.Name != "" || s.Fields != nil || s.Relation != "" || s.ParentSelection != ""
			values := map[string]any{}
			_ = json.Unmarshal(Raw(s), &values)
			for _, slot := range modelScalarSlots {
				if parent, key := modelSlot(values, slot); parent != nil && parent[key] != nil && parent[key] != "" && parent[key] != "count" {
					ports = true
				}
			}
			if !ports {
				continue
			}
		}
		ref := s.ModelObject(*p)
		if s.Object.Name != "" {
			ref = s.Object
		}
		if name := p.SectionInterface(s); name != "" {
			ref.Name = name
		}
		if ref.Name == "" {
			continue
		}
		model := &ModelBinding{Type: ref.Name}
		if s.Model != nil {
			clone := *s.Model
			clone.Fields = slices.Clone(s.Model.Fields)
			model = &clone
			if s.Object.Name != "" {
				model.Type = s.Object.Name
			}
			if s.Fields != nil {
				model.Fields = slices.DeleteFunc(model.Fields, func(f FieldRef) bool { return f.Slot == "" || f.Slot == "fields" })
			}
		}
		for _, field := range s.Fields {
			ref := FieldRef{Name: field}
			if strings.Contains(field, ".") {
				ref.Name, ref.Path = "", strings.Split(field, ".")
			}
			model.Fields = append(model.Fields, ref)
		}
		values := map[string]any{}
		_ = json.Unmarshal(Raw(s), &values)
		for _, slot := range modelScalarSlots {
			parent, key := modelSlot(values, slot)
			if parent == nil {
				continue
			}
			name, ok := parent[key].(string)
			if !ok || name == "" || name == "count" {
				continue
			}
			field := FieldRef{Name: name, Slot: slot}
			if before, after, split := strings.Cut(name, ":"); split {
				if slot == "measure" {
					field.Name, field.Transform = after, before
				} else {
					field.Name, field.Transform = before, after
				}
			}
			if strings.Contains(field.Name, ".") {
				field.Path, field.Name = strings.Split(field.Name, "."), ""
			}
			if field.Name == "id" || len(field.Path) > 0 || lookup == nil || p.SectionInterface(s) != "" {
				model.Fields = slices.DeleteFunc(model.Fields, func(f FieldRef) bool { return f.Slot == slot })
				model.Fields = append(model.Fields, field)
				delete(parent, key)
				continue
			}
			info, known := lookup(ref.Name)
			if _, found := info.Field(field.Name); known && found {
				model.Fields = slices.DeleteFunc(model.Fields, func(f FieldRef) bool { return f.Slot == slot })
				model.Fields = append(model.Fields, field)
				delete(parent, key)
			}
		}
		if s.Relation != "" || s.ParentSelection != "" || s.Widget == "table" && s.Object.Name != "" && s.Object != p.Object {
			parentType := p.Object.Name
			for _, selection := range p.Selections {
				if selection.Name == s.ParentSelection {
					parentType = selection.Object.Name
				}
			}
			if lookup != nil {
				info, _ := lookup(ref.Name)
				for _, f := range info.Fields {
					if f.Type == "reference" && f.Ref == parentType && (s.Relation == "" || f.Inverse == s.Relation) {
						model.Relation = &ModelRelation{ParentType: parentType, ParentSelection: s.ParentSelection, Field: f.Name, Name: f.Inverse}
						break
					}
				}
			}
		}
		if s.Query.Name != "" {
			model.Set = &ModelRecordSet{Query: &AssetBinding{Ref: s.Query}}
		}
		delete(values, "fields")
		delete(values, "object")
		delete(values, "relation")
		delete(values, "parentSelection")
		delete(values, "query")
		values["model"] = model
		var next Section
		if err := json.Unmarshal(Raw(values), &next); err != nil {
			return err
		}
		p.Sections[i] = next
	}
	p.Document.UIProfile = PageModelProfile
	if slices.ContainsFunc(p.Sections, func(s Section) bool { return s.Composite != nil }) {
		p.Document.UIProfile = PageCompositeProfile
	}
	return nil
}

func (p Page) CheckModelBindings(lookup func(string) (EntityInfo, bool), criteria func(AssetRef) (*RuleCriteria, bool)) error {
	if err := p.CheckCompositeBindings(lookup, criteria); err != nil {
		return err
	}
	return p.checkModelSources(p.Sections, lookup, criteria)
}

func (p Page) checkModelSources(sections []Section, lookup func(string) (EntityInfo, bool), criteria func(AssetRef) (*RuleCriteria, bool)) error {
	for _, section := range sections {
		m := section.Model
		if m == nil {
			continue
		}
		where := fmt.Sprintf("page %s section %s model", p.Name, section.ID)
		info, ok := lookup(m.Type)
		if p.Document == nil || !PageUIProfileSupports(p.Document.UIProfile, PageModelProfile) || len(m.Fields) > PageModelMaxFields || len(m.Where) > 32 {
			return fmt.Errorf("%s: invalid profile or budget", where)
		}
		if section.Object.Name != "" || len(section.Fields) > 0 || section.ParentSelection != "" || section.Relation != "" || section.Query.Name != "" {
			return fmt.Errorf("%s: use one model binding", where)
		}
		if name := p.SectionInterface(section); name != "" {
			if m.Type != name || m.Set != nil || m.Options != nil || len(m.Where) > 0 || m.Label != nil || m.Relation != nil {
				return fmt.Errorf("%s: interface binding must retain its original typed query", where)
			}
			for _, f := range m.Fields {
				if f.Name != "" && len(f.Path) > 0 || len(f.Path) > 1 || f.Transform != "" || f.Key() == "" || !slices.Contains([]string{"", "fields", "recordPicker.labelField", "recordCard.labelField"}, f.Slot) {
					return fmt.Errorf("%s: interface fields must come from its retained common shape", where)
				}
			}
			// The retained query's common shape is validated by CheckInterfaceSection.
			continue
		}
		if p.Document == nil || !PageUIProfileSupports(p.Document.UIProfile, PageModelProfile) || !ok || len(m.Fields) > PageModelMaxFields || len(m.Where) > 32 || m.Visibility != "" && m.Visibility != "hide" && m.Visibility != "mark" {
			return fmt.Errorf("%s: invalid object, profile or budget", where)
		}
		if section.Object.Name != "" || len(section.Fields) > 0 || section.ParentSelection != "" || section.Relation != "" || section.Query.Name != "" {
			return fmt.Errorf("%s: use one model binding", where)
		}
		if (m.Set != nil || len(m.Where) > 0 || m.Options != nil) && !ModelWindowWidget(section.Widget) {
			return fmt.Errorf("%s: collection rules need a record window", where)
		}
		seen := map[string]bool{}
		slots := map[string]bool{}
		for _, f := range m.Fields {
			key := f.Slot + "/" + f.Key()
			if seen[key] || f.Name != "" && len(f.Path) > 0 || !ModelFieldSlot(f.Slot) || len(f.Path) > 16 || f.Slot != "" && f.Slot != "fields" && slots[f.Slot] || f.Transform != "" && !slices.Contains([]string{"month", "sum", "avg", "min", "max"}, f.Transform) {
				return fmt.Errorf("%s field %s: invalid model reference", where, key)
			}
			seen[key] = true
			slots[f.Slot] = true
			if _, err := RecordPathField(m.Type, f.Segments(), func(typ string) (EntityInfo, bool) {
				value, found := lookup(typ)
				return ConstraintEntity(value), found
			}); err != nil {
				return fmt.Errorf("%s field %s: %w", where, key, err)
			}
		}
		if r := m.Relation; r != nil {
			field, found := info.Field(r.Field)
			if !found || field.Type != "reference" || field.Ref != r.ParentType || field.Inverse != r.Name {
				return fmt.Errorf("%s: parent reference is not declared", where)
			}
		}
		checkSet := func(set *ModelRecordSet) error {
			if set == nil {
				return nil
			}
			if set.Name != "" {
				root := p.RecordResourceObject(set.RecordVariable)
				owner, known := lookup(root.Name)
				if !known || set.Query != nil || !slices.ContainsFunc(owner.Sets, func(s RecordSet) bool { return s.Name == set.Name && s.Object == m.Type }) {
					return fmt.Errorf("%s: collection must be its owner's declared set", where)
				}
			} else if set.Query == nil || set.Query.Ref.Kind != AssetQuery || set.Query.Ref.Check() != nil || set.Query.SourceVersion == "" {
				return fmt.Errorf("%s: collection needs a declared set or retained query", where)
			}
			return nil
		}
		if err := checkSet(m.Set); err != nil {
			return err
		}
		if m.Options != nil {
			if section.Widget != "record-picker" {
				return fmt.Errorf("%s: options belong to a record selector", where)
			}
			if err := checkSet(m.Options.Set); err != nil {
				return err
			}
			if rule := m.Options.Rule; rule != nil {
				var guard *RuleCriteria
				known := false
				if criteria != nil {
					guard, known = criteria(rule.Action)
				}
				if rule.Action.Kind != AssetAction || !known || guard == nil || guard.Reads.Object != m.Type || len(rule.Inputs) > 64 {
					return fmt.Errorf("%s: original action criteria are unavailable", where)
				}
				for name, typ := range guard.Reads.Inputs {
					id := rule.Inputs[name]
					variable, exists := p.Document.Variables[id]
					if !exists || typ.Kind == "reference" && p.RecordVariableObject(id) != typ.Object || typ.Kind != "reference" && !PageRuleInputFits(variable.Type, typ.Kind) {
						return fmt.Errorf("%s input %s: incompatible page variable", where, name)
					}
				}
				// An original rule can stop using an action input without forcing
				// every referring page to be republished. Unused mappings grant
				// nothing; every supplied variable must still belong to this page.
				for name, id := range rule.Inputs {
					if _, known := p.Document.Variables[id]; !known {
						return fmt.Errorf("%s input %s: unavailable page variable", where, name)
					}
				}
			}
		}
		for i, expr := range m.Where {
			if expr.Kind != "boolean" {
				return fmt.Errorf("%s predicate %d: boolean required", where, i)
			}
			if err := CheckRuleExpression(expr, RuleReadSpec{Object: m.Type}, lookup); err != nil {
				return fmt.Errorf("%s predicate %d: %w", where, i, err)
			}
		}
		if m.Label != nil {
			if m.Label.Kind != "text" {
				return fmt.Errorf("%s label: text required", where)
			}
			if err := CheckRuleExpression(*m.Label, RuleReadSpec{Object: m.Type}, lookup); err != nil {
				return fmt.Errorf("%s label: %w", where, err)
			}
		}
	}
	return nil
}

func CheckRuleExpression(expr Expr, reads RuleReadSpec, lookup func(string) (EntityInfo, bool)) error {
	schema, err := RuleSchema(expr, reads, lookup)
	if err != nil {
		return err
	}
	_, err = CompileExpr(expr, schema)
	return err
}

func PageRuleInputFits(variable, kind string) bool {
	return variable == kind || variable == "string" && (kind == "text" || kind == "choice" || kind == "date" || kind == "datetime") || variable == "number" && (kind == "number" || kind == "integer")
}

func (p Page) ModelDependencies() []AssetRef {
	refs := []AssetRef{}
	for _, s := range p.Sections {
		for _, m := range s.ModelBindings() {
			if p.SectionInterface(s) == "" {
				refs = append(refs, ModelObject(m.Type))
			}
			if m.Relation != nil {
				refs = append(refs, ModelObject(m.Relation.ParentType))
			}
			sets := []*ModelRecordSet{m.Set}
			if m.Options != nil {
				sets = append(sets, m.Options.Set)
				if m.Options.Rule != nil {
					refs = append(refs, m.Options.Rule.Action)
				}
			}
			for _, set := range sets {
				if set != nil && set.Query != nil {
					refs = append(refs, set.Query.Ref)
				}
				if set != nil && set.Name != "" {
					refs = append(refs, p.RecordResourceObject(set.RecordVariable))
				}
			}
		}
	}
	return refs
}

// These widgets already consume finite record windows. Aggregate contracts
// continue to use their original query/aggregate ports.
func ModelWindowWidget(widget string) bool {
	return slices.Contains([]string{"table", "detail", "record-picker", "record-card", "record-list", "record-chart", "record-events", "record-calendar", "record-gantt", "record-leaderboard", "record-scatter", "record-timeline", "kanban", "record-map", "histogram", "term-counts", "tag-counts", "sparkline-kpi"}, widget)
}

// ReplaceModelPorts preserves owner-specific action transport when Build
// consumes the canonical platform binding. It copies no business records.
func ReplaceModelPorts(values, canonical map[string]any) {
	for _, key := range []string{"composite", "model", "object", "fields", "relation", "parentSelection", "query"} {
		if key == "object" || key == "query" {
			delete(values, key)
			continue
		}
		if value, ok := canonical[key]; ok {
			values[key] = value
		} else {
			delete(values, key)
		}
	}
	for _, slot := range modelScalarSlots {
		parent, key := modelSlot(values, slot)
		if parent == nil {
			continue
		}
		source, sourceKey := modelSlot(canonical, slot)
		if source != nil {
			if value, ok := source[sourceKey]; ok {
				parent[key] = value
				continue
			}
		}
		delete(parent, key)
	}
}

// CanonicalModelSection removes the temporary widget ports after a member
// projection without modifying the original model or publication bytes.
func CanonicalModelSection(section Section) Section {
	if section.Composite != nil {
		section.Object = AssetRef{}
		return section
	}
	if section.Model == nil {
		return section
	}
	values := map[string]any{}
	_ = json.Unmarshal(Raw(section), &values)
	ReplaceModelPorts(values, map[string]any{"model": section.Model})
	var next Section
	_ = json.Unmarshal(Raw(values), &next)
	return next
}

func (p Page) EffectiveModelPage(lookup func(string) (EntityInfo, bool)) Page {
	p.Sections = slices.Clone(p.Sections)
	for i, section := range p.Sections {
		p.Sections[i] = p.EffectiveModelSection(section, lookup)
	}
	return p
}

func ModelFields(info EntityInfo, model *ModelBinding, lookup func(string) (EntityInfo, bool)) EntityInfo {
	if model == nil {
		return info
	}
	info.Fields = slices.Clone(info.Fields)
	for _, ref := range model.Fields {
		if len(ref.Segments()) < 2 {
			continue
		}
		field, err := RecordPathField(model.Type, ref.Segments(), lookup)
		if err != nil {
			continue
		}
		field.Name, field.Title, field.ReadOnly = ref.Key(), strings.Join(ref.Segments(), " → "), true
		info.Fields = append(info.Fields, field)
	}
	return info
}
