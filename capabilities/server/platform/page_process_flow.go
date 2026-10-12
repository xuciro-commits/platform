package platform

import (
	"fmt"
	"slices"
)

// checkProcessFlow admits a process flow only where its widget profile is
// available, with the declared bindings and nothing else to read from the page.
func (d *PageDocument) checkProcessFlow(s Section) error {
	if s.Widget != "process-flow" {
		if s.ProcessFlow != nil {
			return fmt.Errorf("process flow configuration needs its widget")
		}
		return nil
	}
	value := s.ProcessFlow
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.ProcessFlow.RequiredUIProfile) || value == nil ||
		value.StepObject == "" || value.RoutingField == "" || value.VersionField == "" || value.StepVersionField == "" || value.StepField == "" || value.OrderField == "" ||
		len(s.Fields) > 0 || len(s.Actions) > 0 || s.Query.Name != "" || s.Relation != "" {
		return fmt.Errorf("process flow needs its profile and the steps, routing, version, step and order it reads")
	}
	return nil
}

// CheckProcessFlow reads the declared bindings against the record's object and
// the step object (ADR-0098 D4): the routing reference the steps hang from, the
// integer version the record pins, the reference to the step it stands at, and
// an order that the steps are sorted by, and the version each step belongs to.
// Lane, title and notes are optional.
func (s Section) CheckProcessFlow(info EntityInfo, entity func(string) (EntityInfo, bool)) error {
	if s.Widget != "process-flow" {
		return nil
	}
	value := s.ProcessFlow
	if value == nil {
		return fmt.Errorf("process flow needs its bindings")
	}
	routing, ok := info.Field(value.RoutingField)
	if !ok || routing.Type != "reference" || routing.Ref == "" {
		return fmt.Errorf("process flow routing field is unavailable")
	}
	version, ok := info.Field(value.VersionField)
	if !ok || version.Type != "integer" {
		return fmt.Errorf("process flow version field must be an integer")
	}
	step, ok := info.Field(value.StepField)
	if !ok || step.Type != "reference" || step.Ref != value.StepObject {
		return fmt.Errorf("process flow step field must refer to the step object")
	}
	if value.NotesField != "" {
		if notes, ok := info.Field(value.NotesField); !ok || notes.Type != "lines" {
			return fmt.Errorf("process flow notes must be a lines field")
		}
	}
	stepInfo, ok := entity(value.StepObject)
	if !ok {
		return fmt.Errorf("process flow step object is unavailable")
	}
	if stepVersion, ok := stepInfo.Field(value.StepVersionField); !ok || stepVersion.Type != "integer" {
		return fmt.Errorf("process flow step version must be an integer on the step object")
	}
	order, ok := stepInfo.Field(value.OrderField)
	if !ok || !slices.Contains([]string{"integer", "decimal"}, order.Type) {
		return fmt.Errorf("process flow order must be a number on the step object")
	}
	if !slices.ContainsFunc(stepInfo.Fields, func(f FieldInfo) bool { return f.Type == "reference" && f.Ref == routing.Ref }) {
		return fmt.Errorf("process flow steps must refer to the routing")
	}
	if value.TitleField != "" {
		if _, ok := stepInfo.Field(value.TitleField); !ok {
			return fmt.Errorf("process flow title field is unavailable")
		}
	}
	if value.LaneField != "" {
		if lane, ok := stepInfo.Field(value.LaneField); !ok || lane.Type != "reference" {
			return fmt.Errorf("process flow lane must be a reference on the step object")
		}
	}
	return nil
}
