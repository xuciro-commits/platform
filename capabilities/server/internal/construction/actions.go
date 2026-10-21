package construction

import (
	"fmt"
	"math"
	"reflect"

	"strconv"
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

// Action is a step people take on one of its records.
// Sets/Creates/Posts/Journal/Reverses are frozen legacy expressions.
// New multi-record capabilities use Transaction exclusively; historical
// definitions retain their original bytes and execution during migration.
type Action struct {
	Adoption    *platform.ProposalAdoption `json:"adoption,omitempty" type:"json" field:"aside"`
	Name        string                     `json:"name" field:"required" help:"Lower-case letters and digits" example:"return"`
	Title       string                     `json:"title" field:"required" title:"What people call it" example:"Hand it back"`
	Description string                     `json:"description,omitempty" help:"What it does, for people and agents"`
	From        []string                   `json:"from" title:"Taken from" help:"The states it takes a record from"`
	ToInput     string                     `json:"toInput,omitempty" title:"State from input"`
	To          string                     `json:"to,omitempty" title:"Leaves it in" help:"The state it leaves the record in; empty: where it was"`
	Inputs      []Input                    `json:"inputs,omitempty" title:"What people give"`
	Transaction *platform.BusinessTxn      `json:"transaction,omitempty"`
	Reads       *platform.RuleReadSpec     `json:"reads,omitempty" type:"json" field:"aside"`
	Sets        []Set                      `json:"sets,omitempty" title:"What it sets"`
	Conditions  []Condition                `json:"conditions,omitempty" title:"What it needs"`
	// Roles are who may take it; empty: every role of the object.
	Roles    []string        `json:"roles,omitempty" title:"Taken by"`
	Approval *ActionApproval `json:"approval,omitempty" title:"Approval"`
	// Creates are related records it makes in the same decision: all of them and the record's change commit together, or none does.
	Creates []Create `json:"creates,omitempty" title:"What it creates"`
	// Posts add to or subtract from balances kept on other objects, under the
	// same decision.
	Posts []Post `json:"posts,omitempty" title:"What it posts"`
	// Journal is the balanced accounting entry the decision causes in the
	// books (core.journal): landed by the host after acceptance,
	// once per decision, into the open period of its date.
	Journal *JournalPost `json:"journal,omitempty" title:"What it books"`
	// Reverses names another action of this object whose postings and journal
	// this one undoes with the opposite sign: a reversal, not a delete.
	Reverses string `json:"reverses,omitempty" title:"Reverses action"`
}

// Create is a record of another defined object made when an action is taken:
// Via is that object's reference field back to this record.
type Create struct {
	Object string `json:"object" field:"required" help:"A defined object of this builder whose reference points at this one" example:"build.followup"`
	Via    string `json:"via" field:"required" help:"That object's reference field to this record" example:"visit"`
	Sets   []Set  `json:"sets,omitempty" title:"What it sets"`
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
	Stereotype  string                     `json:"stereotype,omitempty" title:"Enterprise element type"`
	Stereotypes []string                   `json:"stereotypes,omitempty" title:"Enterprise element types"`
	MinLength   *int                       `json:"minLength,omitempty" title:"Minimum length"`
	Name        string                     `json:"name" field:"required" example:"to"`
	Title       string                     `json:"title" field:"required" title:"Label" example:"Handed to"`
	Type        string                     `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean,choice,reference"`
	Choices     string                     `json:"choices,omitempty" help:"For a choice: the values, comma-separated"`
	Required    bool                       `json:"required,omitempty"`
}

func (in Input) Field() platform.Field {
	c := platform.InputConstraints{}
	if in.Constraints != nil {
		c = *in.Constraints
	}
	if in.MinLength != nil {
		c.MinLength = in.MinLength
	}
	return platform.Field{Name: in.Name, Type: ValueInputTypes[in.Type], Ref: in.Ref, Stereotype: in.Stereotype, Stereotypes: in.Stereotypes, Required: in.Required, Description: in.Title, Choices: ValueChoices(in.Choices), Constraints: &c, Range: in.Range, Group: in.Group}
}

// Set is a field of the record an action sets, from an input, a fixed value,
// the person taking it ($me) or the moment it is taken ($now).
type Set = platform.TxnSet

// Condition is what must hold for the action to be taken, and what a person
// reads when it does not (Foundry's submission criteria, as data).
type Condition struct {
	Expr       *platform.Expr `json:"expr,omitempty" type:"json" field:"aside"`
	When       *Condition     `json:"when,omitempty" title:"Only when"`
	Field      string         `json:"field,omitempty" help:"A declared record path through references, or input.<name>"`
	Operator   string         `json:"operator,omitempty" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value      string         `json:"value,omitempty" help:"What it is compared with; $me is the person taking the action"`
	ValueField string         `json:"valueField,omitempty" help:"Compare with another declared record path or input; excludes value"`
	Message    string         `json:"message" field:"required" help:"What a person reads when it does not hold"`
}

var ValueInputTypes = map[string]string{"text": "string", "longtext": "string", "integer": "integer", "decimal": "number", "date": "date", "datetime": "datetime", "boolean": "boolean", "choice": "string", "reference": "string"}

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

// holds compares typed values. An unset or malformed value fails closed; it
// never falls back to lexical order when a numeric or temporal parse fails.
func ValueHolds(got any, kind, op, want string) bool {
	kind = platform.RuleFieldKind(kind)
	v := got
	if kind == "date" {
		if t, ok := got.(time.Time); ok {
			v = t.UTC().Format(time.DateOnly)
		}
	}
	e := platform.Expr{Kind: "boolean", Op: op, Args: []platform.Expr{platform.ExprReference(kind, "value")}}
	if op == "not empty" {
		e.Op = "not-empty"
	}
	if op != "empty" && op != "not empty" {
		x, err := ValueConditionValue(kind, want)
		if err != nil {
			return false
		}
		if t, ok := x.(time.Time); ok {
			if kind == "date" {
				x = t.Format(time.DateOnly)
			} else {
				x = t.Format(time.RFC3339Nano)
			}
		}
		e.Args = append(e.Args, platform.ExprLiteralValue(kind, x))
	}
	p, err := platform.CompileExpr(e, platform.ExprSchema{Refs: map[string]platform.ExprType{"value": {Kind: kind}}})
	if err != nil {
		return false
	}
	r, err := p.Eval(platform.ExprContext{Refs: map[string]platform.ExprValue{"value": platform.ExprPresent(kind, v)}})
	return err == nil && string(r.Value.Data) == "true"
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
// which records it reads, and which of create, edit and
// archive it takes. The roles an object names become roles of `build`, which
// the Console assigns. Actions and fields name their own roles.
type Access struct {
	Role    string `json:"role" field:"required" help:"A role of the builder app, lower-case letters and digits" example:"desk"`
	Read    string `json:"read" field:"required" choices:"all,below,unit,own,none" help:"all: every record; below: those of the member's units and the units under them; unit: those of the member's own units; own: their own; none: not the object at all"`
	Create  bool   `json:"create,omitempty"`
	Edit    bool   `json:"edit,omitempty"`
	Archive bool   `json:"archive,omitempty"`
}

// ObjectScope names the fields that place a record for row scopes.
type ObjectScope struct {
	Owner     string `json:"owner,omitempty" help:"The field holding the member a record belongs to; empty: whoever created it"`
	Unit      string `json:"unit,omitempty" help:"The field holding the record's organisation (a reference to enterprise.element, or its id)"`
	Structure string `json:"structure,omitempty" help:"The relationship kind \"below\" follows in the enterprise model, e.g. management"`
}
