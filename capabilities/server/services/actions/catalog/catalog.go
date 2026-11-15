package catalog

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

func payloadColumns(columns []platform.FieldInfo) []platform.Field {
	if len(columns) == 0 {
		return nil
	}
	out := make([]platform.Field, 0, len(columns))
	for _, column := range columns {
		if column.ReadOnly {
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money", "attachment": "attachment", "json": "json", "lines": "lines"}[column.Type]
		if typ == "" {
			typ = "string"
		}
		field := platform.Field{Name: column.Name, Type: typ, Required: column.Required && column.When == nil, Description: column.Title,
			Choices: column.Choices, Constraints: column.Constraints, Range: column.Range, Fields: payloadColumns(column.Fields)}
		if column.Ref == "enterprise.element" {
			field.Ref, field.Stereotype, field.Stereotypes = column.Ref, column.Stereotype, slices.Clone(column.Stereotypes)
		}
		out = append(out, field)
	}
	return out
}

func EntityActions(e platform.Entity) []platform.Action {
	info, err := platform.Describe("", e, func(reflect.Type) string { return "?" })
	if err != nil {
		panic(err)
	}
	var fields, editable []platform.Field
	for _, f := range info.Fields {
		if f.ReadOnly || f.Aside {
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money", "attachment": "attachment", "json": "json", "lines": "lines"}[f.Type]
		if typ == "" {
			typ = "string"
		}
		description := f.Title
		if f.Help != "" {
			description += ": " + f.Help
		}
		if len(f.Choices) > 0 {
			description += " (" + strings.Join(f.Choices, ", ") + ")"
		}
		if f.Example != "" {
			description += ", e.g. " + f.Example
		}
		field := platform.Field{Name: f.Name, Type: typ, Required: f.Required && f.When == nil, Description: description, Choices: f.Choices, Constraints: f.Constraints, ValueType: f.ValueType, Range: f.Range, Group: f.Group,
			Fields: payloadColumns(f.Fields)}
		if f.Ref == "enterprise.element" {
			field.Ref, field.Stereotype, field.Stereotypes = f.Ref, f.Stereotype, slices.Clone(f.Stereotypes)
		}
		fields = append(fields, field)
		field.Required = false
		editable = append(editable, field)
	}
	var out []platform.Action
	capability := e.Standard.Capability
	if capability == "" {
		capability = e.Type
	}
	if e.Standard.Create {
		if e.CreateTxn != nil {
			for _, step := range e.CreateTxn.Steps {
				if step.Kind == "update" && step.Target.Type == e.Type && step.Target.ID == "$subject" && step.When == nil {
					for _, assignment := range step.Changes {
						for i := range fields {
							if fields[i].Name == assignment.Field {
								fields[i].Required = false
							}
						}
					}
				}
			}
		}
		out = append(out, platform.Action{Schema: e.Type + ".create", Target: e.Type, New: true, Capability: capability, Title: "Create " + strings.ToLower(info.Title),
			Description: "Create " + article(info.Title) + ".", Payload: fields, Roles: roles(e.Standard, "create")})
	}
	if e.Standard.Edit {
		out = append(out, platform.Action{RecordEdit: true, Schema: e.Type + ".edit", Target: e.Type, Capability: capability, Title: "Edit " + strings.ToLower(info.Title),
			Description: "Change fields of " + article(info.Title) + "; fields left out keep their value.", Payload: editable, Roles: roles(e.Standard, "edit")})
	}
	if e.Standard.Archive {
		out = append(out, platform.Action{Schema: e.Type + ".archive", Target: e.Type, Capability: capability, Title: "Archive " + strings.ToLower(info.Title),
			Description: "Archive " + article(info.Title) + ": it leaves lists but stays referenced and in history.", Payload: []platform.Field{}, Roles: roles(e.Standard, "archive")})
	}
	if e.Lifecycle != nil {
		for i, t := range e.Lifecycle.Actions {
			c := t.Capability
			if c == "" {
				c = e.Type
			}
			payload := t.Parameters
			if payload == nil {
				payload = []platform.Field{}
			}
			description := t.Description
			if description == "" {
				move := info.Lifecycle.Transitions[i]
				to := move.To
				if slices.ContainsFunc(t.Rules, func(rule platform.ActionRule) bool { return rule.Transaction != nil }) {
					to = move.From
				}
				description = fmt.Sprintf("Move %s from %s to %s.", article(info.Title), strings.Join(move.From, " or "), strings.Join(to, " or "))
			}
			out = append(out, platform.Action{Schema: e.Type + "." + t.Name, Target: e.Type, Capability: c, Title: info.Lifecycle.Transitions[i].Title,
				Description: description, Payload: payload, Roles: t.Roles, Adoption: t.Adoption, Approval: t.Approval, DeferRequired: t.DeferRequired, ConfirmationOnly: t.ConfirmationOnly})
			if t.Approval != nil && t.Approval.Pending != "" {
				for _, m := range [][2]string{{platform.ApprovalHeld, "Hold for approval"}, {platform.ApprovalRejected, "Mark rejected"}, {platform.ApprovalReturned, "Return from approval"}} {
					suffix, title := m[0], m[1]
					out = append(out, platform.Action{Schema: e.Type + "." + t.Name + suffix, Target: e.Type, Capability: c, Title: title,
						Description: "Moves the record while its approval runs; only the approval takes it.", Payload: []platform.Field{}, Automation: true})
				}
			}
		}
	}
	return out
}

func article(title string) string {
	t := strings.ToLower(title)
	if t != "" && strings.ContainsRune("aeiou", rune(t[0])) {
		return "an " + t
	}
	return "a " + t
}

func roles(s platform.Standard, verb string) []string {
	own := map[string][]string{"create": s.CreateRoles, "edit": s.EditRoles, "archive": s.ArchiveRoles}[verb]
	if own != nil {
		return own
	}
	return s.Roles
}
