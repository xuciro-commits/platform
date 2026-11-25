package catalog

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/kernel/records"
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

func EntityActions(e platform.RecordType) []platform.Action {
	info, err := records.Describe("", e, func(reflect.Type) string { return "?" })
	if err != nil {
		panic(err)
	}
	var fields, editable []platform.Field
	for _, f := range info.Fields {
		if f.ReadOnly || f.Supplemental {
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
		createField := field
		if f.Default != "" {
			createField.Default = defaultPayload(f.Type, f.Default)
			createField.Required = createField.Required && f.Type == "money"
		}
		if f.Provenance == "from" {
			createField.Required = false
		}
		fields = append(fields, createField)
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
		reverse := ""
		if e.Standard.Restore {
			reverse = e.Type + ".restore"
		}
		out = append(out, platform.Action{Schema: e.Type + ".archive", Reverse: reverse, Target: e.Type, Capability: capability, Title: "Archive " + strings.ToLower(info.Title),
			Description: "Archive " + article(info.Title) + ": it leaves lists but stays referenced and in history.", Payload: []platform.Field{}, Roles: roles(e.Standard, "archive")})
	}
	if e.Standard.Restore {
		out = append(out, platform.Action{Schema: e.Type + ".restore", Reverse: e.Type + ".archive", Target: e.Type, Capability: capability, Title: "Restore " + strings.ToLower(info.Title),
			Description: "Return this archived record to its lists.", Payload: []platform.Field{}, Roles: roles(e.Standard, "archive")})
	}
	if e.Lifecycle != nil {
		var reverses map[int]string
		if info.Lifecycle != nil {
			reverses = stateReverses(e, info.Lifecycle)
		}
		for i, t := range e.Lifecycle.Actions {
			c := t.Capability
			if c == "" {
				c = e.Type
			}
			payload := t.Parameters
			if payload == nil {
				payload = []platform.Field{}
			}
			title := t.Title
			if info.Lifecycle != nil {
				title = info.Lifecycle.Transitions[i].Title
			} else if title == "" {
				title = strings.ToUpper(t.Name[:1]) + t.Name[1:]
			}
			description := t.Description
			if description == "" {
				if info.Lifecycle != nil {
					move := info.Lifecycle.Transitions[i]
					to := move.To
					if slices.ContainsFunc(t.Rules, func(rule platform.ActionRule) bool { return rule.Transaction != nil }) {
						to = move.From
					}
					description = fmt.Sprintf("Move %s from %s to %s.", article(info.Title), strings.Join(move.From, " or "), strings.Join(to, " or "))
				} else {
					description = fmt.Sprintf("Apply %s to %s.", t.Name, article(info.Title))
				}
			}
			out = append(out, platform.Action{Schema: e.Type + "." + t.Name, Reverse: reverses[i], Target: e.Type, Capability: c, Title: title,
				Description: description, Payload: payload, Roles: t.Roles, Adoption: t.Adoption, Approval: t.Approval, DeferRequired: t.DeferRequired, ConfirmationOnly: t.ConfirmationOnly, Open: t.Open})
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

func stateReverses(e platform.RecordType, life *platform.LifecycleInfo) map[int]string {
	type route struct{ from, to string }
	directions := make(map[route][]int)
	for i, action := range e.Lifecycle.Actions {
		move := life.Transitions[i]
		if len(move.From) != 1 || len(move.To) != 1 || move.From[0] == move.To[0] || move.ToInput != "" ||
			action.Approval != nil || action.Adoption != nil || action.ConfirmationOnly || len(action.Effects) != 0 || len(action.Parameters) != 0 || len(action.Rules) != 1 {
			continue
		}
		rule := action.Rules[0]
		if rule.Function != nil || rule.Receipt != nil {
			continue
		}
		if rule.Transaction != nil {
			txn := rule.Transaction.Spec
			if txn == nil || txn.Guard != nil || txn.Confirmation != nil || txn.Dispatch != nil || len(txn.Refusals) != 0 || len(txn.Steps) != 1 {
				continue
			}
			step := txn.Steps[0]
			if step.Kind != "update" || step.Target.Type != e.Type || step.Target.ID != "$subject" || step.Target.Via != "" || step.When != nil ||
				len(step.Refusals) != 0 || len(step.Changes) != 1 || step.Changes[0].Field != life.Field {
				continue
			}
		} else if rule.Edit == nil || rule.Edit.Field != life.Field || rule.Edit.Parameter != "" {
			continue
		}
		key := route{move.From[0], move.To[0]}
		directions[key] = append(directions[key], i)
	}
	reverses := make(map[int]string)
	for key, moves := range directions {
		back := directions[route{key.to, key.from}]
		if len(moves) == 1 && len(back) == 1 {
			reverses[moves[0]] = life.Transitions[back[0]].Schema
		}
	}
	return reverses
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

func defaultPayload(kind, raw string) []byte {
	if raw == "member" || raw == "today" || raw == "now" || strings.HasPrefix(raw, "setting:") {
		return platform.Raw(raw)
	}
	switch kind {
	case "boolean":
		if raw == "true" || raw == "false" {
			return []byte(raw)
		}
	case "integer", "decimal":
		return []byte(raw)
	}
	return platform.Raw(raw)
}
