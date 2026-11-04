package ontology

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

var Operators = []string{"=", "!=", "<", "<=", ">", ">=", "empty", "not empty"}

var ValueInputTypes = map[string]string{"text": "string", "longtext": "string", "integer": "integer", "decimal": "number", "date": "date", "datetime": "datetime", "boolean": "boolean", "choice": "string", "reference": "string", "attachment": "attachment"}

func CheckProcess(o Object, app, objectType, builderRole, userRole, functionCallType string, lookup func(string) (platform.EntityInfo, bool)) error {
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
		if err := CheckActionAdoption(a, app, functionCallType); err != nil {
			return err
		}
		if err := CheckAdoptionSources(o, objectType, a, lookup); err != nil {
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
				if level.Role != builderRole && (len(o.Access) == 0 && level.Role != userRole || len(o.Access) > 0 && !slices.ContainsFunc(o.Access, func(x Access) bool { return x.Role == level.Role && x.Read == "all" })) {
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
				owner, _, ok := strings.Cut(in.Ref, ".")
				if !ok || in.Choices != "" || (platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: in.Ref}).Check() != nil {
					return fmt.Errorf("%s needs a declared reference object for %s", where, in.Name)
				}
				if lookup != nil && in.Ref != "enterprise.element" {
					info, known := lookup(in.Ref)
					if !known || info.App != owner || info.Type != in.Ref {
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
				if !InputFits(in.Type, f.Type) || in.Type == "reference" && in.Ref != f.Ref {
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
				if err := CheckActionCondition(o, objectType, a, *cond.When, lookup, false); err != nil {
					return err
				}
			}
			if err := CheckActionCondition(o, objectType, a, cond, lookup, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func CheckActionAdoption(a Action, app, functionCallType string) error {
	if a.Adoption == nil {
		return nil
	}
	if a.Transaction == nil {
		return fmt.Errorf("Proposal adoption needs the original business transaction")
	}
	if err := a.Adoption.Check(); err != nil {
		return err
	}
	if a.Adoption.Function.App != "" && a.Adoption.Function.App != app {
		return fmt.Errorf("Proposal adoption cannot cross record authorities")
	}
	input := slices.IndexFunc(a.Inputs, func(in Input) bool { return in.Name == a.Adoption.Input })
	if input < 0 || a.Inputs[input].Type != "reference" || a.Inputs[input].Ref != functionCallType || !a.Inputs[input].Required {
		return fmt.Errorf("Adoption needs a required reference input to the retained function call")
	}
	for _, mapping := range a.Adoption.Mapping {
		if !slices.ContainsFunc(a.Inputs, func(in Input) bool { return in.Name == mapping.Input }) {
			return fmt.Errorf("Proposal mapping names an undeclared action input")
		}
	}
	return nil
}

func CheckAdoptionSources(o Object, objectType string, a Action, lookup func(string) (platform.EntityInfo, bool)) error {
	if a.Adoption == nil || a.Transaction == nil {
		return nil
	}
	mapped := map[string]bool{}
	for _, m := range a.Adoption.Mapping {
		mapped[m.Input] = true
	}
	taintedSteps := map[string]bool{}
	consumesRef := func(ref string) bool {
		if strings.HasPrefix(ref, "input.") {
			return mapped[strings.TrimPrefix(ref, "input.")]
		}
		if step, ok := strings.CutPrefix(ref, "step."); ok {
			name, _, _ := strings.Cut(step, ".")
			return taintedSteps[name]
		}
		return mapped[ref]
	}
	var consumes func(platform.Expr) bool
	consumes = func(e platform.Expr) bool {
		if consumesRef(e.Ref) {
			return true
		}
		for _, arg := range e.Args {
			if consumes(arg) {
				return true
			}
		}
		return e.In != nil && consumes(*e.In)
	}
	var problem error
	platform.WalkBusinessTxn(a.Transaction, func(step platform.TxnStep) {
		for _, change := range step.Changes {
			if !consumesRef(change.From) && (change.Expr == nil || !consumes(*change.Expr)) {
				continue
			}
			taintedSteps[step.Name] = true
			source := ""
			if step.Target.Type == objectType {
				for _, field := range o.Fields {
					if field.Name == change.Field {
						source = field.SourceType
						break
					}
				}
			} else if info, ok := lookup(step.Target.Type); ok {
				if field, ok := info.Field(change.Field); ok {
					source = field.SourceType
				}
			}
			if source != "generated" {
				problem = fmt.Errorf("AI proposal destinations must declare generated provenance")
			}
		}
	})
	return problem
}

func CheckActionCondition(o Object, objectType string, a Action, cond Condition, lookup func(string) (platform.EntityInfo, bool), requireMessage bool) error {
	if cond.Expr != nil {
		return nil
	}
	left, err := conditionField(o, objectType, a, cond.Field, lookup)
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
		right, err := conditionField(o, objectType, a, cond.ValueField, lookup)
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

func ConditionField(o Object, objectType string, a Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.FieldInfo, error) {
	return conditionField(o, objectType, a, name, lookup)
}

func conditionField(o Object, objectType string, a Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.FieldInfo, error) {
	if input, ok := strings.CutPrefix(name, "input."); ok {
		for _, in := range a.Inputs {
			if in.Name == input {
				return platform.FieldInfo{Name: name, Type: in.Type, Ref: in.Ref, Choices: ValueChoices(in.Choices)}, nil
			}
		}
		return platform.FieldInfo{}, fmt.Errorf("condition input %q is not declared", input)
	}
	return platform.RecordPathField(objectType, strings.Split(name, "."), func(typ string) (platform.EntityInfo, bool) {
		if typ != objectType {
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

func InputFits(input, field string) bool {
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

func CheckActionPosts(o Object) error {
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
			case len(a.Posts) > 0:
				return fmt.Errorf("the action %q reverses %q and so posts nothing of its own", a.Name, a.Reverses)
			}
		}
	}
	return nil
}

func CheckActionPostings(o Object, app string, lookup func(string) (platform.EntityInfo, bool)) error {
	for _, a := range o.Actions {
		for _, p := range a.Posts {
			where := fmt.Sprintf("the action %q posts to %q", a.Name, p.Object)
			if !strings.HasPrefix(p.Object, app+".") {
				return fmt.Errorf("%s, which is another app's object; change it through that app's actions", where)
			}
			if p.Object == app+"."+o.Name {
				return fmt.Errorf("%s, which is this object itself", where)
			}
			if len(p.Match) == 0 {
				return fmt.Errorf("%s without a field that identifies the balance", where)
			}
			for _, m := range p.Match {
				if err := checkSource(o, a, m.From); err != nil {
					return fmt.Errorf("%s, match %q: %w", where, m.Field, err)
				}
			}
			if err := checkSource(o, a, p.Amount); err != nil {
				return fmt.Errorf("%s, amount: %w", where, err)
			}
			if lookup == nil {
				continue
			}
			info, known := lookup(p.Object)
			if !known {
				return fmt.Errorf("%s, which this tenant has not published", where)
			}
			for _, m := range p.Match {
				if _, ok := info.Field(m.Field); !ok {
					return fmt.Errorf("%s and matches %q, which it has no field for", where, m.Field)
				}
			}
			f, ok := info.Field(p.Field)
			if !ok || f.Type != "integer" && f.Type != "decimal" && f.Type != "money" {
				return fmt.Errorf("%s into %q, which is not an integer, decimal or money field of it", where, p.Field)
			}
		}
	}
	return nil
}

func checkSource(o Object, a Action, from string) error {
	switch {
	case from == "$me", strings.HasPrefix(from, "="):
		return nil
	case strings.HasPrefix(from, "record."):
		name := strings.TrimPrefix(from, "record.")
		for _, f := range o.Fields {
			if f.Name == name {
				return nil
			}
		}
		return fmt.Errorf("%q is not a field of this object", name)
	}
	for _, in := range a.Inputs {
		if in.Name == from {
			return nil
		}
	}
	return fmt.Errorf("%q is not an input of the action", from)
}

func RuleSpec(o Object, app string, a Action, read *platform.RuleReadSpec) platform.RuleReadSpec {
	s := platform.RuleReadSpec{Object: app + "." + o.Name, Inputs: map[string]platform.ExprType{}}
	if a.Transaction != nil {
		s.Inputs["$subject"] = platform.ExprType{Kind: "reference", Object: s.Object}
	}
	if read != nil {
		s.Collections = read.Collections
	}
	for _, in := range a.Inputs {
		s.Inputs[in.Name] = platform.ExprType{Kind: platform.RuleFieldKind(in.Type), Object: in.Ref}
	}
	return s
}

func RuleLookup(o Object, app, builderRole string, lookup func(string) (platform.EntityInfo, bool)) func(string) (platform.EntityInfo, bool) {
	return func(typ string) (platform.EntityInfo, bool) {
		if typ != app+"."+o.Name {
			if lookup == nil {
				return platform.EntityInfo{}, false
			}
			return lookup(typ)
		}
		info := platform.EntityInfo{Type: typ, App: app, Sets: o.Sets}
		for _, f := range o.Fields {
			read := f.Read
			if len(read) > 0 {
				read = append([]string{builderRole}, read...)
			}
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Read: read})
		}
		if len(o.States) > 0 {
			info.Fields = append(info.Fields, platform.FieldInfo{Name: "state", Type: "choice"})
		}
		return info, true
	}
}

func LegacyOperand(o Object, objectType string, a Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
	f, err := ConditionField(o, objectType, a, name, lookup)
	if err != nil {
		return platform.Expr{}, err
	}
	kind := platform.RuleFieldKind(f.Type)
	if strings.HasPrefix(name, "input.") || !strings.Contains(name, ".") {
		return platform.ExprReference(kind, name), nil
	}
	parts := strings.Split(name, ".")
	return platform.Expr{Op: "path", Kind: kind, Args: []platform.Expr{platform.ExprReference("reference", parts[0])}, Path: parts[1:]}, nil
}

func ConditionExpr(o Object, objectType string, a Action, cond Condition, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
	if cond.Expr != nil {
		if cond.Field != "" || cond.Operator != "" || cond.Value != "" || cond.ValueField != "" || cond.When != nil {
			return platform.Expr{}, fmt.Errorf("use one expression condition")
		}
		return *cond.Expr, nil
	}
	left, err := LegacyOperand(o, objectType, a, cond.Field, lookup)
	if err != nil {
		return left, err
	}
	op := cond.Operator
	if op == "not empty" {
		op = "not-empty"
	}
	e := platform.Expr{Op: op, Kind: "boolean", Args: []platform.Expr{left}}
	if op != "empty" && op != "not-empty" {
		var right platform.Expr
		if cond.ValueField != "" {
			right, err = LegacyOperand(o, objectType, a, cond.ValueField, lookup)
		} else if cond.Value == "$me" {
			right = platform.ExprReference("text", "caller.id")
		} else {
			var x any
			x, err = platform.Literal(left.Kind, cond.Value)
			right = platform.ExprLiteralValue(left.Kind, x)
		}
		if err != nil {
			return e, err
		}
		e.Args = append(e.Args, right)
	}
	if cond.When != nil {
		when, err := ConditionExpr(o, objectType, a, *cond.When, lookup)
		if err != nil {
			return e, err
		}
		e = platform.Expr{Op: "or", Kind: "boolean", Args: []platform.Expr{{Op: "not", Kind: "boolean", Args: []platform.Expr{when}}, e}}
	}
	return e, nil
}

func ActionCriteria(o Object, app, builderRole string, a Action, lookup func(string) (platform.EntityInfo, bool)) (*platform.RuleCriteria, error) {
	reads, conditions := a.Reads, a.Conditions
	if txn := a.Transaction; txn != nil {
		reads, conditions = txn.Reads, nil
		if txn.Guard != nil {
			conditions = []Condition{{Expr: txn.Guard, Message: txn.Message}}
		}
	}
	plan := &platform.RuleCriteria{Reads: RuleSpec(o, app, a, reads)}
	used := map[string]bool{}
	var visit func(platform.Expr)
	visit = func(expr platform.Expr) {
		if strings.HasPrefix(expr.Ref, "input.") {
			name, _, _ := strings.Cut(strings.TrimPrefix(expr.Ref, "input."), ".")
			used[name] = true
		}
		for _, arg := range expr.Args {
			visit(arg)
		}
		if expr.In != nil {
			visit(*expr.In)
		}
	}
	for _, condition := range conditions {
		expr, err := ConditionExpr(o, app+"."+o.Name, a, condition, RuleLookup(o, app, builderRole, lookup))
		if err != nil {
			return nil, err
		}
		plan.Conditions = append(plan.Conditions, platform.RuleCriterion{Expr: expr, Message: condition.Message})
		visit(expr)
	}
	for _, set := range plan.Reads.Collections {
		if strings.HasPrefix(set.Of, "input.") {
			name, _, _ := strings.Cut(strings.TrimPrefix(set.Of, "input."), ".")
			used[name] = true
		}
	}
	for name := range plan.Reads.Inputs {
		if !used[name] {
			delete(plan.Reads.Inputs, name)
		}
	}
	return plan, nil
}
