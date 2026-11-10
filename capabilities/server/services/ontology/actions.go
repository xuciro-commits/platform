package ontology

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

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
		case a.Transaction == nil:
			return fmt.Errorf("%s has no transaction", where)
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
			case approval.Pending != "" && (!states[approval.Pending] || approval.Pending == a.From[0]):
				return fmt.Errorf("%s waits in %q, which must be a separate state of the object", where, approval.Pending)
			case approval.Rejected != "" && (approval.Pending == "" || !states[approval.Rejected] || approval.Rejected == approval.Pending):
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
	}
	return nil
}

func CheckActionAdoption(a Action, app, functionCallType string) error {
	if a.Adoption == nil {
		return nil
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

func ActionCriteria(o Object, app, builderRole string, a Action, lookup func(string) (platform.EntityInfo, bool)) (*platform.RuleCriteria, error) {
	plan := &platform.RuleCriteria{Reads: RuleSpec(o, app, a, a.Transaction.Reads)}
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
	if guard := a.Transaction.Guard; guard != nil {
		plan.Conditions = append(plan.Conditions, platform.RuleCriterion{Expr: *guard, Message: a.Transaction.Message})
		visit(*guard)
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
