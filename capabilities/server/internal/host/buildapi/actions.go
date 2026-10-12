package buildapi

import (
	"fmt"
	"math"
	"reflect"
	"slices"

	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

// State is one state a defined object's records are in.
type State struct {
	Name        string `json:"name" field:"required" help:"Lower-case letters and digits" example:"found"`
	Title       string `json:"title" field:"required" title:"What people call it" example:"Found"`
	Tone        string `json:"tone,omitempty" choices:"info,success,warning,danger,neutral"`
	Description string `json:"description,omitempty" help:"What a record in this state means"`
}

// Action is a step people take on one of its records, or a governed creation step when New is true (ADR-0098).
type Action struct {
	New         bool           `json:"new,omitempty" title:"Creates a new record"`
	Name        string         `json:"name" field:"required" help:"Lower-case letters and digits" example:"return"`
	Title       string         `json:"title" field:"required" title:"What people call it" example:"Hand it back"`
	Description string         `json:"description,omitempty" help:"What it does, for people and agents"`
	From        []string       `json:"from,omitempty" title:"Taken from" help:"The states it takes a record from"`
	ToInput     string         `json:"toInput,omitempty" title:"State from input"`
	To          string         `json:"to,omitempty" title:"Leaves it in" help:"The state it leaves the record in; empty: where it was"`
	Branches    []ActionBranch `json:"branches,omitempty" title:"Conditional branches"`
	Inputs      []Input        `json:"inputs,omitempty" title:"What people give"`
	Sets        []Set          `json:"sets,omitempty" title:"What it sets"`
	Conditions  []Condition    `json:"conditions,omitempty" title:"What it needs"`
	// Roles are who may take it (ADR-0037 18b); empty: every role of the object.
	Roles     []string        `json:"roles,omitempty" title:"Taken by"`
	UnitField string          `json:"unitField,omitempty" title:"Unit field or path"`
	Approval  *ActionApproval `json:"approval,omitempty" title:"Approval"`
	// Creates are related records it makes in the same decision (ADR-0040 21c
	// D2): all of them and the record's change commit together, or none does.
	Creates []Create `json:"creates,omitempty" title:"What it creates"`
	// Updates modify referenced parent or target records in the same decision (ADR-0098 D2/D3).
	Updates []UpdateEffect `json:"updates,omitempty" title:"What it updates"`
	// Posts add to or subtract from balances kept on other objects, under the
	// same decision (ADR-0063).
	Posts []Post `json:"posts,omitempty" title:"What it posts"`
	// Journal is the balanced accounting entry the decision causes in the
	// books (core.journal, ADR-0076): landed by the host after acceptance,
	// once per decision, into the open period of its date.
	Journal *JournalPost `json:"journal,omitempty" title:"What it books"`
	// Reverses names another action of this object whose postings and journal
	// this one undoes with the opposite sign (ADR-0076): a reversal, not a delete.
	Reverses string `json:"reverses,omitempty" title:"Reverses action"`
}

// ActionBranch is one guarded destination and assignment rule inside an action (ADR-0098 D2): first matching branch wins.
type ActionBranch struct {
	When []Condition `json:"when,omitempty" title:"When conditions hold"`
	To   string      `json:"to,omitempty" title:"Leaves it in"`
	Sets []Set       `json:"sets,omitempty" title:"What it sets"`
}

// Create is a record of another defined object made when an action is taken:
// Via is that object's reference field back to this record.
type Create struct {
	Object  string `json:"object" field:"required" help:"A defined object of this builder whose reference points at this one" example:"build.followup"`
	Via     string `json:"via" field:"required" help:"That object's reference field to this record" example:"visit"`
	Count   string `json:"count,omitempty" title:"How many to create" help:"An integer input name or =<count> (1–64); empty: one record"`
	Foreach string `json:"foreach,omitempty" title:"For each related record" help:"A relation path to expand into child records"`
	Sets    []Set  `json:"sets,omitempty" title:"What it sets"`
}

// UpdateEffect modifies a referenced record (such as a parent order) inside the same atomic decision (ADR-0098 D3).
type UpdateEffect struct {
	Via  string      `json:"via" field:"required" help:"This record's reference field to the target record" example:"order"`
	When []Condition `json:"when,omitempty" title:"Only when"`
	To   string      `json:"to,omitempty" title:"Leaves target in"`
	Sets []Set       `json:"sets,omitempty" title:"What it sets"`
}

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
	Constraints *platform.InputConstraints `json:"constraints,omitempty"`
	Range       *platform.DateRange        `json:"range,omitempty"`
	Group       string                     `json:"group,omitempty"`
	Ref         string                     `json:"ref,omitempty" title:"Reference object"`
	MinLength   *int                       `json:"minLength,omitempty" title:"Minimum length"`
	Name        string                     `json:"name" field:"required" example:"to"`
	Title       string                     `json:"title" field:"required" title:"Label" example:"Handed to"`
	Type        string                     `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean,choice,reference,tags"`
	Choices     string                     `json:"choices,omitempty" help:"For a choice: the values, comma-separated"`
	Required    bool                       `json:"required,omitempty"`
	When        string                     `json:"when,omitempty" title:"Only when" help:"inputName=value,value — another choice or boolean input of this action"`
	DefaultFrom string                     `json:"defaultFrom,omitempty" title:"Default value source" help:"=<literal> or record.<field>"`
	Filter      []Condition                `json:"filter,omitempty" title:"Candidate filter" help:"For a reference input: conditions candidate records must satisfy"`
}

func (in Input) Field() platform.Field {
	c := platform.InputConstraints{}
	if in.Constraints != nil {
		c = *in.Constraints
	}
	if in.MinLength != nil {
		c.MinLength = in.MinLength
	}
	var when *platform.FieldCondition
	if in.When != "" {
		if field, values, ok := strings.Cut(in.When, "="); ok && field != "" && values != "" {
			when = &platform.FieldCondition{Field: field, In: ValueChoices(values)}
		}
	}
	var filters []platform.FieldFilter
	for _, cond := range in.Filter {
		filters = append(filters, platform.FieldFilter{Field: cond.Field, Operator: cond.Operator, Value: cond.Value, ValueField: cond.ValueField})
	}
	return platform.Field{Name: in.Name, Type: ValueInputTypes[in.Type], Ref: in.Ref, Required: in.Required, Description: in.Title, Choices: ValueChoices(in.Choices), Constraints: &c, Range: in.Range, Group: in.Group, When: when, DefaultFrom: in.DefaultFrom, Filter: filters}
}

// Set is a field of the record an action sets, from an input, a fixed value,
// the person taking it ($me) or the moment it is taken ($now).
type Set struct {
	Field string `json:"field" field:"required" help:"A field of the object"`
	From  string `json:"from" field:"required" help:"An input's name, $me, $now, =<literal>, record.<path>, input.<ref>.<path>, formula:<expr>, table:<name>.<col>, step:first/next, split:<total>:<count> or rollup:sum/count" example:"to"`
}

// Condition is what must hold for the action to be taken, and what a person
// reads when it does not (Foundry's submission criteria, as data).
type Condition struct {
	When       *Condition  `json:"when,omitempty" title:"Only when"`
	Quantifier string      `json:"quantifier,omitempty" choices:"all,none,any,count" title:"Collection quantifier"`
	Collection string      `json:"collection,omitempty" title:"Collection or relation" help:"An object type (e.g. build.order) or relation path (e.g. sfcs, order.sfcs)"`
	Where      []Condition `json:"where,omitempty" title:"Matching conditions"`
	Field      string      `json:"field,omitempty" help:"A declared record path through references, or input.<name>[.<path>]"`
	Operator   string      `json:"operator,omitempty" choices:"=,!=,<,<=,>,>=,in,not in,contains all,contains any,empty,not empty"`
	Value      string      `json:"value,omitempty" help:"What it is compared with; $me is the person taking the action"`
	ValueField string      `json:"valueField,omitempty" help:"Compare with another declared record path or input; excludes value"`
	Message    string      `json:"message,omitempty" help:"What a person reads when it does not hold"`
}

var ValueInputTypes = map[string]string{"text": "string", "longtext": "string", "integer": "integer", "decimal": "number", "date": "date", "datetime": "datetime", "boolean": "boolean", "choice": "string", "reference": "string", "tags": "string[]"}

func ValueConditionValue(kind, raw string) (any, error) {
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

// ValueItems extracts a list of string items from a tags slice, JSON array or comma-separated string.
func ValueItems(v any) []string {
	if v != nil {
		rv := reflect.ValueOf(v)
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			rv = rv.Elem()
		}
		v = rv.Interface()
	}
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		var out []string
		for _, item := range x {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		var out []string
		for _, item := range x {
			if s := strings.TrimSpace(ValueTextOf(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		return ValueChoices(x)
	default:
		if s := strings.TrimSpace(ValueTextOf(v)); s != "" {
			return []string{s}
		}
		return nil
	}
}

// holds compares typed values. An unset or malformed value fails closed; it
// never falls back to lexical order when a numeric or temporal parse fails.
func ValueHolds(got any, kind, op, want string) bool {
	switch op {
	case "contains all":
		gotItems := ValueItems(got)
		wantItems := ValueChoices(want)
		for _, w := range wantItems {
			if !slices.Contains(gotItems, w) {
				return false
			}
		}
		return true
	case "contains any":
		gotItems := ValueItems(got)
		wantItems := ValueChoices(want)
		if len(wantItems) == 0 {
			return len(gotItems) > 0
		}
		for _, w := range wantItems {
			if slices.Contains(gotItems, w) {
				return true
			}
		}
		return false
	case "in":
		text := ValueTextOf(got)
		if text == "" {
			return false
		}
		return slices.Contains(ValueChoices(want), text)
	case "not in":
		text := ValueTextOf(got)
		if text == "" {
			return true
		}
		return !slices.Contains(ValueChoices(want), text)
	}
	text := ValueTextOf(got)
	switch op {
	case "empty":
		return text == ""
	case "not empty":
		return text != ""
	}
	if text == "" {
		return false
	}
	a, errA := ValueConditionValue(kind, text)
	b, errB := ValueConditionValue(kind, want)
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
func ValueTextOf(v any) string {
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
	case []string:
		return strings.Join(x, ",")
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if s := ValueTextOf(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
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

// Access is what one role of the builder app may do with a defined object
// (ADR-0037 D4): which records it reads, and which of create, edit and
// archive it takes. The roles an object names become roles of `build`, which
// the Console assigns. Actions and fields name their own roles.
type Access struct {
	Role    string `json:"role" field:"required" help:"A role of the builder app, lower-case letters and digits" example:"desk"`
	Read    string `json:"read" field:"required" choices:"all,below,unit,own,none" help:"all: every record; below: those of the member's units and the units under them; unit: those of the member's own units; own: their own; none: not the object at all"`
	Create  bool   `json:"create,omitempty"`
	Edit    bool   `json:"edit,omitempty"`
	Archive bool   `json:"archive,omitempty"`
}

// ObjectScope names the fields that place a record for row scopes (ADR-0066).
type ObjectScope struct {
	Owner     string `json:"owner,omitempty" help:"The field holding the member a record belongs to; empty: whoever created it"`
	Unit      string `json:"unit,omitempty" help:"The field holding the record's organisation (a reference to enterprise.element, or its id)"`
	Structure string `json:"structure,omitempty" help:"The relationship kind \"below\" follows in the enterprise model, e.g. management"`
}
