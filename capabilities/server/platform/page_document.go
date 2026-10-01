package platform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

// PageDocument is the presentation tree of a composed page. Sections remain
// the page's authoritative, server-checked business bindings; leaves name
// those sections by stable ID. The UI profile versions layout and finite
// presentation-state semantics; business bindings keep their original owners.
type PageDocument struct {
	FormatVersion int                       `json:"formatVersion"`
	UIProfile     string                    `json:"uiProfile"`
	Root          string                    `json:"root"`
	Nodes         map[string]PageLayoutNode `json:"nodes"`
	Variables     map[string]PageVariable   `json:"variables,omitempty"`
	Overlays      map[string]PageOverlay    `json:"overlays,omitempty"`
	Events        []PageEventBinding        `json:"events,omitempty"`
	Interface     *PageInterface            `json:"interface,omitempty"`
}

type PageLayoutNode struct {
	Kind           string    `json:"kind"` // rows, columns, tabs, flow, toolbar, or widget
	Children       []string  `json:"children,omitempty"`
	Section        string    `json:"section,omitempty"`
	Title          string    `json:"title,omitempty"`
	ActiveVariable string    `json:"activeVariable,omitempty"`
	VisibleWhen    string    `json:"visibleWhen,omitempty"`
	EnabledWhen    string    `json:"enabledWhen,omitempty"`
	Align          string    `json:"align,omitempty"`
	Loop           *PageLoop `json:"loop,omitempty"`
}

var pageNodeID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,79}$`)

// Check rejects cycles, shared children, unreachable nodes and references to
// missing or duplicated sections. Each published section must appear once.
func (d *PageDocument) Check(sections []Section) error {
	if d == nil {
		return nil
	}
	if d.FormatVersion != 2 {
		return fmt.Errorf("page document format %d is unsupported", d.FormatVersion)
	}
	if !SupportsPageUIProfile(d.UIProfile) {
		return fmt.Errorf("page UI profile %q is unsupported", d.UIProfile)
	}
	if d.UIProfile == "platform.page.v2.1" && len(d.Variables) > 0 {
		return fmt.Errorf("page variables require UI profile v2.2")
	}
	for id, variable := range d.Variables {
		if variable.Scope == pageWidgets.Runtime.Loop.Scope && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.5") {
			return fmt.Errorf("page variable %s requires UI profile v2.5", id)
		}
		if variable.Mode == "resource" && (d.UIProfile == "platform.page.v2.1" || d.UIProfile == "platform.page.v2.2") {
			return fmt.Errorf("page variable %s requires UI profile v2.3", id)
		}
	}
	if err := d.CheckVariables(); err != nil {
		return err
	}
	if err := d.checkInterface(); err != nil {
		return err
	}
	if err := d.checkEvents(sections); err != nil {
		return err
	}
	if !pageNodeID.MatchString(d.Root) || len(d.Nodes) == 0 || len(d.Nodes) > 256 {
		return fmt.Errorf("page document needs a valid root and 1 to 256 nodes")
	}
	if d.Nodes[d.Root].Kind == "widget" {
		return fmt.Errorf("page document root must be a layout container")
	}
	if len(sections) == 0 || len(sections) > 128 {
		return fmt.Errorf("page document needs 1 to 128 sections")
	}
	byID := make(map[string]bool, len(sections))
	for _, section := range sections {
		if !pageNodeID.MatchString(section.ID) || byID[section.ID] {
			return fmt.Errorf("page document sections need unique stable IDs")
		}
		byID[section.ID] = true
		if err := checkPageWidget(section); err != nil {
			return fmt.Errorf("page section %s: %w", section.ID, err)
		}
	}
	for id, variable := range d.Variables {
		if variable.Source == nil || variable.Source.Kind == pageWidgets.Runtime.Loop.Source {
			continue
		}
		found := false
		for _, section := range sections {
			for _, resource := range pageWidgets.Runtime.Resources {
				if section.ID == variable.Source.Section && section.Widget == resource.Widget && variable.Source.Kind == resource.Kind {
					found = true
				}
			}
		}
		if !found {
			return fmt.Errorf("page variable %s needs a compatible source section", id)
		}
	}
	for id := range d.Nodes {
		if !pageNodeID.MatchString(id) {
			return fmt.Errorf("page document has invalid node ID %q", id)
		}
	}
	seen := map[string]bool{}
	used := map[string]bool{}
	var walk func(string, int) error
	walk = func(id string, depth int) error {
		if depth > 24 {
			return fmt.Errorf("page document layout is too deep at %q", id)
		}
		node, ok := d.Nodes[id]
		if !ok {
			return fmt.Errorf("page document references missing node %q", id)
		}
		if seen[id] {
			return fmt.Errorf("page document node %q is shared or cyclic", id)
		}
		seen[id] = true
		if len(node.Title) > 1024 {
			return fmt.Errorf("page node %s title is too long", id)
		}
		if d.UIProfile == "platform.page.v2.1" && (node.Kind == "tabs" || node.ActiveVariable != "" || node.VisibleWhen != "" || node.Title != "") {
			return fmt.Errorf("page node %s requires UI profile v2.2", id)
		}
		if node.VisibleWhen != "" && d.Variables[node.VisibleWhen].Type != "boolean" {
			return fmt.Errorf("page node %s visibility needs a boolean variable", id)
		}
		if node.Kind != "tabs" && node.ActiveVariable != "" {
			return fmt.Errorf("page node %s is not tabs", id)
		}
		if (node.Kind == "flow" || node.Kind == "toolbar" || node.Align != "" || node.EnabledWhen != "") && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.4") {
			return fmt.Errorf("page node %s requires UI profile v2.4", id)
		}
		if node.Align != "" && (node.Kind != "flow" && node.Kind != "toolbar" || !slices.Contains([]string{"start", "center", "end", "between"}, node.Align)) {
			return fmt.Errorf("page node %s has unsupported alignment", id)
		}
		if node.EnabledWhen != "" && (node.Kind != "widget" || d.Variables[node.EnabledWhen].Type != "boolean" || !slices.ContainsFunc(sections, func(s Section) bool { return s.ID == node.Section && s.Widget == "button" })) {
			return fmt.Errorf("page node %s enable binding needs a button and boolean variable", id)
		}
		switch node.Kind {
		case "widget":
			if len(node.Children) != 0 || !byID[node.Section] || used[node.Section] {
				return fmt.Errorf("page document widget %q needs one unique section", id)
			}
			used[node.Section] = true
		case "rows", "columns", "tabs", "flow", "toolbar", "loop":
			if node.Section != "" || len(node.Children) == 0 || len(node.Children) > 128 {
				return fmt.Errorf("page document container %q needs children and no section", id)
			}
			if node.Kind == "tabs" {
				variable, ok := d.Variables[node.ActiveVariable]
				var initial string
				if !ok || variable.Type != "string" || variable.Mode != "state" || json.Unmarshal(variable.Initial, &initial) != nil || !slices.Contains(node.Children, initial) {
					return fmt.Errorf("page tabs %s need a text state initialized to a child", id)
				}
			}
			for _, child := range node.Children {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("page document node %q has unsupported layout %q", id, node.Kind)
		}
		return nil
	}
	if err := walk(d.Root, 0); err != nil {
		return err
	}
	for id, overlay := range d.Overlays {
		if d.Nodes[overlay.Root].Kind == "widget" {
			return fmt.Errorf("page overlay %s root must be a container", id)
		}
		if err := walk(overlay.Root, 0); err != nil {
			return err
		}
	}
	if len(seen) != len(d.Nodes) || len(used) != len(byID) {
		return fmt.Errorf("page document has unreachable nodes or sections")
	}
	if err := d.checkLoops(sections); err != nil {
		return err
	}
	// Include producer availability in the dependency graph: mutually hidden
	// tables must not deadlock even when the pure expression graph is acyclic.
	controls := map[string][]string{}
	var collect func(string, []string)
	collect = func(id string, inherited []string) {
		node := d.Nodes[id]
		conditions := slices.Clone(inherited)
		if node.VisibleWhen != "" {
			conditions = append(conditions, node.VisibleWhen)
		}
		if node.Loop != nil {
			conditions = append(conditions, node.Loop.Collection)
		}
		if node.Kind == "widget" {
			controls[node.Section] = conditions
		}
		for _, child := range node.Children {
			collect(child, conditions)
		}
	}
	collect(d.Root, nil)
	for _, overlay := range d.Overlays {
		collect(overlay.Root, []string{overlay.OpenVariable})
	}
	active, complete := map[string]bool{}, map[string]bool{}
	var check func(string) error
	check = func(id string) error {
		if active[id] {
			return fmt.Errorf("page variable %s has cyclic resource visibility", id)
		}
		if complete[id] {
			return nil
		}
		active[id] = true
		variable := d.Variables[id]
		dependencies := []string{}
		if variable.Expression != nil {
			for _, arg := range variable.Expression.Args {
				if arg.Variable != "" {
					dependencies = append(dependencies, arg.Variable)
				}
			}
		}
		if variable.Source != nil {
			dependencies = append(dependencies, controls[variable.Source.Section]...)
		}
		for _, dependency := range dependencies {
			if err := check(dependency); err != nil {
				return err
			}
		}
		active[id] = false
		complete[id] = true
		return nil
	}
	for id := range d.Variables {
		if err := check(id); err != nil {
			return err
		}
	}
	return nil
}

// Visible keeps only the member-filtered sections and their ancestor layout.
// It never mutates the installed definition. An empty root represents a page
// whose widgets are all hidden from this reader.
func (d *PageDocument) Visible(sections []Section) *PageDocument {
	if d == nil {
		return nil
	}
	allowed := make(map[string]bool, len(sections))
	for _, section := range sections {
		allowed[section.ID] = true
	}
	variables := map[string]PageVariable{}
	for id, variable := range d.Variables {
		if variable.Source == nil || variable.Source.Kind == pageWidgets.Runtime.Loop.Source || allowed[variable.Source.Section] {
			variables[id] = variable
		}
	}
	for changed := true; changed; {
		changed = false
		for id, variable := range variables {
			if variable.Scope == pageWidgets.Runtime.Loop.Scope {
				loop := d.Nodes[variable.Owner].Loop
				if loop == nil {
					delete(variables, id)
					changed = true
					continue
				}
				if _, ok := variables[loop.Collection]; !ok {
					delete(variables, id)
					changed = true
					continue
				}
			}
			if variable.Expression != nil {
				for _, arg := range variable.Expression.Args {
					if arg.Variable != "" {
						if _, ok := variables[arg.Variable]; !ok {
							delete(variables, id)
							changed = true
							break
						}
					}
				}
			}
		}
	}
	out := &PageDocument{FormatVersion: d.FormatVersion, UIProfile: d.UIProfile, Root: d.Root, Nodes: map[string]PageLayoutNode{}, Variables: variables, Interface: d.Interface}
	var copyVisible func(string) bool
	copyVisible = func(id string) bool {
		node, ok := d.Nodes[id]
		if !ok {
			return false
		}
		if node.Loop != nil {
			if _, available := variables[node.Loop.Collection]; !available {
				return false
			}
		}
		if node.EnabledWhen != "" {
			if _, available := variables[node.EnabledWhen]; !available {
				return false
			}
		}
		if node.VisibleWhen != "" {
			if _, visible := variables[node.VisibleWhen]; !visible {
				return false
			}
		}
		if node.Kind == "widget" {
			if !allowed[node.Section] {
				return false
			}
			out.Nodes[id] = node
			return true
		}
		children := make([]string, 0, len(node.Children))
		for _, child := range node.Children {
			if copyVisible(child) {
				children = append(children, child)
			}
		}
		if len(children) == 0 && id != d.Root {
			return false
		}
		node.Children = children
		out.Nodes[id] = node
		return true
	}
	for {
		out.Nodes = map[string]PageLayoutNode{}
		out.Overlays = map[string]PageOverlay{}
		missing := map[string]bool{}
		for id, overlay := range d.Overlays {
			if copyVisible(overlay.Root) {
				out.Overlays[id] = overlay
			} else {
				missing[overlay.OpenVariable] = true
			}
		}
		out.Events = nil
		bound := map[string]bool{}
		for _, event := range d.Events {
			if allowed[event.Source] && !missing[event.Target] {
				valid := true
				if event.Navigate != nil {
					for _, arg := range event.Navigate.Inputs {
						if arg.Variable != "" {
							if _, ok := variables[arg.Variable]; !ok {
								valid = false
							}
						}
					}
					for _, id := range event.Navigate.Results {
						if _, ok := variables[id]; !ok {
							valid = false
						}
					}
				}
				if _, ok := variables[event.Target]; valid && (ok || event.Navigate != nil || event.Return) {
					out.Events = append(out.Events, event)
					bound[event.Source] = true
				}
			}
		}
		changed := false
		for _, section := range sections {
			if section.Widget == "button" && allowed[section.ID] && !bound[section.ID] {
				delete(allowed, section.ID)
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	copyVisible(d.Root)
	if _, ok := out.Nodes[d.Root]; !ok {
		out.Nodes = map[string]PageLayoutNode{d.Root: {Kind: "rows"}}
	}
	for id, variable := range out.Variables {
		if variable.Scope == pageWidgets.Runtime.Loop.Scope && out.Nodes[variable.Owner].Kind != "loop" {
			delete(out.Variables, id)
		}
	}
	if d.Interface != nil {
		iface := *d.Interface
		iface.Inputs = map[string]PagePort{}
		iface.Outputs = map[string]PagePort{}
		for id, port := range d.Interface.Inputs {
			if _, ok := out.Variables[port.Variable]; ok {
				iface.Inputs[id] = port
			}
		}
		for id, port := range d.Interface.Outputs {
			if _, ok := out.Variables[port.Variable]; ok {
				iface.Outputs[id] = port
			}
		}
		out.Interface = &iface
	}
	return out
}
