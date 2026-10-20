package build

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/construction"

	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// States and actions a tenant gives its own objects (ADR-0044–D3). An action
// is typed data compiled into the platform's own lifecycle transition: the
// ledger routes it, replay runs it again, and every surface that offers a coded
// transition offers it. Nothing here is code the tenant supplies.

// creator makes one related record, or one balance after a posting, inside
// the decision being taken; nil where no host is attached (descriptors,
// validation), which refuses a Create or a Post.
type creator interface {
	create(c platform.Caller, cr construction.Create, inputs map[string]any, parentType, parent, id string, now time.Time) (any, *kernel.Error)
	post(c platform.Caller, p construction.Post, inputs map[string]any, record any, now time.Time) (any, *kernel.Error)
	transact(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) *kernel.Error
}

// Operators are the comparisons a condition may make.
var Operators = []string{"=", "!=", "<", "<=", ">", ">=", "empty", "not empty"}

// checkProcess refuses states and actions people could not take: names that are
// not names, an action from or to a state the object has not, an input or a
// field it does not have, a condition that compares nothing.
func checkProcess(o construction.Object, lookup func(string) (platform.EntityInfo, bool)) error {
	if len(o.States) == 0 && len(o.Actions) > 0 {
		return fmt.Errorf("an object needs states before it has actions")
	}
	states := map[string]bool{}
	for _, s := range o.States {
		switch {
		case !construction.Named(s.Name):
			return fmt.Errorf("the state name %q is not lower-case letters and digits", s.Name)
		case states[s.Name]:
			return fmt.Errorf("the state %q is declared twice", s.Name)
		case s.Title == "":
			return fmt.Errorf("the state %q has no title", s.Name)
		}
		states[s.Name] = true
	}
	fields := map[string]construction.Field{}
	for _, f := range o.Fields {
		fields[f.Name] = f
	}
	seen := map[string]bool{}
	for _, a := range o.Actions {
		if err := checkActionAdoption(a); err != nil {
			return err
		}
		if err := checkAdoptionSources(o, a, lookup); err != nil {
			return err
		}
		where := fmt.Sprintf("the action %q", a.Name)
		switch {
		case !construction.Named(a.Name):
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
			case !(a.Transaction != nil && approval.Pending == "") && (!states[approval.Pending] || approval.Pending == a.From[0] || approval.Pending == a.To):
				return fmt.Errorf("%s waits in %q, which must be a separate state of the object", where, approval.Pending)
			case approval.Rejected != "" && (a.Transaction != nil && approval.Pending == "" || !states[approval.Rejected] || approval.Rejected == approval.Pending):
				return fmt.Errorf("%s rejects into %q, which must be a state other than pending", where, approval.Rejected)
			case len(approval.Levels) == 0:
				return fmt.Errorf("%s waiting for approval needs an approver level", where)
			}
			for i, level := range approval.Levels {
				if strings.TrimSpace(level.Title) == "" || level.Role == "" {
					return fmt.Errorf("%s approval level %d needs a title and role", where, i+1)
				}
				if level.Role != construction.Builder && (len(o.Access) == 0 && level.Role != construction.User || len(o.Access) > 0 && !slices.ContainsFunc(o.Access, func(x construction.Access) bool { return x.Role == level.Role && x.Read == "all" })) {
					return fmt.Errorf("%s approval level %d names %q, which cannot read every record of this object", where, i+1, level.Role)
				}
			}
		}
		if a.To == "" && len(a.From) == 0 {
			return fmt.Errorf("%s leaves nothing to stay in", where)
		}
		inputs := map[string]construction.Input{}
		for _, in := range a.Inputs {
			switch {
			case !construction.Named(in.Name):
				return fmt.Errorf("%s: the input name %q is not lower-case letters and digits", where, in.Name)
			case inputs[in.Name].Name != "":
				return fmt.Errorf("%s: the input %q is declared twice", where, in.Name)
			case in.Title == "":
				return fmt.Errorf("%s: the input %q has no label", where, in.Name)
			case construction.ValueInputTypes[in.Type] == "":
				return fmt.Errorf("%s: the input %q has no type %q", where, in.Name, in.Type)
			case in.Type == "choice" && len(construction.ValueChoices(in.Choices)) == 0:
				return fmt.Errorf("%s: the choice input %q has no values", where, in.Name)
			}
			if in.Type == "reference" {
				app, _, ok := strings.Cut(in.Ref, ".")
				if !ok || in.Choices != "" || (platform.AssetRef{App: app, Kind: platform.AssetObject, Name: in.Ref}).Check() != nil {
					return fmt.Errorf("%s needs a declared reference object for %s", where, in.Name)
				}
				if lookup != nil && in.Ref != "enterprise.element" {
					info, known := lookup(in.Ref)
					if !known || info.App != app || info.Type != in.Ref {
						return fmt.Errorf("%s reference object %s is unavailable", where, in.Ref)
					}
				}
			} else if in.Ref != "" {
				return fmt.Errorf("%s non-reference input cannot name a reference object", where)
			}
			if err := construction.CheckEnterpriseReference(in.Type, in.Ref, in.Stereotype, in.Stereotypes); err != nil {
				return fmt.Errorf("%s: %w", where, err)
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
			if in.Type != "choice" || !in.Required || len(construction.ValueChoices(in.Choices)) == 0 {
				return fmt.Errorf("%s needs a required state choice input", where)
			}
			for _, state := range construction.ValueChoices(in.Choices) {
				if !states[state] {
					return fmt.Errorf("%s input chooses an undeclared state", where)
				}
			}
		}
		for _, set := range a.Sets {
			if set.Expr != nil {
				continue
			}
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
					for _, option := range construction.ValueChoices(in.Choices) {
						if !slices.Contains(construction.ValueChoices(f.Choices), option) {
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

func checkActionCondition(o construction.Object, a construction.Action, cond construction.Condition, lookup func(string) (platform.EntityInfo, bool), requireMessage bool) error {
	if cond.Expr != nil {
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
func conditionField(o construction.Object, a construction.Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.FieldInfo, error) {
	if input, ok := strings.CutPrefix(name, "input."); ok {
		for _, in := range a.Inputs {
			if in.Name == input {
				return platform.FieldInfo{Name: name, Type: in.Type, Ref: in.Ref, Choices: construction.ValueChoices(in.Choices)}, nil
			}
		}
		return platform.FieldInfo{}, fmt.Errorf("condition input %q is not declared", input)
	}
	return platform.RecordPathField(construction.TypeOf(o.Name), strings.Split(name, "."), func(typ string) (platform.EntityInfo, bool) {
		if typ != construction.TypeOf(o.Name) {
			if lookup == nil {
				return platform.EntityInfo{}, false
			}
			return lookup(typ)
		}
		info := platform.EntityInfo{Type: typ}
		for _, f := range o.Fields {
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Choices: construction.ValueChoices(f.Choices)})
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
func checkCondition(kind string, options []string, c construction.Condition) error {
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
	if _, err := construction.ValueConditionValue(kind, c.Value); err != nil {
		return fmt.Errorf("%q does not fit %s: %w", c.Value, kind, err)
	}
	if kind == "choice" && !slices.Contains(options, c.Value) {
		return fmt.Errorf("%q is not one of this field's choices", c.Value)
	}
	return nil
}

// lifecycle is the object's states and actions as the platform's own lifecycle
// (ADR-0044): each action a transition whose Do checks its conditions and
// sets its fields, inside the decision and again in replay.
func lifecycle(o construction.Object, roles []string, creates creator, lookup func(string) (platform.EntityInfo, bool)) *platform.Lifecycle {
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
					reach = construction.ValueChoices(in.Choices)
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
			takers = append([]string{construction.Builder}, a.Roles...)
		}
		var approval *platform.Approval
		if a.Approval != nil {
			approval = &platform.Approval{Pending: a.Approval.Pending, Rejected: a.Approval.Rejected}
			for _, level := range a.Approval.Levels {
				approval.Levels = append(approval.Levels, platform.ApprovalLevel{Title: level.Title, AppRole: level.Role, All: level.All})
			}
		}
		criteria, _ := actionCriteria(o, a, lookup)
		l.Transitions = append(l.Transitions, platform.Transition{Adoption: a.Adoption, Criteria: criteria, Name: a.Name, Title: a.Title, Description: a.Description,
			From: slices.Clone(a.From), To: reach, ToInput: a.ToInput, Roles: takers, Capability: o.Name, Payload: payload, Approval: approval, DeferRequired: true,
			Do: func(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
				return take(o, action, c, record, raw, now, creates, lookup)
			}, After: stored(o, action, creates)})
		if a.Transaction == nil {
			compiled, err := compileLegacyTransaction(o, a, lookup)
			if err == nil {
				action.Transaction = compiled
			} else {
				t := &l.Transitions[len(l.Transitions)-1]
				legacy := *t
				t.LegacyReplay = &legacy
				t.After = nil
				t.Do = func(platform.Caller, any, json.RawMessage, time.Time) *kernel.Error {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
				}
			}
		}
		if action.Transaction != nil {
			t := &l.Transitions[len(l.Transitions)-1]
			if t.Description == "" {
				t.Description = "Apply this business transaction."
			}
			if a.Transaction == nil {
				legacy := *t
				t.LegacyReplay = &legacy
			}
			t.Txn, t.After, t.Do = action.Transaction, nil, nil
			t.ApplyTxn = func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error {
				if creates == nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transactions need their original owner")
				}
				return creates.transact(c, r, action.Transaction, now)
			}
		}
	}
	return l
}

// stored puts an action's related records under the change that took it, as
// Do built and checked them from the same inputs (ADR-0047 21c).
func stored(o construction.Object, a construction.Action, creates creator) func(platform.Caller, *pb.ChangeRecord, any, time.Time) {
	if len(a.Creates) == 0 && len(a.Posts) == 0 || creates == nil {
		return nil
	}
	return func(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
		if !c.Replaying {
			c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Legacy action storage is reserved for historical inputs"))
			return
		}
		var inputs map[string]any
		_ = json.Unmarshal(r.GetSubmission().GetPayload(), &inputs)
		rec := reflect.ValueOf(record).Elem().Field(0).Interface().(platform.Record)
		for i, cr := range a.Creates {
			if value, err := creates.create(c, cr, inputs, construction.TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err == nil {
				c.Put(r, value)
			}
		}
		for _, p := range a.Posts {
			if value, err := creates.post(c, p, inputs, record, now); err == nil {
				c.Put(r, value)
			}
		}
	}
}

// take is one action on a record: its required inputs, its conditions, then
// the fields it sets. A refusal says what the builder wrote.
func take(o construction.Object, a construction.Action, c platform.Caller, record any, raw json.RawMessage, now time.Time, creates creator, lookup func(string) (platform.EntityInfo, bool)) *kernel.Error {
	if !c.Replaying {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Legacy action execution is reserved for historical inputs")
	}
	var inputs map[string]any
	if len(raw) > 0 && json.Unmarshal(raw, &inputs) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} takes an object of inputs", a.Title)
	}

	v := reflect.ValueOf(record).Elem()
	lookup = ruleLookup(o, lookup)
	criteria, err := actionCriteria(o, a, lookup)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if refusal := criteria.Check(c, record, inputs, now, lookup); refusal != nil {
		return refusal
	}
	if a.ToInput != "" {
		state, ok := inputs[a.ToInput].(string)
		allowed := false
		for _, in := range a.Inputs {
			if in.Name == a.ToInput && slices.Contains(construction.ValueChoices(in.Choices), state) {
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
		e, failure := assignmentExpr(types[set.Field], set, ruleSpec(o, a, a.Reads))
		if failure != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, failure.Error())
		}
		x, refusal := ruleAssignment(e, ruleSpec(o, a, a.Reads), c, record, inputs, now, lookup)
		if refusal != nil {
			return refusal
		}

		if err := assign(v.FieldByName(construction.GoName(set.Field)), x); err != nil {
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
		if _, err := creates.create(c, cr, inputs, construction.TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err != nil {
			return err
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
func checkScope(o construction.Object) error {
	field := func(name string) *construction.Field {
		if i := slices.IndexFunc(o.Fields, func(f construction.Field) bool { return f.Name == name }); i >= 0 {
			return &o.Fields[i]
		}
		return nil
	}
	sc := o.Scope
	if sc == nil {
		sc = &construction.ObjectScope{}
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
func checkAccess(o construction.Object) error {
	roles := map[string]bool{construction.Builder: true}
	for _, a := range o.Access {
		switch {
		case !construction.Named(a.Role):
			return fmt.Errorf("the role %q is not lower-case letters and digits", a.Role)
		case a.Role == construction.Builder:
			return fmt.Errorf("the role %q always does everything with what it builds", construction.Builder)
		case roles[a.Role]:
			return fmt.Errorf("the role %q is given access twice", a.Role)
		case readLevels[a.Read] == "":
			return fmt.Errorf("the role %q reads %q; it reads all, below, unit, own or none", a.Role, a.Read)
		case a.Read == "none" && (a.Create || a.Edit || a.Archive):
			return fmt.Errorf("the role %q may not read the object, so it may not create, edit or archive it either", a.Role)
		}
		roles[a.Role] = true
	}
	known := func(role string) bool { return roles[role] || len(o.Access) == 0 && role == construction.User }
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
// generated actions' roles per verb, and its scope (ADR-0044, D5). With no
// access declared an object keeps the rule of 15a: builder and user do everything.
func access(o construction.Object) (platform.Standard, platform.Scope, []string) {
	std := platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder, construction.User}, Capability: o.Name}
	if len(o.Access) == 0 {
		return std, platform.Scope{}, []string{construction.Builder, construction.User}
	}
	std.Roles = []string{construction.Builder}
	std.CreateRoles, std.EditRoles, std.ArchiveRoles = []string{construction.Builder}, []string{construction.Builder}, []string{construction.Builder}
	scope := platform.Scope{Owner: platform.OwnerCreated, Levels: map[string]string{}, Default: platform.ScopeNone}
	if o.Scope != nil {
		if o.Scope.Owner != "" {
			scope.Owner = o.Scope.Owner
		}
		scope.Unit, scope.Structure = o.Scope.Unit, o.Scope.Structure
	}
	scope.Levels[construction.Builder] = platform.ScopeTenant
	roles := []string{construction.Builder}
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
