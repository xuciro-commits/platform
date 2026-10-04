package build

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// States and actions a tenant gives its own objects (ADR-0037 D1–D3). An action
// is typed data compiled into the platform's own lifecycle transition: the
// ledger routes it, replay runs it again, and every surface that offers a coded
// transition offers it. Nothing here is code the tenant supplies.

// State is one state a defined object's records are in.
type State struct {
	Name        string `json:"name" field:"required" help:"Lower-case letters and digits" example:"found"`
	Title       string `json:"title" field:"required" title:"What people call it" example:"Found"`
	Tone        string `json:"tone,omitempty" choices:"info,success,warning,danger,neutral"`
	Description string `json:"description,omitempty" help:"What a record in this state means"`
}

// Action is a step people take on one of its records.
type Action struct {
	Name        string      `json:"name" field:"required" help:"Lower-case letters and digits" example:"return"`
	Title       string      `json:"title" field:"required" title:"What people call it" example:"Hand it back"`
	Description string      `json:"description,omitempty" help:"What it does, for people and agents"`
	From        []string    `json:"from" title:"Taken from" help:"The states it takes a record from"`
	ToInput     string      `json:"toInput,omitempty" title:"State from input"`
	To          string      `json:"to,omitempty" title:"Leaves it in" help:"The state it leaves the record in; empty: where it was"`
	Inputs      []Input     `json:"inputs,omitempty" title:"What people give"`
	Sets        []Set       `json:"sets,omitempty" title:"What it sets"`
	Conditions  []Condition `json:"conditions,omitempty" title:"What it needs"`
	// Roles are who may take it (ADR-0037 18b); empty: every role of the object.
	Roles    []string        `json:"roles,omitempty" title:"Taken by"`
	Approval *ActionApproval `json:"approval,omitempty" title:"Approval"`
	// Creates are related records it makes in the same decision (ADR-0040 21c
	// D2): all of them and the record's change commit together, or none does.
	Creates []Create `json:"creates,omitempty" title:"What it creates"`
}

// Create is a record of another defined object made when an action is taken:
// Via is that object's reference field back to this record.
type Create struct {
	Object string `json:"object" field:"required" help:"A defined object of this builder whose reference points at this one" example:"build.followup"`
	Via    string `json:"via" field:"required" help:"That object's reference field to this record" example:"visit"`
	Sets   []Set  `json:"sets,omitempty" title:"What it sets"`
}

// creator makes one related record inside the decision being taken; nil where
// no host is attached (descriptors, validation), which refuses a Create.
type creator func(c platform.Caller, cr Create, inputs map[string]any, parentType, parent, id string, now time.Time) (any, *kernel.Error)

// ActionApproval uses the work app's approval chain for a tenant action.
type ActionApproval struct {
	Pending  string          `json:"pending" title:"While it waits"`
	Rejected string          `json:"rejected,omitempty" title:"If rejected"`
	Levels   []ApproverLevel `json:"levels" title:"Approver levels"`
}

// ApproverLevel is a role of the builder app, resolved when the request opens.
type ApproverLevel struct {
	Title string `json:"title" title:"What this level is called"`
	Role  string `json:"role" title:"Approver role"`
	All   bool   `json:"all,omitempty" title:"Everyone in the role must approve"`
}

// Input is one value a person gives when taking an action.
type Input struct {
	Ref       string `json:"ref,omitempty" title:"Reference object"`
	MinLength *int   `json:"minLength,omitempty" title:"Minimum length"`
	Name      string `json:"name" field:"required" example:"to"`
	Title     string `json:"title" field:"required" title:"Label" example:"Handed to"`
	Type      string `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,boolean,choice,reference"`
	Choices   string `json:"choices,omitempty" help:"For a choice: the values, comma-separated"`
	Required  bool   `json:"required,omitempty"`
}

// Set is a field of the record an action sets, from an input, a fixed value,
// the person taking it ($me) or the moment it is taken ($now).
type Set struct {
	Field string `json:"field" field:"required" help:"A field of the object"`
	From  string `json:"from" field:"required" help:"An input's name, $me, $now, or =<a fixed value>" example:"to"`
}

// Condition is what must hold for the action to be taken, and what a person
// reads when it does not (Foundry's submission criteria, as data).
type Condition struct {
	When       *Condition `json:"when,omitempty" title:"Only when"`
	Field      string     `json:"field" field:"required" help:"A declared record path through references, or input.<name>"`
	Operator   string     `json:"operator" field:"required" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value      string     `json:"value,omitempty" help:"What it is compared with; $me is the person taking the action"`
	ValueField string     `json:"valueField,omitempty" help:"Compare with another declared record path or input; excludes value"`
	Message    string     `json:"message" field:"required" help:"What a person reads when it does not hold"`
}

// Operators are the comparisons a condition may make.
var Operators = []string{"=", "!=", "<", "<=", ">", ">=", "empty", "not empty"}

var inputTypes = map[string]string{"text": "string", "longtext": "string", "integer": "integer", "decimal": "number", "date": "date", "boolean": "boolean", "choice": "string", "reference": "string"}

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
		case len(a.From) == 0:
			return fmt.Errorf("%s says no state it is taken from", where)
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
		if a.To == "" && len(a.From) == 0 {
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
			case in.Type == "choice" && len(choices(in.Choices)) == 0:
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
		if a.ToInput != "" {
			in := inputs[a.ToInput]
			if in.Type != "choice" || !in.Required || len(choices(in.Choices)) == 0 {
				return fmt.Errorf("%s needs a required state choice input", where)
			}
			for _, state := range choices(in.Choices) {
				if !states[state] {
					return fmt.Errorf("%s input chooses an undeclared state", where)
				}
			}
		}
		for _, set := range a.Sets {
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
			case strings.HasPrefix(set.From, "="):
				if _, err := literal(f.Type, strings.TrimPrefix(set.From, "=")); err != nil {
					return fmt.Errorf("%s sets %q to %q: %v", where, f.Name, strings.TrimPrefix(set.From, "="), err)
				}
			default:
				in := inputs[set.From]
				if in.Name == "" {
					return fmt.Errorf("%s sets %q from %q, which is not one of its inputs, $me, $now or =<value>", where, f.Name, set.From)
				}
				if !inputFits(in.Type, f.Type) || in.Type == "reference" && in.Ref != f.Ref {
					return fmt.Errorf("%s sets %q (%s) from %q (%s), which does not fit", where, f.Name, f.Type, in.Name, in.Type)
				}
				if f.Type == "choice" {
					for _, option := range choices(in.Choices) {
						if !slices.Contains(choices(f.Choices), option) {
							return fmt.Errorf("%s sets %q from %q, whose value %q is not one of the field's choices", where, f.Name, in.Name, option)
						}
					}
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

func checkActionCondition(o Object, a Action, cond Condition, lookup func(string) (platform.EntityInfo, bool), requireMessage bool) error {
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
	if input, ok := strings.CutPrefix(name, "input."); ok {
		for _, in := range a.Inputs {
			if in.Name == input {
				return platform.FieldInfo{Name: name, Type: in.Type, Ref: in.Ref, Choices: choices(in.Choices)}, nil
			}
		}
		return platform.FieldInfo{}, fmt.Errorf("condition input %q is not declared", input)
	}
	return platform.RecordPathField(TypeOf(o.Name), strings.Split(name, "."), func(typ string) (platform.EntityInfo, bool) {
		if typ != TypeOf(o.Name) {
			if lookup == nil {
				return platform.EntityInfo{}, false
			}
			return lookup(typ)
		}
		info := platform.EntityInfo{Type: typ}
		for _, f := range o.Fields {
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Choices: choices(f.Choices)})
		}
		if len(o.States) > 0 {
			state := platform.FieldInfo{Name: "state", Type: "choice"}
			for _, s := range o.States {
				state.Choices = append(state.Choices, s.Name)
			}
			info.Fields = append(info.Fields, state)
		}
		return info, true
	})
}

func checkConditionFields(left, right platform.FieldInfo, op string) error {
	if left.Type == "money" || right.Type == "money" {
		return fmt.Errorf("money needs a currency-aware comparison rule")
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
		return input == "choice"
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
	if c.Value == "$me" {
		if kind != "text" && kind != "longtext" {
			return fmt.Errorf("$me only compares with a text field")
		}
		return nil
	}
	if c.Operator != "=" && c.Operator != "!=" && kind != "integer" && kind != "decimal" && kind != "date" && kind != "datetime" {
		return fmt.Errorf("%s does not support ordered comparison", kind)
	}
	if _, err := conditionValue(kind, c.Value); err != nil {
		return fmt.Errorf("%q does not fit %s: %w", c.Value, kind, err)
	}
	if kind == "choice" && !slices.Contains(options, c.Value) {
		return fmt.Errorf("%q is not one of this field's choices", c.Value)
	}
	return nil
}

func conditionValue(kind, raw string) (any, error) {
	switch kind {
	case "integer":
		return strconv.ParseInt(raw, 10, 64)
	case "decimal":
		x, err := strconv.ParseFloat(raw, 64)
		if err == nil && (math.IsInf(x, 0) || math.IsNaN(x)) {
			err = fmt.Errorf("a finite number is required")
		}
		return x, err
	case "date":
		if day, err := time.Parse(time.DateOnly, raw); err == nil {
			return day, nil
		}
		// Record fields are decoded as times; authored literals are dates.
		instant, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, err
		}
		return time.Parse(time.DateOnly, instant.UTC().Format(time.DateOnly))
	case "datetime":
		return time.Parse(time.RFC3339, raw)
	case "boolean":
		return strconv.ParseBool(raw)
	default:
		return raw, nil
	}
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
			to = a.From[0] // it stays where it was: Do keeps the status, and it must be among To
		}
		reach := []string{to}
		if a.To == "" {
			reach = slices.Clone(a.From)
		}
		if a.ToInput != "" {
			for _, in := range a.Inputs {
				if in.Name == a.ToInput {
					reach = choices(in.Choices)
				}
			}
		}
		payload := []platform.Field{}
		for _, in := range a.Inputs {
			payload = append(payload, platform.Field{Name: in.Name, Type: inputTypes[in.Type], Ref: in.Ref, Required: in.Required, Description: in.Title, Choices: choices(in.Choices)})
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
		l.Transitions = append(l.Transitions, platform.Transition{Name: a.Name, Title: a.Title, Description: a.Description,
			From: slices.Clone(a.From), To: reach, ToInput: a.ToInput, Roles: takers, Capability: o.Name, Payload: payload, Approval: approval,
			Do: func(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
				return take(o, action, c, record, raw, now, creates, lookup)
			}, After: stored(o, action, creates)})
	}
	return l
}

// stored puts an action's related records under the change that took it, as
// Do built and checked them from the same inputs (ADR-0040 21c).
func stored(o Object, a Action, creates creator) func(platform.Caller, *pb.ChangeRecord, any, time.Time) {
	if len(a.Creates) == 0 || creates == nil {
		return nil
	}
	return func(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
		var inputs map[string]any
		_ = json.Unmarshal(r.GetSubmission().GetPayload(), &inputs)
		rec := reflect.ValueOf(record).Elem().Field(0).Interface().(platform.Record)
		for i, cr := range a.Creates {
			if value, err := creates(c, cr, inputs, TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err == nil {
				c.Put(r, value)
			}
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
	for _, in := range a.Inputs {
		if v, ok := inputs[in.Name]; in.Required && (!ok || v == nil || v == "") {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} needs {input}", a.Title, in.Title)
		}
		if in.MinLength != nil {
			if text, ok := inputs[in.Name].(string); ok && text != "" {
				units := 0
				for _, r := range text {
					units += utf16.RuneLen(r)
				}
				if units < *in.MinLength {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{input} is shorter than its required minimum", in.Title)
				}
			}
		}

	}
	v := reflect.ValueOf(record).Elem()
	value := func(name string) (any, *kernel.Error) {
		if in, ok := strings.CutPrefix(name, "input."); ok {
			return inputs[in], nil
		}
		if strings.Contains(name, ".") {
			rec := v.Field(0).Interface().(platform.Record)
			raw, _, refusal := c.ReadRecordPath(TypeOf(o.Name), rec.ID, strings.Split(name, "."), now)
			if refusal != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A related condition source is unavailable / 关联条件来源不可读")
			}
			var result any
			if json.Unmarshal(raw, &result) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid condition source")
			}
			return result, nil
		}
		f := v.FieldByName(goName(name))
		if !f.IsValid() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Condition field is unavailable")
		}
		return f.Interface(), nil
	}
	evaluate := func(cond Condition) (bool, *kernel.Error) {
		field, err := conditionField(o, a, cond.Field, lookup)
		if err != nil {
			return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Condition field is unavailable")
		}
		got, refusal := value(cond.Field)
		if refusal != nil {
			return false, refusal
		}
		kind, want := field.Type, cond.Value
		if cond.ValueField != "" {
			right, err := conditionField(o, a, cond.ValueField, lookup)
			if err != nil || checkConditionFields(field, right, cond.Operator) != nil {
				return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Condition comparison field is unavailable or incompatible")
			}
			other, refusal := value(cond.ValueField)
			if refusal != nil {
				return false, refusal
			}
			want = textOf(other)
			if want == "" {
				return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{message}", cond.Message)
			}
			if right.Type == "decimal" {
				kind = "decimal"
			}
		} else if want == "$me" {
			want = c.ID
		}
		return holds(got, kind, cond.Operator, want), nil
	}
	for _, cond := range a.Conditions {
		if cond.When != nil {
			held, err := evaluate(*cond.When)
			if err != nil {
				return err
			}
			if !held {
				continue
			}
		}
		held, err := evaluate(cond)
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
			if in.Name == a.ToInput && slices.Contains(choices(in.Choices), state) {
				allowed = true
			}
		}
		if !ok || !allowed {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The selected state is unavailable")
		}
		v.FieldByName("State").SetString(state)
	}

	types := map[string]string{}
	for _, f := range o.Fields {
		types[f.Name] = f.Type
	}
	for _, set := range a.Sets {
		var x any
		switch {
		case set.From == "$me":
			x = c.ID
		case set.From == "$now":
			if types[set.Field] == "date" {
				x = now.UTC().Format(time.DateOnly)
			} else {
				x = now.UTC()
			}
		case strings.HasPrefix(set.From, "="):
			x, _ = literal(types[set.Field], strings.TrimPrefix(set.From, "="))
		default:
			given, ok := inputs[set.From]
			if !ok || given == nil {
				continue // an input left out leaves the field as it was
			}
			x = given
		}
		if err := assign(v.FieldByName(goName(set.Field)), x); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} cannot set {field}: {why}", a.Title, set.Field, err.Error())
		}
	}
	// Related records last: conditions and sets have already held. Each is
	// built and checked now (its required fields and create roles apply); the
	// transition's After stores it under the same change.
	rec := v.Field(0).Interface().(platform.Record)
	for i, cr := range a.Creates {
		if creates == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot create records here", a.Title)
		}
		if _, err := creates(c, cr, inputs, TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err != nil {
			return err
		}
	}
	return nil
}

// holds compares typed values. An unset or malformed value fails closed; it
// never falls back to lexical order when a numeric or temporal parse fails.
func holds(got any, kind, op, want string) bool {
	text := textOf(got)
	switch op {
	case "empty":
		return text == ""
	case "not empty":
		return text != ""
	}
	if text == "" {
		return false
	}
	a, errA := conditionValue(kind, text)
	b, errB := conditionValue(kind, want)
	if errA != nil || errB != nil {
		return false
	}
	cmp := 0
	switch x := a.(type) {
	case int64:
		y := b.(int64)
		cmp = map[bool]int{true: -1, false: 0}[x < y]
		if x > y {
			cmp = 1
		}
	case float64:
		y := b.(float64)
		cmp = map[bool]int{true: -1, false: 0}[x < y]
		if x > y {
			cmp = 1
		}
	case time.Time:
		y := b.(time.Time)
		cmp = map[bool]int{true: -1, false: 0}[x.Before(y)]
		if x.After(y) {
			cmp = 1
		}
	case bool:
		if x != b.(bool) {
			cmp = 1
		}
	case string:
		cmp = strings.Compare(x, b.(string))
	}
	switch op {
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	}
	return false
}

// textOf is a value as a condition reads it: money by its amount, a time by its day.
func textOf(v any) string {
	if v != nil {
		rv := reflect.ValueOf(v)
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return ""
			}
			rv = rv.Elem()
		}
		v = rv.Interface()
	}
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return x.UTC().Format(time.RFC3339)
	case platform.Money:
		if x.Currency == "" && x.Amount == 0 {
			return ""
		}
		return strconv.FormatInt(x.Amount, 10)
	}
	return fmt.Sprint(v)
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

// Access is what one role of the builder app may do with a defined object
// (ADR-0037 D4): which records it reads, and which of create, edit and
// archive it takes. The roles an object names become roles of `build`, which
// the Console assigns. Actions and fields name their own roles.
type Access struct {
	Role    string `json:"role" field:"required" help:"A role of the builder app, lower-case letters and digits" example:"desk"`
	Read    string `json:"read" field:"required" choices:"all,own,none" help:"all: every record; own: those they created; none: not the object at all"`
	Create  bool   `json:"create,omitempty"`
	Edit    bool   `json:"edit,omitempty"`
	Archive bool   `json:"archive,omitempty"`
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
		case !slices.Contains([]string{"all", "own", "none"}, a.Read):
			return fmt.Errorf("the role %q reads %q; it reads all, own or none", a.Role, a.Read)
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
	scope.Levels[Builder] = platform.ScopeTenant
	roles := []string{Builder}
	for _, a := range o.Access {
		roles = append(roles, a.Role)
		scope.Levels[a.Role] = map[string]string{"all": platform.ScopeTenant, "own": platform.ScopeOwn, "none": platform.ScopeNone}[a.Read]
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
