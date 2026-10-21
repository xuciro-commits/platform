package platform

// page_validation.go — shared helpers for Section widget checks (ADR-0070).
// Many widget validators repeat the same "no business bindings" guards
// (Fields/Actions/CollectionVariable/Selection). Centralizing them
// removes ~80 duplicate inline conditions without changing error semantics.

// hasBusinessBindings reports whether a presentation-only Section carries
// business fields, actions, or collection bindings that only data widgets need.
func hasBusinessBindings(s Section) bool {
	return len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != ""
}

// hasCollection reports whether the Section carries any collection-related
// variable (used by table/card/list widgets to forbid spurious bindings).
func hasCollection(s Section) bool {
	return s.CollectionVariable != "" || s.Selection != ""
}

// hasRecordBindings reports whether the Section carries record-level
// bindings outside its widget contract.
func hasRecordBindings(s Section) bool {
	return s.RecordVariable != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.RecordSetVariable != "" || s.Relation != "" || s.ParentSelection != "" || s.Query != (AssetRef{}) || s.InlineEdit != nil || s.Function != nil || s.Operation != nil || len(s.Inputs) > 0
}


