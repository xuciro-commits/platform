package platform

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// The application API owns this build-time UI contract (ADR-0015). The same
// bytes generate the browser registry; they describe presentation, not grants
// or new business execution capabilities.
//
//go:embed pageui/widgets.json
var pageWidgetJSON []byte

type pageModelRole struct {
	Kinds    []string `json:"kinds"`
	Required bool     `json:"required,omitempty"`
	Target   string   `json:"target,omitempty"`
}
type pageModelPort struct {
	ID       string                   `json:"id"`
	Required bool                     `json:"required,omitempty"`
	Roles    map[string]pageModelRole `json:"roles"`
}

type pageWidgetContract struct {
	ModelPorts          []pageModelPort   `json:"modelPorts,omitempty"`
	ConfigurationFields []string          `json:"configurationFields,omitempty"`
	ComponentID         string            `json:"componentID"`
	ConfigVersion       int               `json:"configVersion"`
	RequiredUIProfile   string            `json:"requiredUIProfile"`
	SelectionMode       string            `json:"selectionMode"`
	BindingKinds        []string          `json:"bindingKinds"`
	PropsSchema         ValueSchema       `json:"propsSchema"`
	Defaults            map[string]any    `json:"defaults"`
	InputPorts          []pageWidgetPort  `json:"inputPorts"`
	OutputPorts         []pageWidgetPort  `json:"outputPorts"`
	Events              []pageWidgetEvent `json:"events"`
	Slots               []pageWidgetSlot  `json:"slots"`
	LayoutPreferences   struct {
		Frame string `json:"frame"`
	} `json:"layoutPreferences"`
	LifecyclePolicy *struct {
		StateOwner string   `json:"stateOwner"`
		ClearOn    []string `json:"clearOn"`
		Hidden     string   `json:"hidden"`
	} `json:"lifecyclePolicy"`
}

type pageWidgetSlot struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Placement         string   `json:"placement"`
	RequiredUIProfile string   `json:"requiredUIProfile"`
	AllowedLayouts    []string `json:"allowedLayouts"`
}

func widgetSlot(widget, id string) *pageWidgetSlot {
	for _, contract := range pageWidgets.Widgets {
		if contract.ComponentID == widget {
			for _, slot := range contract.Slots {
				if slot.ID == id {
					return &slot
				}
			}
		}
	}
	return nil
}

type pageWidgetPort struct {
	ID                string `json:"id"`
	BindingField      string `json:"bindingField"`
	Type              string `json:"type"`
	RequiredUIProfile string `json:"requiredUIProfile"`
	Writable          bool   `json:"writable"`
	StateWritable     bool   `json:"stateWritable,omitempty"`
}
type pageWidgetEvent struct {
	RequiredUIProfile string `json:"requiredUIProfile,omitempty"`
	ID                string `json:"id"`
	Payload           string `json:"payload"`
	Required          bool   `json:"required"`
	MaxBindings       int    `json:"maxBindings"`
}

type pageUIContract struct {
	// Documentless freezes the retained flat Section reader. New components
	// and properties use documents; this list does not grow with authoring.
	Documentless struct {
		Widgets []string `json:"widgets"`
		Fields  []string `json:"fields"`
	} `json:"documentless"`
	Features map[string]string `json:"features"`
	Layout   struct {
		PresentationProfile string `json:"presentationProfile"`
		MaxPadding          int    `json:"maxPadding"`
		RequiredUIProfile   string `json:"requiredUIProfile"`
		UnusedProfile       string `json:"unusedProfile"`
		MaxUnused           int    `json:"maxUnused"`
		MinSize             int    `json:"minSize"`
		MaxSize             int    `json:"maxSize"`
		MaxWeight           int    `json:"maxWeight"`
		MaxGap              int    `json:"maxGap"`
		StackBelow          int    `json:"stackBelow"`
	} `json:"layout"`
	UIProfile         string               `json:"uiProfile"`
	SupportedProfiles []string             `json:"supportedProfiles"`
	Widgets           []pageWidgetContract `json:"widgets"`
	Runtime           pageRuntimeContract  `json:"runtime"`
}

var pageWidgets = func() pageUIContract {
	var manifest pageUIContract
	if err := json.Unmarshal(pageWidgetJSON, &manifest); err != nil {
		panic(err)
	}
	seen := map[string]bool{}
	for _, widget := range manifest.Widgets {
		if widget.ComponentID == "" || seen[widget.ComponentID] || widget.ConfigVersion < 1 || !slices.Contains(manifest.SupportedProfiles, widget.RequiredUIProfile) {
			panic("invalid page widget contract identity")
		}
		seen[widget.ComponentID] = true
		for _, name := range widget.ConfigurationFields {
			if _, ok := sectionJSONField(reflect.TypeFor[Section](), []string{name}); !ok {
				panic("unknown page widget configuration field " + name)
			}
		}
		slots := map[string]bool{}
		for _, slot := range widget.Slots {
			if slot.ID == "" || slots[slot.ID] || slot.Title == "" || !slices.Contains([]string{"before", "after"}, slot.Placement) || !slices.Contains(manifest.SupportedProfiles, slot.RequiredUIProfile) || len(slot.AllowedLayouts) == 0 || len(widget.Slots) > 4 {
				panic("invalid page widget slot")
			}
			for _, kind := range slot.AllowedLayouts {
				if !slices.Contains([]string{"rows", "columns", "flow", "toolbar"}, kind) {
					panic("unsupported widget slot layout")
				}
			}
			slots[slot.ID] = true
		}
		fields := map[string]bool{}
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			field, found := sectionJSONField(reflect.TypeFor[Section](), strings.Split(port.BindingField, "."))
			if port.ID == "" || fields[port.BindingField] || (port.BindingField != "enabledWhen" && (!found || field.Kind() != reflect.String)) || !slices.Contains([]string{"record", "record-set", "object-set", "filter", "boolean", "decimal", "number", "statistics", "string", "string-set"}, port.Type) || !slices.Contains(manifest.SupportedProfiles, port.RequiredUIProfile) || port.StateWritable && !port.Writable {
				panic("invalid page widget port")
			}
			fields[port.BindingField] = true
		}
		for _, event := range widget.Events {
			if !slices.Contains([]string{"click", "select"}, event.ID) || event.Payload != "void" || event.MaxBindings != 1 && !(widget.ComponentID == "button-group" && event.MaxBindings == manifest.Runtime.ButtonGroup.MaxButtons) && !(widget.ComponentID == "asset-directory" && event.MaxBindings == manifest.Runtime.Exploration.MaxDirectoryItems) || event.RequiredUIProfile != "" && !slices.Contains(manifest.SupportedProfiles, event.RequiredUIProfile) {
				panic("unsupported page widget event")
			}
		}
		if widget.LayoutPreferences.Frame != "card" && widget.LayoutPreferences.Frame != "inline" {
			panic("invalid widget frame")
		}
		if policy := widget.LifecyclePolicy; policy != nil {
			session := policy.StateOwner == "page-session" && policy.Hidden == "retain" && slices.Equal(policy.ClearOn, []string{"scope-change", "binding-change", "close"})
			instance := policy.StateOwner == "widget-instance" && policy.Hidden == "unmount" && slices.Equal(policy.ClearOn, []string{"record", "member", "definition", "scope-close"})
			if !session && !instance {
				panic("unsupported widget lifecycle policy")
			}
		}
		if err := widget.PropsSchema.Check(); err != nil {
			panic(err)
		}
		defaults, _ := json.Marshal(widget.Defaults)
		if err := widget.PropsSchema.Validate(defaults, 1<<20); err != nil {
			panic(err)
		}
	}
	for _, profile := range manifest.Features {
		if !slices.Contains(manifest.SupportedProfiles, profile) {
			panic("invalid page feature profile")
		}
	}
	return manifest
}()

// PageUIManifest exports the original declarations and their derived author
// fields. Properties and ports need no second list when their shape grows.
func PageUIManifest() string {
	var manifest map[string]json.RawMessage
	_ = json.Unmarshal(pageWidgetJSON, &manifest)
	var widgets []map[string]json.RawMessage
	_ = json.Unmarshal(manifest["widgets"], &widgets)
	for i, widget := range pageWidgets.Widgets {
		widgets[i]["authoringFields"], _ = json.Marshal(widget.authoringFields())
	}
	manifest["widgets"], _ = json.Marshal(widgets)
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	return string(raw)
}

func (w pageWidgetContract) authoringFields() []string {
	fields := append([]string{"id", "widget", "configVersion"}, w.ConfigurationFields...)
	for field := range w.PropsSchema.Properties {
		fields = append(fields, field)
	}
	for _, port := range append(slices.Clone(w.InputPorts), w.OutputPorts...) {
		if port.BindingField != "enabledWhen" {
			fields = append(fields, strings.Split(port.BindingField, ".")[0])
		}
	}
	slices.Sort(fields)
	return slices.Compact(fields)
}

// configVersion belongs to a component's configuration. requiredUIProfile and
// feature profiles describe runtime semantics. An additive ordinary property
// changes its component declaration, not the global UI profile or this reader.
func checkDocumentlessSection(section Section) error {
	if !slices.Contains(pageWidgets.Documentless.Widgets, section.Widget) {
		return fmt.Errorf("widget %s requires a page document", section.Widget)
	}
	raw, err := json.Marshal(section)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for field := range fields {
		if !slices.Contains(pageWidgets.Documentless.Fields, field) {
			return fmt.Errorf("widget property %s requires a page document", field)
		}
	}
	return nil
}

func pageFeatureSupported(profile, feature string) bool {
	return PageUIProfileSupports(profile, pageWidgets.Features[feature])
}

func PageUIProfile() string { return pageWidgets.UIProfile }
func WidgetWritesSelection(id string) bool {
	w := pageWidget(id)
	return w != nil && w.SelectionMode == "write"
}
func WidgetReadsSelection(id string) bool {
	w := pageWidget(id)
	return w != nil && (w.SelectionMode == "read" || w.SelectionMode == "write")
}

func SupportsPageUIProfile(profile string) bool {
	return slices.Contains(pageWidgets.SupportedProfiles, profile)
}

func pageWidget(id string) *pageWidgetContract {
	for i := range pageWidgets.Widgets {
		if pageWidgets.Widgets[i].ComponentID == id {
			return &pageWidgets.Widgets[i]
		}
	}
	return nil
}
func widgetPort(id, field string) *pageWidgetPort {
	w := pageWidget(id)
	if w == nil {
		return nil
	}
	for _, ports := range [][]pageWidgetPort{w.InputPorts, w.OutputPorts} {
		for i := range ports {
			if ports[i].BindingField == field {
				return &ports[i]
			}
		}
	}
	return nil
}
func widgetEvent(id, event string) *pageWidgetEvent {
	w := pageWidget(id)
	if w == nil {
		return nil
	}
	for i := range w.Events {
		if w.Events[i].ID == event {
			return &w.Events[i]
		}
	}
	return nil
}
func (s Section) variablePorts() map[string]string {
	ports := map[string]string{}
	for _, widget := range pageWidgets.Widgets {
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			if port.BindingField == "enabledWhen" {
				continue // This port belongs to the layout node, not its Section.
			}
			if _, seen := ports[port.BindingField]; !seen {
				ports[port.BindingField] = sectionJSONText(reflect.ValueOf(s), strings.Split(port.BindingField, "."))
			}
		}
	}
	return ports
}

// The retained Section wire format is flat. All manifest paths meet it here,
// rather than requiring another field list whenever a widget gains a port.
func sectionJSONField(typ reflect.Type, path []string) (reflect.Type, bool) {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if len(path) == 0 {
		return typ, true
	}
	if typ.Kind() != reflect.Struct {
		return nil, false
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			if found, ok := sectionJSONField(field.Type, path); ok {
				return found, true
			}
		}
		if strings.Split(field.Tag.Get("json"), ",")[0] == path[0] {
			return sectionJSONField(field.Type, path[1:])
		}
	}
	return nil, false
}

func sectionJSONText(value reflect.Value, path []string) string {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if len(path) == 0 {
		return value.String()
	}
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if field.Anonymous {
			if _, ok := sectionJSONField(field.Type, path); ok {
				return sectionJSONText(value.Field(i), path)
			}
		}
		if strings.Split(field.Tag.Get("json"), ",")[0] == path[0] {
			return sectionJSONText(value.Field(i), path[1:])
		}
	}
	return ""
}

// The same Section references drive member visibility and port validation.
// Nested inputs remain owned by their original widget declarations.
func (s Section) variableReferences() []string {
	ports := s.variablePorts()
	ids := make([]string, 0, len(ports))
	for _, id := range ports {
		ids = append(ids, id)
	}
	ids = append(ids, s.EmbeddingVariables()...)
	if s.AI != nil {
		ids = append(ids, s.AI.QuestionVariable)
	}
	if s.Widget == "observation" && s.Observation != nil {
		ids = append(ids, s.Observation.RowOutput, s.Observation.AssetOutput)
	}
	for _, facet := range s.Facets {
		ids = append(ids, facet.Variable)
	}
	return append(ids, s.FilterSearchVariable)
}

func (d *PageDocument) checkWidgetPorts(s Section) error {
	if s.ChartVariant != "" && (s.Widget != "chart" || s.Mark != "arc" || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.ChartPresentation.RequiredUIProfile) || !slices.Contains(pageWidgets.Runtime.ChartPresentation.Variants, s.ChartVariant)) {
		return fmt.Errorf("chart variant needs its pie profile and mark")
	}
	if s.Mark != "" && !pageFeatureSupported(d.UIProfile, "chartMark") {
		return fmt.Errorf("chart mark requires UI profile v2.24")
	}
	if w := pageWidget(s.Widget); w != nil && !PageUIProfileSupports(d.UIProfile, w.RequiredUIProfile) {
		return fmt.Errorf("widget %s requires UI profile %s", s.Widget, w.RequiredUIProfile)
	}
	ports := s.variablePorts()
	for field, id := range ports {
		if id == "" {
			continue
		}
		if field == "selectionSetVariable" {
			v := d.Variables[id]
			if v.Mode == "shared" {
				if err := d.checkSharedRecordSetOutput(s); err != nil {
					return err
				}
			} else if v.Mode != "resource" || v.Source == nil || v.Source.Kind != "records" || v.Source.Section != s.ID {
				return fmt.Errorf("table selection set needs its own record-set resource")
			}
		}
		port := widgetPort(s.Widget, field)
		v, ok := d.Variables[id]
		readonlyIndexChoice := field == "choiceVariable" && s.ChoiceInput != nil && (s.ChoiceInput.Variant == "steps" || s.ChoiceInput.Variant == "tabs") && v.Mode == "constant"
		if port == nil || !ok || !PageUIProfileSupports(d.UIProfile, port.RequiredUIProfile) || v.Type != port.Type || port.Writable && field != "selectionSetVariable" && !readonlyIndexChoice && !(v.Writable || port.StateWritable && v.Mode == "state") {
			return fmt.Errorf("widget %s has an invalid %s port", s.Widget, field)
		}
	}
	return nil
}

func PageUIProfileSupports(profile, required string) bool {
	current, minimum := slices.Index(pageWidgets.SupportedProfiles, profile), slices.Index(pageWidgets.SupportedProfiles, required)
	return current >= 0 && minimum >= 0 && current >= minimum
}

// Widgets remains the original public discovery list, derived from the one
// descriptor rather than a separately maintained enumeration.
var Widgets = func() []string {
	ids := make([]string, 0, len(pageWidgets.Widgets))
	for _, widget := range pageWidgets.Widgets {
		ids = append(ids, widget.ComponentID)
	}
	return ids
}()

func checkPageWidget(section Section) error {
	for _, widget := range pageWidgets.Widgets {
		if widget.ComponentID != section.Widget {
			continue
		}
		if section.ConfigVersion != widget.ConfigVersion {
			return fmt.Errorf("widget %s config version %d is unsupported", section.Widget, section.ConfigVersion)
		}
		// Only presentation properties are validated here. Asset references,
		// selections, queries and actions retain their original host owners.
		rawSection, _ := json.Marshal(section)
		var values map[string]json.RawMessage
		_ = json.Unmarshal(rawSection, &values)
		props := map[string]json.RawMessage{}
		for _, contract := range pageWidgets.Widgets {
			for key := range contract.PropsSchema.Properties {
				if value, present := values[key]; present {
					if _, declared := widget.PropsSchema.Properties[key]; !declared {
						return fmt.Errorf("widget %s does not declare property %s", section.Widget, key)
					}
					props[key] = value
				}
			}
		}
		raw, _ := json.Marshal(props)
		return widget.PropsSchema.Validate(raw, 1<<20)
	}
	return fmt.Errorf("widget %q is unavailable", section.Widget)
}

// CheckSectionFields applies the same original-field rules to installed pages
// and frozen release descriptors. Binding and action owners check their own edges.
func (p Page) CheckSectionFields(s Section, info EntityInfo) error {
	for _, check := range []func(EntityInfo) error{
		s.CheckTimeline,
		func(info EntityInfo) error { return p.CheckKanban(s, info) },
		s.CheckStatusTracker,
		s.CheckMetricPresentation,
		s.CheckRecordList,
		s.CheckHeatmap,
		s.CheckScatter,
		s.CheckMap,
		s.CheckRecordChart,
		s.CheckRecordEvents,
		s.CheckRecordPicker,
		s.CheckLeaderboard,
		s.CheckHistogram,
		s.CheckRecordCard,
		s.CheckRecordComparison,
		s.CheckSparkline,
		s.CheckTerms,
		s.CheckCollectionAnalysis,
		s.CheckSummary,
		s.CheckRecordGantt,
		s.CheckRecordCalendar,
	} {
		if err := check(info); err != nil {
			return err
		}
	}
	return nil
}
