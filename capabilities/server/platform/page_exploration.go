package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

type PageResourceList struct {
	LabelField  string          `json:"labelField"`
	StatusField string          `json:"statusField"`
	StatusTones []PageEventTone `json:"statusTones,omitempty"`
}
type PageAssetDirectoryItem struct {
	ID    string       `json:"id"`
	Label string       `json:"label"`
	Asset AssetBinding `json:"asset"`
}
type PageAssetDirectory struct {
	Items []PageAssetDirectoryItem `json:"items"`
}
type PageGraphObject struct {
	Object     AssetRef `json:"object"`
	LabelField string   `json:"labelField"`
}
type PageGraphRelation struct {
	ID      string       `json:"id"`
	Binding AssetBinding `json:"binding"`
}
type PageGraphOutput struct {
	ID       string   `json:"id"`
	Object   AssetRef `json:"object"`
	Variable string   `json:"variable"`
}
type PageGraphExplorer struct {
	Objects   []PageGraphObject   `json:"objects"`
	Relations []PageGraphRelation `json:"relations"`
	Outputs   []PageGraphOutput   `json:"outputs,omitempty"`
}
type PageNeighborhoodGroup struct {
	ID        string       `json:"id"`
	Binding   AssetBinding `json:"binding"`
	Direction string       `json:"direction"`
	Limit     int          `json:"limit"`
	Badge     string       `json:"badge"`
	Tone      string       `json:"tone"`
}
type PageVertexGraph struct {
	Groups []PageNeighborhoodGroup `json:"groups"`
}

func explorationWidget(widget string) bool {
	return slices.Contains([]string{"resource-list", "asset-directory", "graph-explorer", "vertex-graph"}, widget)
}
func (s Section) ExplorationVariables() []string {
	switch s.Widget {
	case "resource-list":
		return []string{s.CollectionVariable}
	case "graph-explorer", "vertex-graph":
		return []string{s.RecordVariable}
	}
	return nil
}
func (s Section) ExplorationBindings() []AssetBinding {
	bindings := []AssetBinding{}
	if s.AssetDirectory != nil {
		for _, i := range s.AssetDirectory.Items {
			bindings = append(bindings, i.Asset)
		}
	}
	if s.GraphExplorer != nil {
		for _, r := range s.GraphExplorer.Relations {
			bindings = append(bindings, r.Binding)
		}
	}
	if s.VertexGraph != nil {
		for _, g := range s.VertexGraph.Groups {
			bindings = append(bindings, g.Binding)
		}
	}
	return bindings
}
func (s Section) ExplorationReferences() []AssetRef {
	refs := []AssetRef{}
	for _, b := range s.ExplorationBindings() {
		refs = append(refs, b.Ref)
	}
	if s.GraphExplorer != nil {
		for _, o := range s.GraphExplorer.Objects {
			refs = append(refs, o.Object)
		}
	}
	return refs
}
func validExplorationBinding(b AssetBinding) bool {
	return b.Ref.Check() == nil && b.SourceVersion != "" && len(b.SourceVersion) <= 256
}
func (d *PageDocument) checkExploration(s Section) error {
	if s.ResourceList != nil && s.Widget != "resource-list" || s.AssetDirectory != nil && s.Widget != "asset-directory" || s.GraphExplorer != nil && s.Widget != "graph-explorer" || s.VertexGraph != nil && s.Widget != "vertex-graph" {
		return fmt.Errorf("exploration configuration needs its original widget")
	}
	if !explorationWidget(s.Widget) {
		return nil
	}
	c := pageWidgets.Runtime.Exploration
	if !PageUIProfileSupports(d.UIProfile, c.RequiredUIProfile) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.RecordSetVariable != "" || s.FilterVariable != "" || s.Relation != "" || s.ParentSelection != "" || s.Query != (AssetRef{}) || s.InlineEdit != nil || s.Function != nil || s.Operation != nil || len(s.Inputs) > 0 {
		return fmt.Errorf("exploration needs its finite profile without independent business bindings")
	}
	if s.Widget == "resource-list" {
		r, v := s.ResourceList, d.Variables[s.CollectionVariable]
		if r == nil || r.LabelField == "" || r.StatusField == "" || len(r.LabelField) > 256 || len(r.StatusField) > 256 || s.RecordVariable != "" || v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
			return fmt.Errorf("resource list needs its original scoped window, title and status")
		}
		q := d.Queries[v.Source.Query]
		if q.Limit != c.MaxResourceWindow || q.Offset != 0 || !slices.Equal(q.Sort, []string{"id"}) || q.ItemOwner != "" {
			return fmt.Errorf("resource list needs twelve original records, zero offset and ID order")
		}
		seen := map[string]bool{}
		if len(r.StatusTones) > pageWidgets.Runtime.RecordEvents.MaxTones {
			return fmt.Errorf("resource status tones exceed their bound")
		}
		for _, tone := range r.StatusTones {
			if tone.Value == "" || len(tone.Value) > 256 || seen[tone.Value] || !slices.Contains(pageWidgets.Runtime.RecordEvents.Tones, tone.Tone) {
				return fmt.Errorf("resource tones need unique bounded original values and supported tones")
			}
			seen[tone.Value] = true
		}
		return nil
	}
	if s.Selection != "" || s.CollectionVariable != "" {
		return fmt.Errorf("exploration cannot bind an independent collection or selection")
	}
	if s.Widget == "asset-directory" {
		if s.AssetDirectory == nil || len(s.AssetDirectory.Items) < 1 || len(s.AssetDirectory.Items) > c.MaxDirectoryItems || s.Object != (AssetRef{}) || s.RecordVariable != "" {
			return fmt.Errorf("asset directory needs one to four explicit published assets")
		}
		seen := map[string]bool{}
		for _, i := range s.AssetDirectory.Items {
			if !pageNodeID.MatchString(i.ID) || i.Label == "" || len(i.Label) > 1024 || seen[i.ID] || !validExplorationBinding(i.Asset) {
				return fmt.Errorf("directory items need stable IDs, original labels and exact published assets")
			}
			seen[i.ID] = true
		}
		return nil
	}
	v := d.Variables[s.RecordVariable]
	if v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
		return fmt.Errorf("graph needs its original scoped confirmed record producer")
	}
	if s.Widget == "vertex-graph" {
		if s.VertexGraph == nil || len(s.VertexGraph.Groups) != 2 {
			return fmt.Errorf("neighborhood needs its two explicit source groups")
		}
		seen := map[string]bool{}
		for index, g := range s.VertexGraph.Groups {
			limit, badge := 4, "S"
			if index == 1 {
				limit, badge = 3, "A"
			}
			if !pageNodeID.MatchString(g.ID) || seen[g.ID] || !validExplorationBinding(g.Binding) || g.Binding.Ref.Kind != AssetLinkType || g.Direction != "forward" || g.Limit != limit || g.Badge != badge || !slices.Contains(pageWidgets.Runtime.RecordEvents.Tones, g.Tone) {
				return fmt.Errorf("neighborhood needs original forward links with four S and three A nodes")
			}
			seen[g.ID] = true
		}
		return nil
	}
	g := s.GraphExplorer
	if g == nil || len(g.Objects) < 1 || len(g.Objects) > c.MaxObjects || len(g.Relations) > c.MaxRelations || len(g.Outputs) > c.MaxObjects {
		return fmt.Errorf("graph explorer needs bounded explicit objects, relations and output ports")
	}
	objects, relations, variables, ports := map[AssetRef]bool{}, map[AssetRef]bool{}, map[string]bool{}, map[string]bool{}
	for _, o := range g.Objects {
		if o.Object.Check() != nil || o.Object.Kind != AssetObject || objects[o.Object] || o.LabelField == "" || len(o.LabelField) > 256 {
			return fmt.Errorf("graph objects need original unique identities and title fields")
		}
		objects[o.Object] = true
	}
	for _, r := range g.Relations {
		if !pageNodeID.MatchString(r.ID) || ports[r.ID] || relations[r.Binding.Ref] || !validExplorationBinding(r.Binding) || r.Binding.Ref.Kind != AssetLinkType {
			return fmt.Errorf("graph relations need unique retained original link types")
		}
		ports[r.ID] = true
		relations[r.Binding.Ref] = true
	}
	ports = map[string]bool{}
	outputObjects := map[AssetRef]bool{}
	for _, o := range g.Outputs {
		output := d.Variables[o.Variable]
		if !pageNodeID.MatchString(o.ID) || ports[o.ID] || variables[o.Variable] || outputObjects[o.Object] || !objects[o.Object] || output.Type != "record" || output.Mode != "resource" || output.Writable || output.Source == nil || output.Source.Kind != "record" || output.Source.Section != s.ID || output.Source.Port != o.ID || output.Scope != v.Scope || output.Owner != v.Owner {
			return fmt.Errorf("graph output needs one original typed producer port in the root owner")
		}
		ports[o.ID] = true
		variables[o.Variable] = true
		outputObjects[o.Object] = true
	}
	return nil
}
func (s Section) CheckResourceList(info EntityInfo) error {
	if s.Widget != "resource-list" {
		return nil
	}
	r := s.ResourceList
	if r == nil || !visibleContextField(info, r.LabelField, true) {
		return fmt.Errorf("resource list title needs a visible original field or ID")
	}
	status, ok := info.Field(r.StatusField)
	if !ok || !slices.Contains([]string{"choice", "text"}, status.Type) {
		return fmt.Errorf("resource status needs its original visible choice or text field")
	}
	for _, tone := range r.StatusTones {
		if status.Type == "choice" && !slices.Contains(status.Choices, tone.Value) {
			return fmt.Errorf("resource tone does not name an original status choice")
		}
	}
	return nil
}
func (p Page) GraphOutputObject(section, port, variable string) AssetRef {
	if port == "" {
		return AssetRef{}
	}
	for _, s := range p.Sections {
		if s.ID != section || s.Widget != "graph-explorer" || s.GraphExplorer == nil {
			continue
		}
		for _, o := range s.GraphExplorer.Outputs {
			if o.ID == port && o.Variable == variable {
				return o.Object
			}
		}
	}
	return AssetRef{}
}
func (p Page) CheckExplorationBinding(s Section) error {
	if p.Document != nil {
		v := p.Document.Variables[s.RecordVariable]
		if v.Source != nil && v.Source.Port != "" {
			object := s.Object
			if object.Name == "" {
				object = p.Object
			}
			if original := p.RecordResourceObject(s.RecordVariable); original.Name == "" || original != object {
				return fmt.Errorf("graph output consumer differs from its original full object identity")
			}
		}
	}
	if s.Widget != "graph-explorer" && s.Widget != "vertex-graph" {
		return nil
	}
	object := s.Object
	if object.Name == "" {
		object = p.Object
	}
	original := p.RecordResourceObject(s.RecordVariable)
	if original.Name == "" || original != object {
		return fmt.Errorf("graph root differs from its original full record identity")
	}
	if s.GraphExplorer != nil && !slices.ContainsFunc(s.GraphExplorer.Objects, func(o PageGraphObject) bool { return o.Object == original }) {
		return fmt.Errorf("graph object allowlist omits its original root")
	}
	return nil
}
func (p Page) CheckExplorationSchema(s Section, object func(AssetRef) (EntityInfo, bool), link func(AssetBinding) (LinkType, bool)) error {
	if s.Widget != "graph-explorer" && s.Widget != "vertex-graph" {
		return nil
	}
	if err := p.CheckExplorationBinding(s); err != nil {
		return err
	}
	root := p.RecordResourceObject(s.RecordVariable)
	if info, ok := object(root); !ok || info.App != root.App || info.Type != root.Name {
		return fmt.Errorf("graph original root owner is unavailable")
	}
	if s.GraphExplorer != nil {
		objects := map[AssetRef]bool{}
		for _, o := range s.GraphExplorer.Objects {
			info, ok := object(o.Object)
			if !ok || info.App != o.Object.App || info.Type != o.Object.Name || !visibleContextField(info, o.LabelField, true) {
				return fmt.Errorf("graph object or original title is unavailable")
			}
			objects[o.Object] = true
		}
		for _, r := range s.GraphExplorer.Relations {
			l, ok := link(r.Binding)
			parent, pok := object(l.Parent)
			child, cok := object(l.Child)
			if !ok || r.Binding.Ref.App != l.Child.App || !objects[l.Parent] || !objects[l.Child] || !pok || !cok || l.CheckSchema(parent, child) != nil {
				return fmt.Errorf("graph retained relation endpoints or schema are unavailable")
			}
		}
	}
	if s.VertexGraph != nil {
		for _, g := range s.VertexGraph.Groups {
			l, ok := link(g.Binding)
			parent, pok := object(l.Parent)
			child, cok := object(l.Child)
			if !ok || g.Binding.Ref.App != l.Child.App || l.Parent != root || !pok || !cok || l.CheckSchema(parent, child) != nil {
				return fmt.Errorf("neighborhood group must retain its actual original root relation")
			}
		}
	}
	return nil
}
func (p Page) CheckResourceListQuery(id string, named *Definition) error {
	if p.Document == nil {
		return nil
	}
	for _, s := range p.Sections {
		v := p.Document.Variables[s.CollectionVariable]
		if s.Widget == "resource-list" && v.Source != nil && v.Source.Query == id && named != nil && named.Query != nil && (named.Query.Limit != 0 && named.Query.Limit < pageWidgets.Runtime.Exploration.MaxResourceWindow || len(named.Query.Sort) > 0 && !slices.Equal(named.Query.Sort, []string{"id"})) {
			return fmt.Errorf("resource list named query must preserve twelve records and ID order")
		}
	}
	return nil
}

// Member projection keeps only the original readable endpoints and fields.
// Original validation remains strict; a projected graph may have no links.
func (p Page) ProjectExploration(s Section, object func(AssetRef) (EntityInfo, bool), link func(AssetBinding) (LinkType, bool)) (Section, bool) {
	if s.GraphExplorer != nil {
		graph := *s.GraphExplorer
		graph.Objects = slices.DeleteFunc(slices.Clone(graph.Objects), func(o PageGraphObject) bool {
			info, ok := object(o.Object)
			return !ok || info.App != o.Object.App || !visibleContextField(info, o.LabelField, true)
		})
		allowed := map[AssetRef]bool{}
		for _, o := range graph.Objects {
			allowed[o.Object] = true
		}
		if !allowed[p.RecordResourceObject(s.RecordVariable)] {
			return s, false
		}
		graph.Relations = slices.DeleteFunc(slices.Clone(graph.Relations), func(r PageGraphRelation) bool {
			l, ok := link(r.Binding)
			parent, pok := object(l.Parent)
			child, cok := object(l.Child)
			return !ok || r.Binding.Ref.App != l.Child.App || !allowed[l.Parent] || !allowed[l.Child] || !pok || !cok || l.CheckSchema(parent, child) != nil
		})
		reachable := map[AssetRef]bool{p.RecordResourceObject(s.RecordVariable): true}
		for changed := true; changed; {
			changed = false
			for _, r := range graph.Relations {
				l, _ := link(r.Binding)
				if reachable[l.Parent] && !reachable[l.Child] {
					reachable[l.Child] = true
					changed = true
				}
				if reachable[l.Child] && !reachable[l.Parent] {
					reachable[l.Parent] = true
					changed = true
				}
			}
		}
		graph.Objects = slices.DeleteFunc(graph.Objects, func(o PageGraphObject) bool { return !reachable[o.Object] })
		graph.Relations = slices.DeleteFunc(graph.Relations, func(r PageGraphRelation) bool {
			l, _ := link(r.Binding)
			return !reachable[l.Parent] || !reachable[l.Child]
		})
		graph.Outputs = slices.DeleteFunc(slices.Clone(graph.Outputs), func(o PageGraphOutput) bool { return !reachable[o.Object] })
		if graph.Relations == nil {
			graph.Relations = []PageGraphRelation{}
		}
		s.GraphExplorer = &graph
	}
	if s.VertexGraph != nil {
		graph := *s.VertexGraph
		graph.Groups = slices.DeleteFunc(slices.Clone(graph.Groups), func(g PageNeighborhoodGroup) bool {
			l, ok := link(g.Binding)
			parent, pok := object(l.Parent)
			child, cok := object(l.Child)
			return !ok || l.Parent != p.RecordResourceObject(s.RecordVariable) || !pok || !cok || l.CheckSchema(parent, child) != nil
		})
		s.VertexGraph = &graph
	}
	return s, true
}

func checkFrozenExploration(p Page, s Section, lookup map[AssetRef]ReleaseAsset) error {
	if !explorationWidget(s.Widget) {
		return nil
	}
	for _, b := range s.ExplorationBindings() {
		asset, ok := lookup[b.Ref]
		if !ok || asset.SourceVersion != b.SourceVersion {
			return fmt.Errorf("exploration needs its exact original published asset %s", b.Ref)
		}
	}
	object := func(ref AssetRef) (EntityInfo, bool) {
		asset, ok := lookup[ref]
		info, err := queryObjectDescriptor(asset.Body)
		if !ok || err != nil || !frozenObjectOwnerMatches(asset, ref) {
			return EntityInfo{}, false
		}
		info.App = ref.App
		return info, true
	}
	if s.Widget == "resource-list" {
		ref := s.Object
		if ref.Name == "" {
			ref = p.Object
		}
		info, ok := object(ref)
		if !ok || s.CheckResourceList(info) != nil {
			return fmt.Errorf("frozen resource list original owner/title/status is unavailable")
		}
	}
	return p.CheckExplorationSchema(s, object, func(b AssetBinding) (LinkType, bool) {
		asset, ok := lookup[b.Ref]
		var l LinkType
		err := json.Unmarshal(asset.Body, &l)
		return l, ok && err == nil && asset.SourceVersion == b.SourceVersion
	})
}
