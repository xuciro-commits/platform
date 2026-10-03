package platform

import (
	"fmt"
	"slices"
)

type PageLoop struct {
	Collection   string `json:"collection"`
	ItemVariable string `json:"itemVariable"`
	Limit        int    `json:"limit"`
}

func (d *PageDocument) loopOwners() map[string]string {
	owners := map[string]string{}
	var visit func(string, string)
	visit = func(id, owner string) {
		if _, seen := owners[id]; seen {
			return
		}
		owners[id] = owner
		node := d.Nodes[id]
		if node.Kind == "loop" {
			owner = id
		}
		for _, child := range d.ownedChildren(id) {
			visit(child, owner)
		}
	}
	visit(d.Root, "")
	for _, overlay := range d.Overlays {
		visit(overlay.Root, "")
	}
	return owners
}

// Each independent presentation root owns its Overlay locals.
func (d *PageDocument) overlayOwners() map[string]string {
	owners := map[string]string{}
	var visit func(string, string)
	visit = func(id, owner string) {
		if _, seen := owners[id]; seen {
			return
		}
		owners[id] = owner
		for _, child := range d.ownedChildren(id) {
			visit(child, owner)
		}
	}
	visit(d.Root, "")
	for id, overlay := range d.Overlays {
		visit(overlay.Root, id)
	}
	return owners
}

func (d *PageDocument) checkLoops(sections []Section) error {
	contract := pageWidgets.Runtime.Loop
	owners := d.loopOwners()
	overlays := d.overlayOwners()
	sectionOverlays := map[string]string{}
	sectionOwners := map[string]string{}
	for id, node := range d.Nodes {
		if node.Kind == "widget" {
			sectionOwners[node.Section] = owners[id]
			sectionOverlays[node.Section] = overlays[id]
		}
	}
	accessible := func(variable, owner, overlay string) bool {
		v, ok := d.Variables[variable]
		return variable == "" || ok && (v.Scope == "page" || v.Scope == "application" || v.Scope == "loop-item" && v.Owner == owner && owner != "" || v.Scope == "overlay" && v.Owner == overlay && overlay != "")
	}
	count, total := 0, 0
	for id, node := range d.Nodes {
		owner := owners[id]
		if !accessible(node.VisibleWhen, owner, overlays[id]) || !accessible(node.EnabledWhen, owner, overlays[id]) || !accessible(node.ActiveVariable, owner, overlays[id]) || !accessible(node.ValueVariable, owner, overlays[id]) {
			return fmt.Errorf("page node %s variable escapes its presentation scope", id)
		}
		if node.Kind != "loop" {
			if node.Loop != nil {
				return fmt.Errorf("page node %s is not a loop", id)
			}
			continue
		}
		if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.5") {
			return fmt.Errorf("page loop %s requires UI profile v2.5", id)
		}
		if owner != "" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.20") {
			return fmt.Errorf("page loop %s cannot nest in another loop", id)
		}
		if owner != "" && owners[owner] != "" {
			return fmt.Errorf("page loop %s exceeds supported depth", id)
		}
		loop := node.Loop
		if loop == nil || loop.Limit < 1 || loop.Limit > contract.MaxItems {
			return fmt.Errorf("page loop %s needs a bounded limit", id)
		}
		count++
		factor, err := d.loopFactor(owner)
		if err != nil {
			return err
		}
		total += loop.Limit * factor
		collection := d.Variables[loop.Collection]
		item := d.Variables[loop.ItemVariable]
		if !accessible(loop.Collection, owner, overlays[id]) || collection.Type != "object-set" || collection.Source == nil || !(collection.Mode == "resource" && (collection.Scope == "page" || collection.Scope == "overlay" || collection.Scope == "loop-item" && collection.Owner == owner) && (collection.Source.Kind == "query" || collection.Source.Kind == "plan") || collection.Mode == "shared" && collection.Scope == "application" && collection.Source.Kind == "application" && collection.Source.Object != nil) || collection.Source.Kind == "query" && sectionOwners[collection.Source.Section] != "" {
			return fmt.Errorf("page loop %s needs an external scoped query window", id)
		}
		if owner != "" && (collection.Source.Kind != "plan" || d.Queries[collection.Source.Query].ItemOwner != owner) {
			return fmt.Errorf("nested loop must read its parent-owned plan")
		}
		if item.Scope != contract.Scope || item.Type != "record" || item.Mode != "resource" || item.Owner != id || item.Source == nil || item.Source.Kind != contract.Source || item.Source.Node != id {
			return fmt.Errorf("page loop %s needs its own item record variable", id)
		}
	}
	if count > contract.MaxContainers || total > contract.MaxTotalItems {
		return fmt.Errorf("page loop budget exceeded")
	}
	for id, variable := range d.Variables {
		if variable.Mode == "resource" && variable.Source != nil && variable.Source.Section != "" {
			producer := sectionOverlays[variable.Source.Section]
			if (variable.Scope == "page" && producer != "" && PageUIProfileSupports(d.UIProfile, "platform.page.v2.11")) || (variable.Scope == "overlay" && variable.Owner != producer) {
				return fmt.Errorf("page variable %s resource escapes its producer scope", id)
			}
		}
		if variable.Scope == "overlay" {
			if _, ok := d.Overlays[variable.Owner]; !ok {
				return fmt.Errorf("page variable %s needs an existing overlay owner", id)
			}
		}
		if variable.Scope != contract.Scope {
			continue
		}
		loop := d.Nodes[variable.Owner].Loop
		if loop == nil || variable.Mode == "resource" && loop.ItemVariable != id && !(variable.Type == "object-set" && variable.Source != nil && variable.Source.Kind == "plan" && d.Queries[variable.Source.Query].ItemOwner == variable.Owner) {
			return fmt.Errorf("page variable %s needs an existing loop owner", id)
		}
	}
	for _, section := range sections {
		for _, id := range []string{section.GroupValueVariable, section.GroupSetVariable} {
			if !accessible(id, "", sectionOverlays[section.ID]) || section.Widget == "treemap" && sectionOwners[section.ID] != "" {
				return fmt.Errorf("treemap filter needs its original page or overlay owner")
			}
		}
		for _, id := range []string{section.RowValueVariable, section.RowSetVariable, section.ColumnValueVariable, section.ColumnSetVariable} {
			if !accessible(id, "", sectionOverlays[section.ID]) || section.Widget == "heatmap" && sectionOwners[section.ID] != "" {
				return fmt.Errorf("heatmap filter needs its original page or overlay owner")
			}
		}
		if !accessible(section.PickerValueVariable, "", sectionOverlays[section.ID]) {
			return fmt.Errorf("picker ID state cannot escape its scope")
		}
		if !accessible(section.AlertValueVariable, "", sectionOverlays[section.ID]) || section.Widget == "alert-banner" && sectionOwners[section.ID] != "" {
			return fmt.Errorf("alert cannot escape its scope or enter a loop")
		}
		if !accessible(section.DateVariable, "", sectionOverlays[section.ID]) || section.Widget == "date-input" && sectionOwners[section.ID] != "" {
			return fmt.Errorf("date input cannot escape its scope or enter a loop")
		}
		if !accessible(section.ChoiceSetVariable, "", sectionOverlays[section.ID]) || !accessible(section.ChoiceVariable, "", sectionOverlays[section.ID]) || section.Widget == "choice-input" && sectionOwners[section.ID] != "" {
			return fmt.Errorf("choice input cannot escape its scope or enter a loop")
		}
		if !accessible(section.BooleanVariable, "", sectionOverlays[section.ID]) || section.Widget == "boolean-input" && sectionOwners[section.ID] != "" {
			return fmt.Errorf("boolean input cannot escape its scope or enter a loop")
		}
		if !accessible(section.RangeMinVariable, "", sectionOverlays[section.ID]) || !accessible(section.RangeMaxVariable, "", sectionOverlays[section.ID]) || section.Widget == "range-input" && sectionOwners[section.ID] != "" {
			return fmt.Errorf("range drafts cannot escape their scope or enter a loop")
		}
		if !accessible(section.StatisticsVariable, "", sectionOverlays[section.ID]) || section.Widget == "summary-stats" && sectionOwners[section.ID] != "" {
			return fmt.Errorf("summary cannot escape its scope or enter a loop")
		}
		if !accessible(section.GaugeValueVariable, sectionOwners[section.ID], sectionOverlays[section.ID]) {
			return fmt.Errorf("gauge scalar cannot escape its scope")
		}
		if !accessible(section.ProgressValueVariable, sectionOwners[section.ID], sectionOverlays[section.ID]) || !accessible(section.ProgressTotalVariable, sectionOwners[section.ID], sectionOverlays[section.ID]) {
			return fmt.Errorf("progress scalar cannot escape its scope")
		}
		if !accessible(section.CountVariable, "", sectionOverlays[section.ID]) || section.Widget == "collection-title" && sectionOwners[section.ID] != "" {
			return fmt.Errorf("collection count cannot escape its scope or enter a loop")
		}
		if !accessible(section.CollectionVariable, "", sectionOverlays[section.ID]) {
			return fmt.Errorf("page section %s window escapes its overlay scope", section.ID)
		}
		if section.SelectionSetVariable != "" && (!accessible(section.SelectionSetVariable, "", sectionOverlays[section.ID]) || sectionOwners[section.ID] != "") {
			return fmt.Errorf("record selection set escapes its local table scope")
		}
		owner := sectionOwners[section.ID]
		if owner != "" && !slices.Contains(contract.RecordWidgets, section.Widget) && !slices.Contains(contract.PresentationWidgets, section.Widget) {
			return fmt.Errorf("page loop %s does not support widget %s", owner, section.Widget)
		}
		if owner != "" && slices.Contains(contract.RecordWidgets, section.Widget) && section.RecordVariable != d.Nodes[owner].Loop.ItemVariable {
			return fmt.Errorf("page loop section %s must bind its item record", section.ID)
		}
		if section.FilterVariable != "" {
			v := d.Variables[section.FilterVariable]
			if owner != "" || section.CollectionVariable != "" || v.Type != "filter" || v.Mode != "shared" || (section.Widget == "filter" && (!v.Writable || sectionOverlays[section.ID] != "")) {
				return fmt.Errorf("page section %s has an invalid shared filter port", section.ID)
			}
		}
		if section.SelectionVariable != "" {
			v := d.Variables[section.SelectionVariable]
			if owner != "" || sectionOverlays[section.ID] != "" || section.Selection != "" || v.Type != "record" || v.Mode != "shared" || !v.Writable {
				return fmt.Errorf("page section %s needs a writable shared record output on a root table", section.ID)
			}
		}
		if section.RecordVariable != "" {
			v := d.Variables[section.RecordVariable]
			if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.5") || (owner == "" || v.Scope != "loop-item" || v.Owner != owner) && !(v.Scope == "page" && v.Mode == "input") && !(v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Object != nil && PageUIProfileSupports(d.UIProfile, "platform.page.v2.14")) && !(v.Mode == "resource" && v.Source != nil && v.Source.Kind == "record" && accessible(section.RecordVariable, "", sectionOverlays[section.ID]) && PageUIProfileSupports(d.UIProfile, "platform.page.v2.11")) || v.Type != "record" || section.Selection != "" || !slices.Contains(contract.RecordWidgets, section.Widget) {
				return fmt.Errorf("page section %s record binding escapes its loop scope", section.ID)
			}
		}
	}
	for _, event := range d.Events {
		if event.Event == "select" && sectionOwners[event.Source] != "" {
			return fmt.Errorf("selection event does not support a loop producer")
		}
		if !accessible(event.Target, sectionOwners[event.Source], sectionOverlays[event.Source]) {
			return fmt.Errorf("page event %s target escapes its presentation scope", event.Source)
		}
		if event.Navigate != nil {
			for _, arg := range event.Navigate.Inputs {
				if !accessible(arg.Variable, sectionOwners[event.Source], sectionOverlays[event.Source]) {
					return fmt.Errorf("page navigation input escapes its presentation scope")
				}
			}
			for _, target := range event.Navigate.Results {
				if !accessible(target, sectionOwners[event.Source], sectionOverlays[event.Source]) {
					return fmt.Errorf("page navigation output escapes its presentation scope")
				}
			}
		}
	}
	return nil
}

// LoopRecordSource returns the original query-producing section. The host
// compares its object with each template binding using its native entity API.
func (d *PageDocument) LoopRecordSource(variable string) string {
	if d == nil {
		return ""
	}
	v := d.Variables[variable]
	loop := d.Nodes[v.Owner].Loop
	if loop == nil {
		return ""
	}
	collection := d.Variables[loop.Collection]
	if collection.Source == nil {
		return ""
	}
	return collection.Source.Section
}

func (d *PageDocument) loopFactor(owner string) (int, error) {
	factor := 1
	seen := map[string]bool{}
	owners := d.loopOwners()
	for owner != "" {
		loop := d.Nodes[owner].Loop
		if seen[owner] || loop == nil || loop.Limit < 1 || loop.Limit > pageWidgets.Runtime.Loop.MaxItems || len(seen) >= pageWidgets.Runtime.Loop.MaxDepth {
			return 0, fmt.Errorf("invalid loop ancestor budget")
		}
		seen[owner] = true
		factor *= loop.Limit
		owner = owners[owner]
	}
	return factor, nil
}
