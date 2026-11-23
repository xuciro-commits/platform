package authoring

import (
	"encoding/json"
	"fmt"
	ruleprotocol "platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func computeRulesOf(o ontology.Object, lookup func(string) (platform.RecordTypeInfo, bool)) func(platform.Caller, any, any, time.Time) *platform.Error {
	list, orderErr := ontology.FormulaOrder(o.Fields)
	followed := slices.DeleteFunc(slices.Clone(o.Fields), func(f ontology.Field) bool { return f.From == nil })
	defaults := slices.DeleteFunc(slices.Clone(o.Fields), func(f ontology.Field) bool { return f.Default == "" })
	if len(list) == 0 && len(followed) == 0 && len(defaults) == 0 {
		return nil
	}
	lookup = ruleLookup(o, lookup)
	return func(c platform.Caller, record, previous any, now time.Time) *platform.Error {
		if orderErr != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, orderErr.Error())
		}
		if previous == nil {
			for _, f := range defaults {
				if f.From == nil {
					if err := applyDefault(c, f, record, now); err != nil {
						return err
					}
				}
			}
		}
		for _, f := range followed {
			if err := follow(c, o, f, lookup, record, previous, now); err != nil {
				return err
			}
		}
		if previous == nil {
			for _, f := range defaults {
				if f.From != nil {
					if err := applyDefault(c, f, record, now); err != nil {
						return err
					}
				}
			}
		}
		for _, f := range list {
			e, err := ontology.FormulaExpr(o.Fields, f)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			s := ruleSpec(o, ontology.Action{}, o.Reads)
			schema, err := ruleprotocol.RuleSchema(e, s, lookup, c.Rules())
			if f.Formula != "" {
				refs := map[string]platform.ExprType{}
				for _, field := range o.Fields {
					kind := platform.RuleFieldKind(field.Type)
					if field.Type == "money" {
						kind = "decimal"
					}
					refs[field.Name] = platform.ExprType{Kind: kind}
				}
				schema = platform.ExprSchema{Refs: refs}
				_, err = c.Rules().CompileFrozenFormula(e, schema)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			ctx, err := ruleprotocol.RuleContext(c, e, s, lookup, record, nil, now)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
			}
			if f.Formula != "" {
				for _, field := range o.Fields {
					if field.Type == "money" {
						v := ctx.Refs[field.Name]
						v.Kind = "decimal"
						if v.Status == "value" {
							var amount struct {
								Amount json.RawMessage `json:"amount"`
							}
							if json.Unmarshal(v.Data, &amount) == nil {
								v.Data = amount.Amount
							}
						}
						ctx.Refs[field.Name] = v
					}
				}
			}
			var program platform.ExprProgram
			if f.Formula != "" {
				program, err = c.Rules().CompileFrozenFormula(e, schema)
			} else {
				program, err = c.Rules().CompileExpr(e, schema)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			var result platform.ExprResult
			if f.Formula != "" {
				result, err = program.EvalFrozenFormula(ctx)
			} else {
				result, err = program.Eval(ctx)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Field {field}: {reason} / 字段 {field}：{reason}", f.Name, err.Error(), f.Name, err.Error())
			}
			if f.FormulaExpr != nil {
				holder := reflect.ValueOf(record).Elem().FieldByName("RuleSources" + ontology.GoName(f.Name))
				if holder.IsValid() {
					holder.Set(reflect.ValueOf(ctx.Sources))
				}
			}
			if result.Value.Status == "null" {
				reflect.ValueOf(record).Elem().FieldByName(ontology.GoName(f.Name)).SetZero()
				continue
			}
			value, err := ruleprotocol.ExprGoValue(result.Value)
			if err == nil {
				err = assign(reflect.ValueOf(record).Elem().FieldByName(ontology.GoName(f.Name)), value)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
		return nil
	}
}

func follow(c platform.Caller, o ontology.Object, f ontology.Field, lookup func(string) (platform.RecordTypeInfo, bool), record, previous any, now time.Time) *platform.Error {
	target := reflect.ValueOf(record).Elem().FieldByName(ontology.GoName(f.Name))
	if previous == nil && !target.IsZero() {
		return nil
	}
	if previous != nil {
		before := reflect.ValueOf(previous)
		changed := func(ref string) bool {
			name, _, _ := strings.Cut(ref, ".")
			now, then := reflect.ValueOf(record).Elem().FieldByName(ontology.GoName(name)), before.FieldByName(ontology.GoName(name))
			return now.IsValid() && then.IsValid() && !reflect.DeepEqual(now.Interface(), then.Interface())
		}
		if !reflect.DeepEqual(before.FieldByName(ontology.GoName(f.Name)).Interface(), target.Interface()) || !reads(*f.From, changed) {
			return nil
		}
	}
	s := ruleSpec(o, ontology.Action{}, o.Reads)
	schema, err := ruleprotocol.RuleSchema(*f.From, s, lookup, c.Rules())
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	ctx, err := ruleprotocol.RuleContext(c, *f.From, s, lookup, record, nil, now)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
	}
	program, err := c.Rules().CompileExpr(*f.From, schema)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	result, err := program.Eval(ctx)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Field {field}: {reason} / 字段 {field}：{reason}", f.Name, err.Error(), f.Name, err.Error())
	}
	if result.Value.Status != "value" {
		if f.Required && f.Default == "" {
			return c.RefuseFields([]platform.FieldIssue{{Code: "required", Message: "Enter a value.", Path: []string{f.Name}}})
		}
		target.SetZero()
		return nil
	}
	value, err := ruleprotocol.ExprGoValue(result.Value)
	if err == nil {
		err = assign(target, value)
	}
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

func applyDefault(c platform.Caller, f ontology.Field, record any, now time.Time) *platform.Error {
	target := reflect.ValueOf(record).Elem().FieldByName(ontology.GoName(f.Name))
	if !target.IsValid() {
		return nil
	}
	if setting, ok := strings.CutPrefix(f.Default, "setting:"); ok {
		v := c.Setting("platform/" + setting)
		if v == "" {
			v = c.Setting(setting)
		}
		if v == "" {
			return nil
		}
		if f.Type == "money" {
			if target.Kind() == reflect.Pointer && !target.IsNil() {
				m := target.Elem().Interface().(platform.Money)
				if m.Currency == "" {
					m.Currency = v
					target.Elem().Set(reflect.ValueOf(m))
				}
			} else if target.Kind() == reflect.Struct {
				m := target.Interface().(platform.Money)
				if m.Currency == "" {
					m.Currency = v
					target.Set(reflect.ValueOf(m))
				}
			}
			return nil
		}
		if !target.IsZero() {
			return nil
		}
		if err := assign(target, v); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		return nil
	}
	if !target.IsZero() {
		return nil
	}
	var value any
	switch f.Default {
	case "member":
		if c.ID == "" || strings.HasPrefix(c.ID, "app:") {
			return nil
		}
		value = c.ID
	case "today":
		value = string(c.Today(now))
	case "now":
		if f.Type == "datetime" {
			value = now.UTC()
		} else {
			value = now.UTC().Format(time.RFC3339)
		}
	default:
		if offset, ok := ontology.NowOffset(f.Default); ok {
			value = now.UTC().Add(offset)
			break
		}
		switch f.Type {
		case "boolean":
			value = f.Default == "true"
		case "integer", "decimal":
			value = json.Number(f.Default)
		default:
			value = f.Default
		}
	}
	if err := assign(target, value); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

func reads(e platform.Expr, changed func(string) bool) bool {
	return e.Ref != "" && changed(e.Ref) || slices.ContainsFunc(e.Args, func(arg platform.Expr) bool { return reads(arg, changed) }) || e.In != nil && reads(*e.In, changed)
}

func ruleLookup(o ontology.Object, lookup func(string) (platform.RecordTypeInfo, bool)) func(string) (platform.RecordTypeInfo, bool) {
	return ontology.RuleLookup(o, ID, Builder, lookup)
}

func ruleSpec(o ontology.Object, a ontology.Action, read *platform.RuleReadSpec) platform.RuleReadSpec {
	return ontology.RuleSpec(o, ID, a, read)
}

func actionCriteria(o ontology.Object, a ontology.Action, lookup func(string) (platform.RecordTypeInfo, bool)) (*platform.RuleCriteria, error) {
	return ontology.ActionCriteria(o, ID, Builder, a, lookup)
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

func (b *Build) normalizeObjectRules(o *ontology.Object) error {
	lookup := ruleLookup(*o, b.conditionLookup)
	for i := range o.Fields {
		f := &o.Fields[i]
		if f.Formula != "" {
			used, parseErr := platform.FormulaFields(f.Formula)
			if parseErr != nil {
				return parseErr
			}
			for _, operand := range o.Fields {
				if operand.Type == "money" && used[operand.Name] {
					return fmt.Errorf("field %s legacy money formula needs an explicit currency-aware Expr", f.Name)
				}
			}
			e, err := ontology.FormulaExpr(o.Fields, *f)
			if err != nil {
				return err
			}
			f.FormulaExpr = &e
			f.Formula = ""
		}
	}
	for i := range o.Actions {
		if err := ontology.NormalizeTransaction(*o, &o.Actions[i], lookup); err != nil {
			return err
		}
	}
	if o.CreateTransaction != nil {
		action := ontology.Action{Name: "creation", Transaction: o.CreateTransaction}
		for _, field := range o.Fields {
			action.Inputs = append(action.Inputs, ontology.Input{Name: field.Name, Title: field.Title, Type: field.Type, Ref: field.Ref, Choices: field.Choices})
		}
		if err := ontology.NormalizeTransaction(*o, &action, lookup); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) checkObjectRules(o ontology.Object, engines ...platform.RuleEngine) error {
	if err := b.checkObjectProtocols(o); err != nil {
		return err
	}
	lookup := ruleLookup(o, b.conditionLookup)
	if _, err := ontology.FormulaOrder(o.Fields); err != nil {
		return err
	}
	for _, f := range o.Fields {
		if strings.HasPrefix(f.Name, "rulesources") || strings.HasPrefix(f.Name, "aisources") || f.Name == "rulewithheld" {
			return fmt.Errorf("field %s is reserved for rule provenance", f.Name)
		}
	}
	check := func(e platform.Expr, s platform.RuleReadSpec, where string) error {
		if err := b.checkRuleCollections(s, where, lookup); err != nil {
			return err
		}
		_, err := ruleprotocol.RuleSchema(e, s, lookup, engines...)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		return nil
	}
	for _, f := range o.Fields {
		if f.FormulaExpr != nil {
			if platform.RuleFieldKind(f.Type) != platform.RuleFieldKind(f.FormulaExpr.Kind) {
				return fmt.Errorf("field %s formula result type differs", f.Name)
			}
			if err := check(*f.FormulaExpr, ruleSpec(o, ontology.Action{}, o.Reads), "field "+f.Name); err != nil {
				return err
			}
		}
		if f.From != nil {
			if f.FormulaExpr != nil || f.Formula != "" || f.ReadOnly || f.AliasOf != "" || platform.RuleFieldKind(f.Type) != platform.RuleFieldKind(f.From.Kind) {
				return fmt.Errorf("field %s follows a rule only as an editable field of the same type", f.Name)
			}
			if err := check(*f.From, ruleSpec(o, ontology.Action{}, o.Reads), "field "+f.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Build) checkRuleCollections(s platform.RuleReadSpec, where string, lookup func(string) (platform.RecordTypeInfo, bool)) error {
	for name, set := range s.Collections {
		if set.Set != "" {
			info, ok := lookup(s.Object)
			if !ok || set.App != info.App || set.Query != "" || set.Of != "" && set.Of != "$subject" || !slices.ContainsFunc(info.Sets, func(declaration platform.RecordSet) bool {
				return declaration.Name == set.Set && declaration.Object == set.Object
			}) {
				return fmt.Errorf("%s collection %s must bind a declared set of its object", where, name)
			}
			continue
		}
		if b.host == nil {
			return fmt.Errorf("%s collection %s is unavailable", where, name)
		}
		if set.Of != "" && set.Of != "$subject" {
			_, selected, err := ruleprotocol.RuleCollectionSubjectField(s.Object, set.Of, lookup)
			if err != nil {
				return err
			}
			if _, ok := s.Inputs[set.Of]; !ok && !selected {
				return fmt.Errorf("%s collection %s needs a declared input", where, name)
			}
		}
		q, version, ok := b.host.RuleQuery(set.App, set.Query, set.Version)
		if set.App == ID {
			if _, selected := b.jointDeclarations[platform.AssetRef{App: set.App, Kind: platform.AssetQuery, Name: set.Query}]; selected {
				q, version, ok = b.queries.Definition(set.Query, set.Version)
			}
		}
		if !ok || version != set.Version || q.Object != set.Object || (q.By == "") != (set.Of == "") {
			return fmt.Errorf("%s collection %s does not bind its retained query/object/input", where, name)
		}
	}
	return nil
}
