package build

import (
	"encoding/json"
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

type creator interface {
	create(c platform.Caller, cr Create, inputs map[string]any, parentType, parent, id string, now time.Time) (any, *kernel.Error)
	post(c platform.Caller, p Post, inputs map[string]any, record any, now time.Time) (any, *kernel.Error)
	businessRuntime() platform.TransactionExecutor
}

var Operators = []string{"=", "!=", "<", "<=", ">", ">=", "empty", "not empty"}

func checkProcess(o Object, lookup func(string) (platform.EntityInfo, bool)) error {
	if len(o.States) == 0 && len(o.Actions) > 0 {
		return fmt.Errorf("an object needs states before it has actions")
	}
	states := map[string]bool{}
	for _, s := range o.States {
		switch {
		case !Named(s.Name):
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
		if err := checkActionAdoption(a); err != nil {
			return err
		}
		if err := checkAdoptionSources(o, a, lookup); err != nil {
			return err
		}
		where := fmt.Sprintf("the action %q", a.Name)
		switch {
		case !Named(a.Name):
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
			case !Named(in.Name):
				return fmt.Errorf("%s: the input name %q is not lower-case letters and digits", where, in.Name)
			case inputs[in.Name].Name != "":
				return fmt.Errorf("%s: the input %q is declared twice", where, in.Name)
			case in.Title == "":
				return fmt.Errorf("%s: the input %q has no label", where, in.Name)
			case ValueInputTypes[in.Type] == "":
				return fmt.Errorf("%s: the input %q has no type %q", where, in.Name, in.Type)
			case in.Type == "choice" && len(ValueChoices(in.Choices)) == 0:
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
			if err := CheckEnterpriseReference(in.Type, in.Ref, in.Stereotype, in.Stereotypes); err != nil {
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
			if in.Type != "choice" || !in.Required || len(ValueChoices(in.Choices)) == 0 {
				return fmt.Errorf("%s needs a required state choice input", where)
			}
			for _, state := range ValueChoices(in.Choices) {
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
				if _, err := platform.Literal(f.Type, strings.TrimPrefix(set.From, "=")); err != nil {
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
					for _, option := range ValueChoices(in.Choices) {
						if !slices.Contains(ValueChoices(f.Choices), option) {
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

func conditionField(o Object, a Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.FieldInfo, error) {
	if input, ok := strings.CutPrefix(name, "input."); ok {
		for _, in := range a.Inputs {
			if in.Name == input {
				return platform.FieldInfo{Name: name, Type: in.Type, Ref: in.Ref, Choices: ValueChoices(in.Choices)}, nil
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
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Choices: ValueChoices(f.Choices)})
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
	if _, err := platform.ConditionValue(kind, c.Value); err != nil {
		return fmt.Errorf("%q does not fit %s: %w", c.Value, kind, err)
	}
	if kind == "choice" && !slices.Contains(options, c.Value) {
		return fmt.Errorf("%q is not one of this field's choices", c.Value)
	}
	return nil
}

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
			to = a.From[0]
		}
		reach := []string{to}
		if a.To == "" {
			reach = slices.Clone(a.From)
		}
		if a.ToInput != "" {
			for _, in := range a.Inputs {
				if in.Name == a.ToInput {
					reach = ValueChoices(in.Choices)
				}
			}
		}
		payload := []platform.Field{}
		for _, in := range a.Inputs {
			payload = append(payload, in.Field())
		}
		action := a
		takers := slices.Clone(roles)
		if len(a.Roles) > 0 {
			takers = append([]string{Builder}, a.Roles...)
		}
		var approval *platform.Approval
		if a.Approval != nil {
			approval = &platform.Approval{Pending: a.Approval.Pending, Rejected: a.Approval.Rejected}
			for _, level := range a.Approval.Levels {
				approval.Levels = append(approval.Levels, platform.ApprovalLevel{Title: level.Title, AppRole: level.Role, All: level.All})
			}
		}
		criteria, _ := actionCriteria(o, a, lookup)
		l.Transitions = append(l.Transitions, platform.Transition{ConfirmationOnly: a.ConfirmationOnly, Adoption: a.Adoption, Criteria: criteria, Name: a.Name, Title: a.Title, Description: a.Description,
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
				return creates.businessRuntime().Transact(c, r, action.Transaction, now)
			}
		}
	}
	return l
}

func stored(o Object, a Action, creates creator) func(platform.Caller, *pb.ChangeRecord, any, time.Time) {
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
			if value, err := creates.create(c, cr, inputs, TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err == nil {
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

func take(o Object, a Action, c platform.Caller, record any, raw json.RawMessage, now time.Time, creates creator, lookup func(string) (platform.EntityInfo, bool)) *kernel.Error {
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
			if in.Name == a.ToInput && slices.Contains(ValueChoices(in.Choices), state) {
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
		e, failure := platform.AssignmentExpr(types[set.Field], set, ruleSpec(o, a, a.Reads))
		if failure != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, failure.Error())
		}
		x, refusal := platform.RuleAssignment(e, ruleSpec(o, a, a.Reads), c, record, inputs, now, lookup)
		if refusal != nil {
			return refusal
		}

		if err := assign(v.FieldByName(GoName(set.Field)), x); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} cannot set {field}: {why}", a.Title, set.Field, err.Error())
		}
	}
	rec := v.Field(0).Interface().(platform.Record)
	for i, cr := range a.Creates {
		if creates == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot create records here", a.Title)
		}
		if _, err := creates.create(c, cr, inputs, TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err != nil {
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

func checkAccess(o Object) error {
	roles := map[string]bool{Builder: true}
	for _, a := range o.Access {
		switch {
		case !Named(a.Role):
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

type State struct {
	Name        string `json:"name" field:"required" help:"Lower-case letters and digits" example:"found"`
	Title       string `json:"title" field:"required" title:"What people call it" example:"Found"`
	Tone        string `json:"tone,omitempty" choices:"info,success,warning,danger,neutral"`
	Description string `json:"description,omitempty" help:"What a record in this state means"`
}

type Action struct {
	ConfirmationOnly bool                       `json:"confirmationOnly,omitempty" title:"Require original confirmation receipt"`
	Adoption         *platform.ProposalAdoption `json:"adoption,omitempty" type:"json" field:"aside"`
	Name             string                     `json:"name" field:"required" help:"Lower-case letters and digits" example:"return"`
	Title            string                     `json:"title" field:"required" title:"What people call it" example:"Hand it back"`
	Description      string                     `json:"description,omitempty" help:"What it does, for people and agents"`
	From             []string                   `json:"from" title:"Taken from" help:"The states it takes a record from"`
	ToInput          string                     `json:"toInput,omitempty" title:"State from input"`
	To               string                     `json:"to,omitempty" title:"Leaves it in" help:"The state it leaves the record in; empty: where it was"`
	Inputs           []Input                    `json:"inputs,omitempty" title:"What people give"`
	Transaction      *platform.BusinessTxn      `json:"transaction,omitempty"`
	Reads            *platform.RuleReadSpec     `json:"reads,omitempty" type:"json" field:"aside"`
	Sets             []Set                      `json:"sets,omitempty" title:"What it sets"`
	Conditions       []Condition                `json:"conditions,omitempty" title:"What it needs"`
	Roles            []string                   `json:"roles,omitempty" title:"Taken by"`
	Approval         *ActionApproval            `json:"approval,omitempty" title:"Approval"`
	Creates          []Create                   `json:"creates,omitempty" title:"What it creates"`
	Posts            []Post                     `json:"posts,omitempty" title:"What it posts"`
	Journal          *JournalPost               `json:"journal,omitempty" title:"What it books"`
	Reverses         string                     `json:"reverses,omitempty" title:"Reverses action"`
}

type Create struct {
	Object string `json:"object" field:"required" help:"A defined object of this builder whose reference points at this one" example:"build.followup"`
	Via    string `json:"via" field:"required" help:"That object's reference field to this record" example:"visit"`
	Sets   []Set  `json:"sets,omitempty" title:"What it sets"`
}

type ActionApproval struct {
	Pending  string          `json:"pending" title:"While it waits"`
	Rejected string          `json:"rejected,omitempty" title:"If rejected"`
	Levels   []ApproverLevel `json:"levels" title:"Approver levels"`
}

type ApproverLevel struct {
	Title string `json:"title" title:"What this level is called"`
	Role  string `json:"role" title:"Approver role"`
	All   bool   `json:"all,omitempty" title:"Everyone in the role must approve"`
}

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

type Set = platform.TxnSet

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

type Access struct {
	Role    string `json:"role" field:"required" help:"A role of the builder app, lower-case letters and digits" example:"desk"`
	Read    string `json:"read" field:"required" choices:"all,below,unit,own,none" help:"all: every record; below: those of the member's units and the units under them; unit: those of the member's own units; own: their own; none: not the object at all"`
	Create  bool   `json:"create,omitempty"`
	Edit    bool   `json:"edit,omitempty"`
	Archive bool   `json:"archive,omitempty"`
}

type ObjectScope struct {
	Owner     string `json:"owner,omitempty" help:"The field holding the member a record belongs to; empty: whoever created it"`
	Unit      string `json:"unit,omitempty" help:"The field holding the record's organisation (a reference to enterprise.element, or its id)"`
	Structure string `json:"structure,omitempty" help:"The relationship kind \"below\" follows in the enterprise model, e.g. management"`
}

func (b *Build) checkJournal(o Object) error {
	if o.Numbering != nil {
		if err := o.Numbering.Check(o); err != nil {
			return err
		}
	}
	for _, a := range o.Actions {
		if a.Reverses != "" {
			j := -1
			for k, other := range o.Actions {
				if other.Name == a.Reverses {
					j = k
				}
			}
			switch {
			case j < 0 || a.Reverses == a.Name:
				return fmt.Errorf("the action %q reverses %q, which is not another action of this object", a.Name, a.Reverses)
			case len(a.Posts) > 0 || a.Journal != nil:
				return fmt.Errorf("the action %q reverses %q and so posts and books nothing of its own", a.Name, a.Reverses)
			}
		}
		if a.Journal == nil {
			continue
		}
		where := fmt.Sprintf("the action %q books", a.Name)
		if a.Journal.ClosedPeriod != "" && a.Journal.ClosedPeriod != "reject" && a.Journal.ClosedPeriod != "next-open" {
			return fmt.Errorf("%s with an unknown closed period policy", where)
		}
		if len(a.Journal.Lines) < 2 {
			return fmt.Errorf("%s fewer than two lines", where)
		}
		if a.Journal.Date != "" {
			if err := checkSource(o, a, a.Journal.Date); err != nil {
				return fmt.Errorf("%s, date: %w", where, err)
			}
		}
		for i, l := range a.Journal.Lines {
			if l.Account == "" || (l.Debit == "") == (l.Credit == "") {
				return fmt.Errorf("%s line %d without an account, or without exactly one of debit and credit", where, i+1)
			}
			for _, src := range []string{l.Account, l.Debit, l.Credit, l.Object, l.Partner} {
				if src == "" {
					continue
				}
				if err := checkSource(o, a, src); err != nil {
					return fmt.Errorf("%s line %d: %w", where, i+1, err)
				}
			}
		}
	}
	return nil
}

func numberingOf(o Object, model any) func(c platform.Caller, record any, now time.Time) *kernel.Error {
	if o.Numbering == nil {
		return nil
	}
	n, typ := *o.Numbering, reflect.TypeOf(model)
	return func(c platform.Caller, record any, now time.Time) *kernel.Error {
		v := reflect.ValueOf(record).Elem()
		field := v.FieldByName(GoName(n.Field))
		if !field.IsValid() || field.Kind() != reflect.String || field.String() != "" || v.Field(0).Interface().(platform.Record).Revision > 0 {
			return nil
		}
		year := now.UTC().Year()
		_, count, err := c.FindOf(typ, platform.Query{Limit: 1, Archived: true})
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The number range is not readable here")
		}
		seq := count + 1
		if n.Yearly {
			prefix := n.Prefix + strconv.Itoa(year) + "-"
			all, _, _ := c.FindOf(typ, platform.Query{Limit: 100000, Archived: true})
			seq = 1
			for _, r := range all {
				if strings.HasPrefix(reflect.ValueOf(r).FieldByName(GoName(n.Field)).String(), prefix) {
					seq++
				}
			}
		}
		field.SetString(n.Format(seq, year))
		return nil
	}
}
