package build

import (
	"encoding/json"
	"fmt"
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
	To          string      `json:"to,omitempty" title:"Leaves it in" help:"The state it leaves the record in; empty: where it was"`
	Inputs      []Input     `json:"inputs,omitempty" title:"What people give"`
	Sets        []Set       `json:"sets,omitempty" title:"What it sets"`
	Conditions  []Condition `json:"conditions,omitempty" title:"What it needs"`
}

// Input is one value a person gives when taking an action.
type Input struct {
	Name     string `json:"name" field:"required" example:"to"`
	Title    string `json:"title" field:"required" title:"Label" example:"Handed to"`
	Type     string `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,boolean,choice"`
	Choices  string `json:"choices,omitempty" help:"For a choice: the values, comma-separated"`
	Required bool   `json:"required,omitempty"`
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
	Field    string `json:"field" field:"required" help:"A field of the record, or input.<name>"`
	Operator string `json:"operator" field:"required" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value    string `json:"value,omitempty" help:"What it is compared with; $me is the person taking the action"`
	Message  string `json:"message" field:"required" help:"What a person reads when it does not hold"`
}

// Operators are the comparisons a condition may make.
var Operators = []string{"=", "!=", "<", "<=", ">", ">=", "empty", "not empty"}

var inputTypes = map[string]string{"text": "string", "longtext": "string", "integer": "integer", "decimal": "number", "date": "date", "boolean": "boolean", "choice": "string"}

// checkProcess refuses states and actions people could not take: names that are
// not names, an action from or to a state the object has not, an input or a
// field it does not have, a condition that compares nothing.
func checkProcess(o Object) error {
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
		case a.To != "" && !states[a.To]:
			return fmt.Errorf("%s leaves records in %q, which is not a state of the object", where, a.To)
		}
		seen[a.Name] = true
		for _, s := range a.From {
			if !states[s] {
				return fmt.Errorf("%s is taken from %q, which is not a state of the object", where, s)
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
			inputs[in.Name] = in
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
				if inputs[set.From].Name == "" {
					return fmt.Errorf("%s sets %q from %q, which is not one of its inputs, $me, $now or =<value>", where, f.Name, set.From)
				}
			}
		}
		for _, cond := range a.Conditions {
			name, isInput := strings.CutPrefix(cond.Field, "input.")
			known := isInput && inputs[name].Name != "" || !isInput && (fields[cond.Field].Name != "" || cond.Field == "state" && len(o.States) > 0)
			switch {
			case !known:
				return fmt.Errorf("%s needs %q, which is not a field of the object or one of its inputs", where, cond.Field)
			case !slices.Contains(Operators, cond.Operator):
				return fmt.Errorf("%s: %q is not one of %s", where, cond.Operator, strings.Join(Operators, ", "))
			case cond.Operator != "empty" && cond.Operator != "not empty" && cond.Value == "":
				return fmt.Errorf("%s: %s %s compares with nothing", where, cond.Field, cond.Operator)
			case strings.TrimSpace(cond.Message) == "":
				return fmt.Errorf("%s: the condition on %s says nothing to a person it stops", where, cond.Field)
			}
		}
	}
	return nil
}

// lifecycle is the object's states and actions as the platform's own lifecycle
// (ADR-0037 D1): each action a transition whose Do checks its conditions and
// sets its fields, inside the decision and again in replay.
func lifecycle(o Object, roles []string) *platform.Lifecycle {
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
		payload := []platform.Field{}
		for _, in := range a.Inputs {
			payload = append(payload, platform.Field{Name: in.Name, Type: inputTypes[in.Type], Required: in.Required, Description: in.Title, Choices: choices(in.Choices)})
		}
		action := a
		l.Transitions = append(l.Transitions, platform.Transition{Name: a.Name, Title: a.Title, Description: a.Description,
			From: slices.Clone(a.From), To: reach, Roles: slices.Clone(roles), Capability: o.Name, Payload: payload,
			Do: func(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
				return take(o, action, c, record, raw, now)
			}})
	}
	return l
}

// take is one action on a record: its required inputs, its conditions, then
// the fields it sets. A refusal says what the builder wrote.
func take(o Object, a Action, c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
	var inputs map[string]any
	if len(raw) > 0 && json.Unmarshal(raw, &inputs) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} takes an object of inputs", a.Title)
	}
	for _, in := range a.Inputs {
		if v, ok := inputs[in.Name]; in.Required && (!ok || v == nil || v == "") {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} needs {input}", a.Title, in.Title)
		}
	}
	v := reflect.ValueOf(record).Elem()
	value := func(name string) any {
		if in, ok := strings.CutPrefix(name, "input."); ok {
			return inputs[in]
		}
		f := v.FieldByName(goName(name))
		if !f.IsValid() {
			return nil
		}
		return f.Interface()
	}
	for _, cond := range a.Conditions {
		if !holds(value(cond.Field), cond.Operator, cond.Value, c.ID) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{message}", cond.Message)
		}
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
	return nil
}

// holds compares a value with what a condition names. Numbers compare as
// numbers, everything else as its text; $me is the person taking the action.
func holds(got any, op, want, me string) bool {
	if want == "$me" {
		want = me
	}
	text := textOf(got)
	switch op {
	case "empty":
		return text == ""
	case "not empty":
		return text != ""
	}
	a, errA := strconv.ParseFloat(text, 64)
	b, errB := strconv.ParseFloat(want, 64)
	numeric := errA == nil && errB == nil
	cmp := strings.Compare(text, want)
	if numeric {
		cmp = map[bool]int{true: -1, false: 0}[a < b]
		if a > b {
			cmp = 1
		}
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
