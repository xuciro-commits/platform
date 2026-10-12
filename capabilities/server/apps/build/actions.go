package build

import (
	"encoding/json"
	"fmt"
	buildapi "platformserver/internal/host/buildapi"

	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// States and actions a tenant gives its own objects (ADR-0037 D1–D3). An action
// is typed data compiled into the platform's own lifecycle transition: the
// ledger routes it, replay runs it again, and every surface that offers a coded
// transition offers it. Nothing here is code the tenant supplies.

// creator makes one related record, or one balance after a posting, inside
// the decision being taken; nil where no host is attached (descriptors,
// validation), which refuses a Create or a Post.
type creator interface {
	create(c platform.Caller, cr Create, inputs map[string]any, parentType, parent, id string, now time.Time) (any, *kernel.Error)
	createBatch(c platform.Caller, o Object, a Action, cr Create, ruleIdx int, inputs map[string]any, record any, overlay []any, now time.Time) ([]any, *kernel.Error)
	updateRelated(c platform.Caller, o Object, a Action, u UpdateEffect, inputs map[string]any, record any, overlay []any, now time.Time) (any, bool, *kernel.Error)
	post(c platform.Caller, p Post, inputs map[string]any, record any, now time.Time) (any, *kernel.Error)
	evalSource(c platform.Caller, o Object, a Action, targetField Field, from string, inputs map[string]any, record any, overlay []any, loopIdx, loopCount int, loopItem map[string]any, now time.Time) (any, bool, *kernel.Error)
	evalQuantifier(c platform.Caller, o Object, a Action, cond Condition, inputs map[string]any, record any, overlay []any, now time.Time) (bool, *kernel.Error)
}

// Operators are the comparisons a condition may make.
var Operators = []string{"=", "!=", "<", "<=", ">", ">=", "in", "not in", "contains all", "contains any", "empty", "not empty"}

// checkProcess refuses states and actions people could not take: names that are
// not names, an action from or to a state the object has not, an input or a
// field it does not have, a condition that compares nothing.
func checkProcess(o Object, lookup func(string) (platform.EntityInfo, bool)) error {
	if len(o.States) == 0 && len(o.Actions) > 0 {
		return fmt.Errorf("an object needs states before it has actions")
	}
	states := map[string]bool{}
	for _, s := range o.States {
		switch {
		case !named(s.Name):
			return fmt.Errorf("the state name %q is not lower-case letters and digits", s.Name)
		case states[s.Name]:
			return fmt.Errorf("the state %q is declared twice", s.Name)
		case s.Title == "":
			return fmt.Errorf("the state %q has no title", s.Name)
		}
		states[s.Name] = true
	}
	fields := map[string]Field{}
	for _, f := range o.Fields {
		fields[f.Name] = f
	}
	seen := map[string]bool{}
	for _, a := range o.Actions {
		where := fmt.Sprintf("the action %q", a.Name)
		switch {
		case !named(a.Name):
			return fmt.Errorf("the action name %q is not lower-case letters and digits", a.Name)
		case seen[a.Name]:
			return fmt.Errorf("%s is declared twice", where)
		case slices.Contains([]string{"create", "edit", "archive", "publish"}, a.Name):
			return fmt.Errorf("%s is the platform's own", where)
		case a.Title == "":
			return fmt.Errorf("%s has no title", where)
		case !a.New && len(a.From) == 0:
			return fmt.Errorf("%s says no state it is taken from", where)
		case a.New && len(a.From) > 0:
			return fmt.Errorf("%s creates a new record and cannot name starting states", where)
		case a.New && (a.Approval != nil || a.Reverses != ""):
			return fmt.Errorf("%s creates a new record and cannot wait for approval or reverse another action", where)
		case a.ToInput != "" && (a.To != "" || a.Approval != nil):
			return fmt.Errorf("%s cannot combine input-selected state with a fixed state or approval", where)
		case a.To != "" && !states[a.To]:
			return fmt.Errorf("%s leaves records in %q, which is not a state of the object", where, a.To)
		}
		seen[a.Name] = true
		for _, s := range a.From {
			if !states[s] {
				return fmt.Errorf("%s is taken from %q, which is not a state of the object", where, s)
			}
		}
		if approval := a.Approval; approval != nil {
			switch {
			case len(a.From) != 1:
				return fmt.Errorf("%s waiting for approval needs exactly one starting state", where)
			case !states[approval.Pending] || approval.Pending == a.From[0] || approval.Pending == a.To:
				return fmt.Errorf("%s waits in %q, which must be a separate state of the object", where, approval.Pending)
			case approval.Rejected != "" && (!states[approval.Rejected] || approval.Rejected == approval.Pending):
				return fmt.Errorf("%s rejects into %q, which must be a state other than pending", where, approval.Rejected)
			case len(approval.Levels) == 0:
				return fmt.Errorf("%s waiting for approval needs an approver level", where)
			}
			for i, level := range approval.Levels {
				if strings.TrimSpace(level.Title) == "" || level.Role == "" {
					return fmt.Errorf("%s approval level %d needs a title and role", where, i+1)
				}
				if level.Role != Builder && (len(o.Access) == 0 && level.Role != User || len(o.Access) > 0 && !slices.ContainsFunc(o.Access, func(x Access) bool { return x.Role == level.Role && x.Read == "all" })) {
					return fmt.Errorf("%s approval level %d names %q, which cannot read every record of this object", where, i+1, level.Role)
				}
			}
		}
		if !a.New && a.To == "" && len(a.From) == 0 {
			return fmt.Errorf("%s leaves nothing to stay in", where)
		}
		inputs := map[string]Input{}
		for _, in := range a.Inputs {
			switch {
			case !named(in.Name):
				return fmt.Errorf("%s: the input name %q is not lower-case letters and digits", where, in.Name)
			case inputs[in.Name].Name != "":
				return fmt.Errorf("%s: the input %q is declared twice", where, in.Name)
			case in.Title == "":
				return fmt.Errorf("%s: the input %q has no label", where, in.Name)
			case inputTypes[in.Type] == "":
				return fmt.Errorf("%s: the input %q has no type %q", where, in.Name, in.Type)
			case in.Type == "choice" && len(buildapi.ValueChoices(in.Choices)) == 0:
				return fmt.Errorf("%s: the choice input %q has no values", where, in.Name)
			}
			if in.Type == "reference" {
				app, _, ok := strings.Cut(in.Ref, ".")
				if !ok || in.Choices != "" || (platform.AssetRef{App: app, Kind: platform.AssetObject, Name: in.Ref}).Check() != nil {
					return fmt.Errorf("%s needs a declared reference object for %s", where, in.Name)
				}
				if lookup != nil {
					info, known := lookup(in.Ref)
					if !known || info.App != app || info.Type != in.Ref {
						return fmt.Errorf("%s reference object %s is unavailable", where, in.Ref)
					}
				}
			} else if in.Ref != "" {
				return fmt.Errorf("%s non-reference input cannot name a reference object", where)
			}
			if in.MinLength != nil && (!slices.Contains([]string{"text", "longtext"}, in.Type) || *in.MinLength < 0 || *in.MinLength > 4096) {
				return fmt.Errorf("%s input minimum length is not supported", where)
			}
			inputs[in.Name] = in
		}
		declarations := make([]platform.Field, 0, len(a.Inputs))
		for _, in := range a.Inputs {
			declarations = append(declarations, in.Field())
		}
		if err := platform.CheckInputs(declarations); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if a.ToInput != "" {
			in := inputs[a.ToInput]
			if in.Type != "choice" || !in.Required || len(buildapi.ValueChoices(in.Choices)) == 0 {
				return fmt.Errorf("%s needs a required state choice input", where)
			}
			for _, state := range buildapi.ValueChoices(in.Choices) {
				if !states[state] {
					return fmt.Errorf("%s input chooses an undeclared state", where)
				}
			}
		}
		for _, set := range a.Sets {
			if err := checkActionSet(where, o, a, fields, inputs, set, lookup); err != nil {
				return err
			}
		}
		for _, br := range a.Branches {
			if br.To != "" && !states[br.To] {
				return fmt.Errorf("%s branch leaves records in %q, which is not a state of the object", where, br.To)
			}
			for _, w := range br.When {
				if err := checkActionCondition(o, a, w, lookup, false); err != nil {
					return err
				}
			}
			for _, set := range br.Sets {
				if err := checkActionSet(where, o, a, fields, inputs, set, lookup); err != nil {
					return err
				}
			}
		}
		for _, cond := range a.Conditions {
			if cond.When != nil {
				if cond.When.When != nil {
					return fmt.Errorf("nested condition guards are unsupported")
				}
				if err := checkActionCondition(o, a, *cond.When, lookup, false); err != nil {
					return err
				}
			}
			if err := checkActionCondition(o, a, cond, lookup, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkActionSet(where string, o Object, a Action, fields map[string]Field, inputs map[string]Input, set Set, lookup func(string) (platform.EntityInfo, bool)) error {
	f, ok := fields[set.Field]
	if !ok {
		return fmt.Errorf("%s sets %q, which is not a field of the object", where, set.Field)
	}
	switch {
	case set.From == "$me":
		if f.Type != "text" {
			return fmt.Errorf("%s sets %q to the person taking it, but it is a %s field", where, f.Name, f.Type)
		}
	case set.From == "$now":
		if f.Type != "date" && f.Type != "datetime" {
			return fmt.Errorf("%s sets %q to now, but it is a %s field", where, f.Name, f.Type)
		}
	case set.From == "=":
		return nil
	case strings.HasPrefix(set.From, "="):
		if f.Type == "tags" {
			return nil
		}
		if _, err := literal(f.Type, strings.TrimPrefix(set.From, "=")); err != nil {
			return fmt.Errorf("%s sets %q to %q: %v", where, f.Name, strings.TrimPrefix(set.From, "="), err)
		}
	case strings.HasPrefix(set.From, "append:"):
		if f.Type != "lines" && f.Type != "tags" {
			return fmt.Errorf("%s appends to %q, which is not a lines or tags field", where, f.Name)
		}
	case strings.HasPrefix(set.From, "formula:"):
		if f.Type != "integer" && f.Type != "decimal" {
			return fmt.Errorf("%s sets %q from a formula, which requires an integer or decimal field", where, f.Name)
		}
		if _, err := buildapi.ValueParseFormula(strings.TrimPrefix(set.From, "formula:")); err != nil {
			return fmt.Errorf("%s sets %q: %v", where, f.Name, err)
		}
	case strings.HasPrefix(set.From, "table:"):
		rest := strings.TrimPrefix(set.From, "table:")
		if tbName, colName, ok := strings.Cut(rest, "."); !ok || !named(tbName) || !named(colName) {
			return fmt.Errorf("%s sets %q from invalid table reference %q", where, f.Name, set.From)
		}
	case strings.HasPrefix(set.From, "step:"):
		if _, _, _, _, ok := parseStepExpr(set.From); !ok {
			return fmt.Errorf("%s sets %q from invalid step expression %q", where, f.Name, set.From)
		}
	case strings.HasPrefix(set.From, "rollup:"):
		if f.Type != "integer" && f.Type != "decimal" {
			return fmt.Errorf("%s sets %q from a rollup, which requires an integer or decimal field", where, f.Name)
		}
	case strings.HasPrefix(set.From, "record.") || strings.HasPrefix(set.From, "input.") && strings.Count(set.From, ".") >= 2:
		srcInfo, err := conditionField(o, a, set.From, lookup)
		if err != nil {
			return fmt.Errorf("%s sets %q from %q: %w", where, f.Name, set.From, err)
		}
		if srcInfo.Type != "" && !inputFits(srcInfo.Type, f.Type) && !(srcInfo.Type == "text" && f.Type == "reference") {
			return fmt.Errorf("%s sets %q (%s) from %q (%s), which does not fit", where, f.Name, f.Type, set.From, srcInfo.Type)
		}
	default:
		inName := strings.TrimPrefix(set.From, "input.")
		in := inputs[inName]
		if in.Name == "" {
			return fmt.Errorf("%s sets %q from %q, which is not one of its inputs, $me, $now or =<value>", where, f.Name, set.From)
		}
		if !inputFits(in.Type, f.Type) || in.Type == "reference" && in.Ref != f.Ref {
			return fmt.Errorf("%s sets %q (%s) from %q (%s), which does not fit", where, f.Name, f.Type, in.Name, in.Type)
		}
		if f.Type == "choice" {
			for _, option := range buildapi.ValueChoices(in.Choices) {
				if !slices.Contains(buildapi.ValueChoices(f.Choices), option) {
					return fmt.Errorf("%s sets %q from %q, whose value %q is not one of the field's choices", where, f.Name, in.Name, option)
				}
			}
		}
	}
	return nil
}

func checkActionCondition(o Object, a Action, cond Condition, lookup func(string) (platform.EntityInfo, bool), requireMessage bool) error {
	if cond.Quantifier != "" {
		if !slices.Contains([]string{"all", "none", "any", "count"}, cond.Quantifier) {
			return fmt.Errorf("unsupported condition quantifier %q", cond.Quantifier)
		}
		if strings.TrimSpace(cond.Collection) == "" {
			return fmt.Errorf("quantified condition needs a collection")
		}
		if cond.Quantifier == "count" {
			if !slices.Contains([]string{"=", "!=", "<", "<=", ">", ">="}, cond.Operator) {
				return fmt.Errorf("count condition needs a comparison operator")
			}
			if _, err := strconv.Atoi(cond.Value); err != nil {
				return fmt.Errorf("count condition value %q is not an integer", cond.Value)
			}
		}
		if requireMessage && strings.TrimSpace(cond.Message) == "" {
			return fmt.Errorf("the condition on %s says nothing to a person it stops", cond.Collection)
		}
		return nil
	}
	if strings.HasPrefix(cond.Field, "step:") {
		if _, _, _, _, ok := parseStepExpr(cond.Field); !ok {
			return fmt.Errorf("invalid step expression %q", cond.Field)
		}
		if !slices.Contains(Operators, cond.Operator) {
			return fmt.Errorf("unsupported condition operator %q", cond.Operator)
		}
		if requireMessage && strings.TrimSpace(cond.Message) == "" {
			return fmt.Errorf("the condition on %s says nothing to a person it stops", cond.Field)
		}
		return nil
	}
	left, err := conditionField(o, a, cond.Field, lookup)
	if err != nil {
		return fmt.Errorf("action condition: %w", err)
	}
	switch {
	case !slices.Contains(Operators, cond.Operator):
		return fmt.Errorf("unsupported condition operator %q", cond.Operator)
	case cond.Value != "" && cond.ValueField != "":
		return fmt.Errorf("use a value or a comparison field, not both")
	case cond.Operator != "empty" && cond.Operator != "not empty" && cond.Value == "" && cond.ValueField == "":
		return fmt.Errorf("%s %s compares with nothing", cond.Field, cond.Operator)
	case requireMessage && strings.TrimSpace(cond.Message) == "":
		return fmt.Errorf("the condition on %s says nothing to a person it stops", cond.Field)
	}
	if cond.ValueField != "" {
		if cond.Operator == "empty" || cond.Operator == "not empty" {
			return fmt.Errorf("an emptiness check needs no comparison field")
		}
		right, err := conditionField(o, a, cond.ValueField, lookup)
		if err != nil {
			return fmt.Errorf("action condition: %w", err)
		}
		if err := checkConditionFields(left, right, cond.Operator); err != nil {
			return fmt.Errorf("action condition: %w", err)
		}
	} else if err := checkCondition(left.Type, left.Choices, cond); err != nil {
		return fmt.Errorf("the condition on %s: %w", cond.Field, err)
	}
	return nil
}

// conditionField resolves a typed operand against this draft and installed
// related objects. It never infers a field from an arbitrary JSON expression.
func conditionField(o Object, a Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.FieldInfo, error) {
	entityLookup := func(typ string) (platform.EntityInfo, bool) {
		if typ != buildapi.TypeOf(o.Name) {
			if lookup == nil {
				return platform.EntityInfo{}, false
			}
			return lookup(typ)
		}
		info := platform.EntityInfo{Type: typ, Fields: []platform.FieldInfo{{Name: "id", Type: "text"}}}
		for _, f := range o.Fields {
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Choices: buildapi.ValueChoices(f.Choices)})
		}
		if len(o.States) > 0 {
			state := platform.FieldInfo{Name: "state", Type: "choice"}
			for _, s := range o.States {
				state.Choices = append(state.Choices, s.Name)
			}
			info.Fields = append(info.Fields, state)
		}
		return info, true
	}
	if input, ok := strings.CutPrefix(name, "input."); ok {
		inName, rest, hasPath := strings.Cut(input, ".")
		for _, in := range a.Inputs {
			if in.Name == inName {
				if !hasPath {
					return platform.FieldInfo{Name: name, Type: in.Type, Ref: in.Ref, Choices: buildapi.ValueChoices(in.Choices)}, nil
				}
				if in.Type != "reference" || in.Ref == "" {
					return platform.FieldInfo{}, fmt.Errorf("condition input %q is not a reference", inName)
				}
				if lookup == nil {
					return platform.FieldInfo{Name: name, Type: "text"}, nil
				}
				return platform.RecordPathField(in.Ref, strings.Split(rest, "."), entityLookup)
			}
		}
		return platform.FieldInfo{}, fmt.Errorf("condition input %q is not declared", inName)
	}
	clean := strings.TrimPrefix(name, "record.")
	if clean == "id" {
		return platform.FieldInfo{Name: "id", Type: "text"}, nil
	}
	return platform.RecordPathField(buildapi.TypeOf(o.Name), strings.Split(clean, "."), entityLookup)
}

func checkConditionFields(left, right platform.FieldInfo, op string) error {
	if left.Type == "money" || right.Type == "money" {
		return fmt.Errorf("money needs a currency-aware comparison rule")
	}
	if op == "contains all" || op == "contains any" || op == "in" || op == "not in" {
		return nil
	}
	numeric := func(kind string) bool { return kind == "integer" || kind == "decimal" }
	if left.Type != right.Type && !(numeric(left.Type) && numeric(right.Type)) || left.Type == "reference" && left.Ref != right.Ref {
		return fmt.Errorf("condition fields have incompatible types %s and %s", left.Type, right.Type)
	}
	if op != "=" && op != "!=" && !numeric(left.Type) && left.Type != "date" && left.Type != "datetime" {
		return fmt.Errorf("%s does not support ordered comparison", left.Type)
	}
	return nil
}

// inputFits is the publication-time contract for assigning an action input to
// an object field. In particular a string is not silently accepted as a date,
// number or reference merely because both travel through JSON.
func inputFits(input, field string) bool {
	switch field {
	case "text", "longtext":
		return input == "text" || input == "longtext" || input == "choice"
	case "decimal":
		return input == "decimal" || input == "integer"
	case "choice":
		return input == "choice" || input == "text"
	default:
		return input == field
	}
}

// checkCondition prevents a published rule from silently changing meaning
// through lexical comparison (for example an integer compared with "abc").
func checkCondition(kind string, options []string, c Condition) error {
	if c.Operator == "empty" || c.Operator == "not empty" {
		return nil
	}
	if kind == "money" {
		return fmt.Errorf("money needs a currency-aware comparison rule")
	}
	if c.Operator == "in" || c.Operator == "not in" || c.Operator == "contains all" || c.Operator == "contains any" {
		if c.Operator == "in" || c.Operator == "not in" {
			if kind == "choice" {
				for _, item := range buildapi.ValueChoices(c.Value) {
					if !slices.Contains(options, item) {
						return fmt.Errorf("%q is not one of this field's choices", item)
					}
				}
			}
		}
		return nil
	}
	if c.Value == "$me" {
		if kind != "text" && kind != "longtext" {
			return fmt.Errorf("$me only compares with a text field")
		}
		return nil
	}
	if c.Operator != "=" && c.Operator != "!=" && kind != "integer" && kind != "decimal" && kind != "date" && kind != "datetime" {
		return fmt.Errorf("%s does not support ordered comparison", kind)
	}
	if _, err := buildapi.ValueConditionValue(kind, c.Value); err != nil {
		return fmt.Errorf("%q does not fit %s: %w", c.Value, kind, err)
	}
	if kind == "choice" && !slices.Contains(options, c.Value) {
		return fmt.Errorf("%q is not one of this field's choices", c.Value)
	}
	return nil
}

// lifecycle is the object's states and actions as the platform's own lifecycle
// (ADR-0037 D1): each action a transition whose Do checks its conditions and
// sets its fields, inside the decision and again in replay.
func lifecycle(o Object, roles []string, creates creator, lookup func(string) (platform.EntityInfo, bool)) *platform.Lifecycle {
	if len(o.States) == 0 {
		return nil
	}
	l := &platform.Lifecycle{Field: "state", Initial: o.States[0].Name}
	for _, s := range o.States {
		l.States = append(l.States, platform.State{Name: s.Name, Title: s.Title, Tone: s.Tone, Description: s.Description})
	}
	for _, a := range o.Actions {
		to := a.To
		if to == "" {
			if a.New {
				to = o.States[0].Name
			} else if len(a.From) > 0 {
				to = a.From[0] // it stays where it was: Do keeps the status, and it must be among To
			}
		}
		reach := []string{to}
		if a.To == "" && !a.New {
			reach = slices.Clone(a.From)
		}
		for _, br := range a.Branches {
			if br.To != "" && !slices.Contains(reach, br.To) {
				reach = append(reach, br.To)
			}
		}
		if a.ToInput != "" {
			for _, in := range a.Inputs {
				if in.Name == a.ToInput {
					reach = buildapi.ValueChoices(in.Choices)
				}
			}
		}
		payload := []platform.Field{}
		for _, in := range a.Inputs {
			payload = append(payload, in.Field())
		}
		action := a
		takers := slices.Clone(roles)
		if len(a.Roles) > 0 { // the builder always tries what it builds
			takers = append([]string{Builder}, a.Roles...)
		}
		var approval *platform.Approval
		if a.Approval != nil {
			approval = &platform.Approval{Pending: a.Approval.Pending, Rejected: a.Approval.Rejected}
			for _, level := range a.Approval.Levels {
				approval.Levels = append(approval.Levels, platform.ApprovalLevel{Title: level.Title, AppRole: level.Role, All: level.All})
			}
		}
		l.Transitions = append(l.Transitions, platform.Transition{New: a.New, Name: a.Name, Title: a.Title, Description: a.Description,
			From: slices.Clone(a.From), To: reach, ToInput: a.ToInput, Roles: takers, Capability: o.Name, Payload: payload, Approval: approval, DeferRequired: true,
			Do: func(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
				return take(o, action, c, record, raw, now, creates, lookup)
			}, After: stored(o, action, creates)})
	}
	return l
}

// stored puts an action's related records under the change that took it, as
// Do built and checked them from the same inputs (ADR-0040 21c).
func stored(o Object, a Action, creates creator) func(platform.Caller, *pb.ChangeRecord, any, time.Time) {
	if len(a.Creates) == 0 && len(a.Updates) == 0 && len(a.Posts) == 0 || creates == nil {
		return nil
	}
	return func(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
		var inputs map[string]any
		_ = json.Unmarshal(r.GetSubmission().GetPayload(), &inputs)
		if inputs == nil {
			inputs = map[string]any{}
		}
		applyInputDefaults(o, a, c, inputs, record, creates, now)
		overlay := []any{record}
		for i, cr := range a.Creates {
			if values, err := creates.createBatch(c, o, a, cr, i, inputs, record, overlay, now); err == nil {
				for _, value := range values {
					c.Put(r, value)
					overlay = append(overlay, value)
				}
			}
		}
		for _, u := range a.Updates {
			if updated, ok, err := creates.updateRelated(c, o, a, u, inputs, record, overlay, now); err == nil && ok {
				c.Put(r, updated)
				overlay = append(overlay, updated)
			}
		}
		for _, p := range a.Posts {
			if value, err := creates.post(c, p, inputs, record, now); err == nil {
				c.Put(r, value)
			}
		}
	}
}

func applyInputDefaults(o Object, a Action, c platform.Caller, inputs map[string]any, record any, creates creator, now time.Time) {
	eval := (*Build)(nil).evalSource
	if creates != nil {
		eval = creates.evalSource
	}
	for _, in := range a.Inputs {
		if in.DefaultFrom == "" {
			continue
		}
		if cur, exists := inputs[in.Name]; exists && cur != nil && buildapi.ValueTextOf(cur) != "" {
			continue
		}
		val, present, err := eval(c, o, a, Field{Name: in.Name, Type: in.Type, Ref: in.Ref, Choices: in.Choices}, in.DefaultFrom, inputs, record, nil, 1, 1, nil, now)
		if err == nil && present {
			inputs[in.Name] = val
		}
	}
}

// take is one action on a record: its required inputs, its conditions, then
// the fields it sets. A refusal says what the builder wrote.
func take(o Object, a Action, c platform.Caller, record any, raw json.RawMessage, now time.Time, creates creator, lookup func(string) (platform.EntityInfo, bool)) *kernel.Error {
	var inputs map[string]any
	if len(raw) > 0 && json.Unmarshal(raw, &inputs) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} takes an object of inputs", a.Title)
	}
	if inputs == nil {
		inputs = map[string]any{}
	}
	applyInputDefaults(o, a, c, inputs, record, creates, now)
	if !c.Replaying {
		fields := make([]platform.Field, 0, len(a.Inputs))
		for _, in := range a.Inputs {
			fields = append(fields, in.Field())
		}
		if issues := platform.InputIssues(fields, inputs); len(issues) > 0 {
			refusal := c.RefuseFields(issues)
			if required := slices.IndexFunc(issues, func(issue platform.FieldIssue) bool { return issue.Code == "required" && len(issue.Path) > 0 }); required >= 0 {
				name := issues[required].Path[0]
				label := name
				if input := slices.IndexFunc(a.Inputs, func(in Input) bool { return in.Name == name }); input >= 0 && a.Inputs[input].Title != "" {
					label = a.Inputs[input].Title
				}
				refusal.Message = fmt.Sprintf("%s needs %s", a.Title, label)
			}
			return refusal
		}
	}
	b, _ := creates.(*Build)
	v := reflect.ValueOf(record).Elem()
	// Action-level organizational unit path check (ADR-0098 D2).
	if a.UnitField != "" && !c.Replaying && !c.Automation && !c.Holds(c.App, Builder) {
		unitVal, refusal := operandValue(c, b, o, a, a.UnitField, inputs, record, nil, lookup, now)
		if refusal != nil {
			return refusal
		}
		unit := buildapi.ValueTextOf(unitVal)
		structure := "site"
		if o.Scope != nil && o.Scope.Structure != "" {
			structure = o.Scope.Structure
		}
		if unit == "" || !slices.Contains(c.Units(structure, now), unit) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} is not assigned to the unit for {action}", c.ID, a.Title)
		}
	}
	// Check input candidate filters on reference parameters (ADR-0098 D3).
	if !c.Replaying && b != nil && b.host != nil {
		for _, in := range a.Inputs {
			if in.Type != "reference" || len(in.Filter) == 0 {
				continue
			}
			refID := buildapi.ValueTextOf(inputs[in.Name])
			if refID == "" {
				continue
			}
			for _, fCond := range in.Filter {
				checkCond := fCond
				if !strings.HasPrefix(checkCond.Field, "input.") && !strings.HasPrefix(checkCond.Field, "record.") {
					checkCond.Field = "input." + in.Name + "." + strings.TrimPrefix(checkCond.Field, "item.")
				}
				held, refusal := evaluateConditionOnRecord(c, b, o, a, checkCond, inputs, record, nil, lookup, now)
				if refusal != nil {
					return refusal
				}
				if !held {
					msg := fCond.Message
					if msg == "" {
						msg = fmt.Sprintf("%s does not satisfy %s filter", refID, in.Title)
					}
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", msg)
				}
			}
		}
	}
	for _, cond := range a.Conditions {
		held, err := evaluateConditionOnRecord(c, b, o, a, cond, inputs, record, []any{record}, lookup, now)
		if err != nil {
			return err
		}
		if !held {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{message}", cond.Message)
		}
	}
	if a.ToInput != "" {
		state, ok := inputs[a.ToInput].(string)
		allowed := false
		for _, in := range a.Inputs {
			if in.Name == a.ToInput && slices.Contains(buildapi.ValueChoices(in.Choices), state) {
				allowed = true
			}
		}
		if !ok || !allowed {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The selected state is unavailable")
		}
		v.FieldByName("State").SetString(state)
	}

	fieldMap := map[string]Field{}
	for _, f := range o.Fields {
		fieldMap[f.Name] = f
	}
	eval := (*Build)(nil).evalSource
	if creates != nil {
		eval = creates.evalSource
	}
	applySets := func(sets []Set, overlay []any) *kernel.Error {
		for _, set := range sets {
			targetField := fieldMap[set.Field]
			x, present, refusal := eval(c, o, a, targetField, set.From, inputs, record, overlay, 1, 1, nil, now)
			if refusal != nil {
				return refusal
			}
			if !present {
				continue // an input left out leaves the field as it was
			}
			if err := assign(v.FieldByName(goName(set.Field)), x); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} cannot set {field}: {why}", a.Title, set.Field, err.Error())
			}
		}
		return nil
	}
	if err := applySets(a.Sets, []any{record}); err != nil {
		return err
	}
	// Validate and stage related record creations so branches and parent updates
	// can observe newly created child records in the same decision.
	overlay := []any{record}
	for i, cr := range a.Creates {
		if creates == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot create records here", a.Title)
		}
		created, err := creates.createBatch(c, o, a, cr, i, inputs, record, overlay, now)
		if err != nil {
			return err
		}
		overlay = append(overlay, created...)
	}
	// Evaluate conditional branches (first matching branch wins, ADR-0098 D2).
	branched := false
	for _, br := range a.Branches {
		matched := true
		for _, w := range br.When {
			held, err := evaluateConditionOnRecord(c, b, o, a, w, inputs, record, overlay, lookup, now)
			if err != nil {
				return err
			}
			if !held {
				matched = false
				break
			}
		}
		if matched {
			branched = true
			if err := applySets(br.Sets, overlay); err != nil {
				return err
			}
			if br.To != "" {
				v.FieldByName("State").SetString(br.To)
			}
			break
		}
	}
	if !branched && a.To != "" && a.ToInput == "" {
		v.FieldByName("State").SetString(a.To)
	}
	// Validate related parent/target record updates with the final record state overlaid.
	for _, u := range a.Updates {
		if creates == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot update related records here", a.Title)
		}
		updated, ok, err := creates.updateRelated(c, o, a, u, inputs, record, overlay, now)
		if err != nil {
			return err
		}
		if ok {
			overlay = append(overlay, updated)
		}
	}
	for _, p := range o.EffectivePosts(a) {
		if creates == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot post balances here", a.Title)
		}
		if _, err := creates.post(c, p, inputs, v.Addr().Interface(), now); err != nil {
			return err
		}
	}
	return nil
}

// literal is a fixed value a builder wrote, as the field's type holds it.
func literal(kind, s string) (any, error) {
	switch kind {
	case "integer":
		return strconv.Atoi(s)
	case "decimal":
		return strconv.ParseFloat(s, 64)
	case "boolean":
		return strconv.ParseBool(s)
	case "date":
		_, err := time.Parse(time.DateOnly, s)
		return s, err
	case "datetime":
		return time.Parse(time.RFC3339, s)
	case "money":
		return nil, fmt.Errorf("an amount is not set to a fixed value")
	}
	return s, nil
}

// assign sets a record's field from a value decoded from JSON or built above.
func assign(f reflect.Value, x any) error {
	if !f.IsValid() {
		return fmt.Errorf("no such field")
	}
	raw, err := json.Marshal(x)
	if err != nil {
		return err
	}
	target := reflect.New(f.Type())
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return fmt.Errorf("%v does not fit a %s", x, f.Type())
	}
	f.Set(target.Elem())
	return nil
}

var readLevels = map[string]string{"all": platform.ScopeTenant, "below": platform.ScopeBelow, "unit": platform.ScopeUnit, "own": platform.ScopeOwn, "none": platform.ScopeNone}

// checkScope: the scope fields exist and suit their use, and every read level
// that needs one has it.
func checkScope(o Object) error {
	field := func(name string) *Field {
		if i := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == name }); i >= 0 {
			return &o.Fields[i]
		}
		return nil
	}
	sc := o.Scope
	if sc == nil {
		sc = &ObjectScope{}
	}
	if sc.Owner != "" {
		if f := field(sc.Owner); f == nil || f.Type != "text" && f.Type != "reference" {
			return fmt.Errorf("the owner field %q is not a text or reference field of this object", sc.Owner)
		}
	}
	if sc.Unit != "" {
		if f := field(sc.Unit); f == nil || f.Type != "text" && f.Type != "reference" {
			return fmt.Errorf("the unit field %q is not a text or reference field of this object", sc.Unit)
		}
	}
	for _, a := range o.Access {
		if (a.Read == "unit" || a.Read == "below") && (sc.Unit == "" || sc.Structure == "") {
			return fmt.Errorf("the role %q reads by unit, so the object needs a unit field and a structure under Scope", a.Role)
		}
	}
	return nil
}

// checkAccess refuses access people could not be given: a role that is not a
// name, one listed twice, one that writes what it may not read, and actions or
// fields naming roles the object does not declare.
func checkAccess(o Object) error {
	roles := map[string]bool{Builder: true}
	for _, a := range o.Access {
		switch {
		case !named(a.Role):
			return fmt.Errorf("the role %q is not lower-case letters and digits", a.Role)
		case a.Role == Builder:
			return fmt.Errorf("the role %q always does everything with what it builds", Builder)
		case roles[a.Role]:
			return fmt.Errorf("the role %q is given access twice", a.Role)
		case readLevels[a.Read] == "":
			return fmt.Errorf("the role %q reads %q; it reads all, below, unit, own or none", a.Role, a.Read)
		case a.Read == "none" && (a.Create || a.Edit || a.Archive):
			return fmt.Errorf("the role %q may not read the object, so it may not create, edit or archive it either", a.Role)
		}
		roles[a.Role] = true
	}
	known := func(role string) bool { return roles[role] || len(o.Access) == 0 && role == User }
	for _, act := range o.Actions {
		for _, r := range act.Roles {
			if !known(r) {
				return fmt.Errorf("the action %q is for %q, which is not a role of this object", act.Name, r)
			}
		}
	}
	for _, f := range o.Fields {
		for _, r := range append(slices.Clone(f.Read), f.Write...) {
			if !known(r) {
				return fmt.Errorf("the field %q names %q, which is not a role of this object", f.Name, r)
			}
		}
		if len(f.Read) > 0 {
			for _, r := range f.Write {
				if !slices.Contains(f.Read, r) {
					return fmt.Errorf("the field %q is set by %q, which may not read it", f.Name, r)
				}
			}
		}
	}
	return nil
}

// access is the object's access as the platform's own declarations: its
// generated actions' roles per verb, and its scope (ADR-0037 D4, D5). With no
// access declared an object keeps the rule of 15a: builder and user do everything.
func access(o Object) (platform.Standard, platform.Scope, []string) {
	std := platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, User}, Capability: o.Name}
	if len(o.Access) == 0 {
		return std, platform.Scope{}, []string{Builder, User}
	}
	std.Roles = []string{Builder}
	std.CreateRoles, std.EditRoles, std.ArchiveRoles = []string{Builder}, []string{Builder}, []string{Builder}
	scope := platform.Scope{Owner: platform.OwnerCreated, Levels: map[string]string{}, Default: platform.ScopeNone}
	if o.Scope != nil {
		if o.Scope.Owner != "" {
			scope.Owner = o.Scope.Owner
		}
		scope.Unit, scope.Structure = o.Scope.Unit, o.Scope.Structure
	}
	scope.Levels[Builder] = platform.ScopeTenant
	roles := []string{Builder}
	for _, a := range o.Access {
		roles = append(roles, a.Role)
		scope.Levels[a.Role] = readLevels[a.Read]
		if a.Create {
			std.CreateRoles = append(std.CreateRoles, a.Role)
		}
		if a.Edit {
			std.EditRoles = append(std.EditRoles, a.Role)
		}
		if a.Archive {
			std.ArchiveRoles = append(std.ArchiveRoles, a.Role)
		}
	}
	return std, scope, roles
}
