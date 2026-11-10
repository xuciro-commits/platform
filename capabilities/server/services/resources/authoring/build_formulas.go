package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func computeRulesOf(o Object, lookup func(string) (platform.EntityInfo, bool)) func(platform.Caller, any, time.Time) *kernel.Error {
	list, orderErr := ontology.FormulaOrder(o.Fields)
	if len(list) == 0 {
		return nil
	}
	lookup = ruleLookup(o, lookup)
	return func(c platform.Caller, record any, now time.Time) *kernel.Error {
		if orderErr != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, orderErr.Error())
		}
		for _, f := range list {
			e, err := ontology.FormulaExpr(o.Fields, f)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			s := ruleSpec(o, Action{}, o.Reads)
			schema, err := platform.RuleSchema(e, s, lookup, c.Rules())
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
			ctx, err := c.RuleContext(e, s, lookup, record, nil, now)
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
			value, err := platform.ExprGoValue(result.Value)
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

func ruleLookup(o Object, lookup func(string) (platform.EntityInfo, bool)) func(string) (platform.EntityInfo, bool) {
	return ontology.RuleLookup(o, ID, Builder, lookup)
}

func ruleSpec(o Object, a Action, read *platform.RuleReadSpec) platform.RuleReadSpec {
	return ontology.RuleSpec(o, ID, a, read)
}

func actionCriteria(o Object, a Action, lookup func(string) (platform.EntityInfo, bool)) (*platform.RuleCriteria, error) {
	return ontology.ActionCriteria(o, ID, Builder, a, lookup)
}

func (b *Build) normalizeObjectRules(o *Object) error {
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
		if err := b.normalizeTransaction(*o, &o.Actions[i], lookup); err != nil {
			return err
		}
	}
	if o.CreateTransaction != nil {
		action := Action{Name: "creation", Transaction: o.CreateTransaction}
		for _, field := range o.Fields {
			action.Inputs = append(action.Inputs, Input{Name: field.Name, Title: field.Title, Type: field.Type, Ref: field.Ref, Choices: field.Choices})
		}
		if err := b.normalizeTransaction(*o, &action, lookup); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) checkObjectRules(o Object, engines ...platform.RuleEngine) error {
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
		_, err := platform.RuleSchema(e, s, lookup, engines...)
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
			if err := check(*f.FormulaExpr, ruleSpec(o, Action{}, o.Reads), "field "+f.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Build) checkRuleCollections(s platform.RuleReadSpec, where string, lookups ...func(string) (platform.EntityInfo, bool)) error {
	lookup := b.conditionLookup
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
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
			_, selected, err := platform.RuleCollectionSubjectField(s.Object, set.Of, lookup)
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
				q, version, ok = b.QueryDefinition(set.Query, set.Version)
			}
		}
		if !ok || version != set.Version || q.Object != set.Object || (q.By == "") != (set.Of == "") {
			return fmt.Errorf("%s collection %s does not bind its retained query/object/input", where, name)
		}
	}
	return nil
}
