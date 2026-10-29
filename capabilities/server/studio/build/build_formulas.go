package build

import (
	"context"
	"encoding/json"
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"regexp"
	"slices"

	"strings"
	"time"
)

func formulaExpr(fields []Field, f Field) (platform.Expr, error) {
	if f.FormulaExpr != nil {
		if f.Formula != "" {
			return platform.Expr{}, fmt.Errorf("field %s has two formulas", f.Name)
		}
		return *f.FormulaExpr, nil
	}
	types := map[string]string{}
	for _, operand := range fields {
		if operand.Formula == "" && operand.FormulaExpr == nil && (operand.Type == "integer" || operand.Type == "decimal" || operand.Type == "money") {
			types[operand.Name] = operand.Type
			if operand.Type == "money" {
				types[operand.Name] = "decimal"
			}
		}
	}
	e, err := platform.LowerFormula(f.Formula, types)
	if err != nil {
		return e, err
	}
	if f.Type == "integer" {
		e = platform.Expr{Op: "round", Kind: "integer", Args: []platform.Expr{e}}
	}
	return e, nil
}
func checkFormulas(fields []Field) error {
	for _, f := range fields {
		if f.Formula != "" {
			if f.Type != "integer" && f.Type != "decimal" {
				return fmt.Errorf("field %s legacy formula needs a numeric destination", f.Name)
			}
			if _, err := formulaExpr(fields, f); err != nil {
				return fmt.Errorf("field %s: %w", f.Name, err)
			}
		}
	}
	return nil
}
func computeRulesOf(o Object, lookup func(string) (platform.EntityInfo, bool)) func(platform.Caller, any, time.Time) *kernel.Error {
	list, orderErr := formulaOrder(o.Fields)
	if len(list) == 0 {
		return nil
	}
	lookup = ruleLookup(o, lookup)
	return func(c platform.Caller, record any, now time.Time) *kernel.Error {
		if orderErr != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, orderErr.Error())
		}
		for _, f := range list {
			e, err := formulaExpr(o.Fields, f)
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
				holder := reflect.ValueOf(record).Elem().FieldByName("RuleSources" + GoName(f.Name))
				if holder.IsValid() {
					holder.Set(reflect.ValueOf(ctx.Sources))
				}
			}
			value, err := platform.ExprGoValue(result.Value)
			if err == nil {
				err = assign(reflect.ValueOf(record).Elem().FieldByName(GoName(f.Name)), value)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
		return nil
	}
}

func formulaOrder(fields []Field) ([]Field, error) {
	byName := map[string]Field{}
	for _, f := range fields {
		if f.Formula != "" || f.FormulaExpr != nil {
			byName[f.Name] = f
		}
	}
	visited, visiting := map[string]bool{}, map[string]bool{}
	out := []Field{}
	var walk func(string) error
	walk = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("field %s formula dependency is cyclic", name)
		}
		f, ok := byName[name]
		if !ok {
			return nil
		}
		visiting[name] = true
		if f.FormulaExpr != nil {
			var refs func(platform.Expr) error
			refs = func(e platform.Expr) error {
				if _, ok := byName[e.Ref]; ok {
					if err := walk(e.Ref); err != nil {
						return err
					}
				}
				for _, a := range e.Args {
					if err := refs(a); err != nil {
						return err
					}
				}
				if e.In != nil {
					return refs(*e.In)
				}
				return nil
			}
			if err := refs(*f.FormulaExpr); err != nil {
				return err
			}
		}
		delete(visiting, name)
		visited[name] = true
		out = append(out, f)
		return nil
	}
	for _, f := range fields {
		if err := walk(f.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func ruleLookup(o Object, lookup func(string) (platform.EntityInfo, bool)) func(string) (platform.EntityInfo, bool) {
	return func(typ string) (platform.EntityInfo, bool) {
		if typ != TypeOf(o.Name) {
			if lookup == nil {
				return platform.EntityInfo{}, false
			}
			return lookup(typ)
		}
		info := platform.EntityInfo{Type: typ, App: ID, Sets: o.Sets}
		for _, f := range o.Fields {
			read := f.Read
			if len(read) > 0 {
				read = append([]string{Builder}, read...)
			}
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Read: read})
		}
		if len(o.States) > 0 {
			info.Fields = append(info.Fields, platform.FieldInfo{Name: "state", Type: "choice"})
		}
		return info, true
	}
}
func ruleSpec(o Object, a Action, read *platform.RuleReadSpec) platform.RuleReadSpec {
	s := platform.RuleReadSpec{Object: TypeOf(o.Name), Inputs: map[string]platform.ExprType{}}
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
func legacyOperand(o Object, a Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
	f, err := conditionField(o, a, name, lookup)
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
func conditionExpr(o Object, a Action, cond Condition, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
	if cond.Expr != nil {
		if cond.Field != "" || cond.Operator != "" || cond.Value != "" || cond.ValueField != "" || cond.When != nil {
			return platform.Expr{}, fmt.Errorf("use one expression condition")
		}
		return *cond.Expr, nil
	}
	left, err := legacyOperand(o, a, cond.Field, lookup)
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
			right, err = legacyOperand(o, a, cond.ValueField, lookup)
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
		when, err := conditionExpr(o, a, *cond.When, lookup)
		if err != nil {
			return e, err
		}
		e = platform.Expr{Op: "or", Kind: "boolean", Args: []platform.Expr{{Op: "not", Kind: "boolean", Args: []platform.Expr{when}}, e}}
	}
	return e, nil
}

func actionCriteria(o Object, a Action, lookup func(string) (platform.EntityInfo, bool)) (*platform.RuleCriteria, error) {
	reads, conditions := a.Reads, a.Conditions
	if txn := a.Transaction; txn != nil {
		reads, conditions = txn.Reads, nil
		if txn.Guard != nil {
			conditions = []Condition{{Expr: txn.Guard, Message: txn.Message}}
		}
	}
	plan := &platform.RuleCriteria{Reads: ruleSpec(o, a, reads)}
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
		expr, err := conditionExpr(o, a, condition, ruleLookup(o, lookup))
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
			e, err := formulaExpr(o.Fields, *f)
			if err != nil {
				return err
			}
			f.FormulaExpr = &e
			f.Formula = ""
		}
	}
	for i := range o.Actions {
		a := &o.Actions[i]
		s := ruleSpec(*o, *a, a.Reads)
		for j := range a.Conditions {
			cond := &a.Conditions[j]
			if cond.Expr == nil {
				if cond.When != nil {
					if cond.When.When != nil {
						return fmt.Errorf("nested legacy guards require Expr")
					}
					if err := checkActionCondition(*o, *a, *cond.When, lookup, false); err != nil {
						return err
					}
				}
				if err := checkActionCondition(*o, *a, *cond, lookup, true); err != nil {
					return err
				}
			}
			e, err := conditionExpr(*o, *a, *cond, lookup)
			if err != nil {
				return err
			}
			*cond = Condition{Expr: &e, Message: cond.Message}
		}
		for j := range a.Sets {
			set := &a.Sets[j]
			f, err := conditionField(*o, *a, set.Field, lookup)
			if err != nil {
				return err
			}
			if set.Expr == nil {
				if !strings.HasPrefix(set.From, "=") && set.From != "$me" && set.From != "$now" {
					ix := -1
					for k, in := range a.Inputs {
						if in.Name == set.From {
							ix = k
							break
						}
					}
					if ix < 0 {
						return fmt.Errorf("action %s sets %s from %q, which is undeclared", a.Name, set.Field, set.From)
					}
					if !inputFits(a.Inputs[ix].Type, f.Type) {
						return fmt.Errorf("action %s assignment input %s does not fit %s", a.Name, set.From, f.Type)
					}
					if f.Type == "choice" {
						for _, option := range ValueChoices(a.Inputs[ix].Choices) {
							found := false
							for _, allowed := range f.Choices {
								if option == allowed {
									found = true
								}
							}
							if !found {
								return fmt.Errorf("input choice %s is not one of the field's choices", option)
							}
						}
					}
				}
			}
			e, err := platform.AssignmentExpr(f.Type, *set, s)
			if err != nil {
				return err
			}
			*set = Set{Field: set.Field, Expr: &e}
		}
	}
	if err := b.migrateObjectTransactions(o); err != nil {
		return err
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
	if _, err := formulaOrder(o.Fields); err != nil {
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
	for _, a := range o.Actions {
		s := ruleSpec(o, a, a.Reads)
		for _, cond := range a.Conditions {
			if cond.Expr != nil {
				if cond.Expr.Kind != "boolean" || cond.Message == "" {
					return fmt.Errorf("action %s guard needs boolean and a refusal message", a.Name)
				}
				if err := check(*cond.Expr, s, "action "+a.Name); err != nil {
					return err
				}
			}
		}
		for _, set := range a.Sets {
			if set.Expr != nil {
				f, err := conditionField(o, a, set.Field, lookup)
				if err != nil || !platform.RuleAssignmentFits(f.Type, set.Expr.Kind) {
					return fmt.Errorf("action %s assignment type differs", a.Name)
				}
				if err := check(*set.Expr, s, "action "+a.Name+" set "+set.Field); err != nil {
					return err
				}
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

const (
	SchemaTablePublish = TableType + ".publish"
	tableRows          = 500
	tableCols          = 16
)

func (b *Build) tableEntity() platform.Entity {
	return platform.Entity{Type: TableType, Title: "Decision table", Plural: "Decision tables", Model: Table{}, Display: "title", Description: "Condition columns to result columns; the first matching row decides. Published as a native operation.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			tb := record.(*Table)
			if err := tb.NormalizeRules(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if err := b.checkTableShape(*tb, c.Rules()); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "compute"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Check the table and install it as an operation at once.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "compute", Payload: []platform.Field{}, Do: b.publishTable}}}}
}

func (b *Build) publishTable(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	tb, ok := record.(*Table)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if err := tb.NormalizeRules(); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	if err := b.checkTable(*tb, c.Rules()); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if tb.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A table may retain at most 64 published versions")
	}
	tb.Version++
	frozen := *tb
	frozen.Published = ""
	frozen.Versions = nil
	if c.Staging() {
		if err := b.host.ValidateInstallOperation(c, frozen.Definition()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := b.installTable(c, frozen); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	raw, _ := json.Marshal(frozen)
	if tb.Published != "" && !slices.Contains(tb.Versions, tb.Published) {
		tb.Versions = append(tb.Versions, tb.Published)
	}
	tb.Published = string(raw)
	tb.Versions = append(tb.Versions, tb.Published)
	return nil
}

func (b *Build) installTable(c platform.Caller, tb Table) error {
	if err := b.host.InstallOperation(c, tb.Definition(), tb.Version); err != nil {
		return err
	}
	b.tables[tb.Name] = tb
	return nil
}

func (b *Build) installTables() error {
	list, err := readDefinitionInventory[Table](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return err
	}
	for _, record := range list {
		if record.Published == "" || record.Archived {
			continue
		}
		tb, ok := ReadPublication[Table](record.Published)
		if !ok || tb.Name != record.Name {
			return fmt.Errorf("decision table %s has a malformed publication", record.Name)
		}
		tb.Versions = record.Versions
		if err := b.installTable(platform.Caller{Replaying: true}, tb); err != nil {
			return err
		}
	}
	return nil
}

var columnName = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

func (b *Build) checkTable(tb Table, engines ...platform.RuleEngine) error {
	if err := b.checkName(tb.Name, tb.ID); err != nil {
		return err
	}
	return b.checkTableShape(tb, engines...)
}

func (b *Build) checkTableShape(tb Table, engines ...platform.RuleEngine) error {
	if strings.TrimSpace(tb.Title) == "" || len(tb.Inputs) == 0 || len(tb.Outputs) == 0 || len(tb.Inputs)+len(tb.Outputs) > tableCols || len(tb.Rows) == 0 || len(tb.Rows) > tableRows {
		return fmt.Errorf("a table needs a title, 1 to %d columns and 1 to %d rows", tableCols, tableRows)
	}
	seen := map[string]bool{}
	for _, col := range append(append([]TableColumn{}, tb.Inputs...), tb.Outputs...) {
		if !columnName.MatchString(col.Name) || seen[col.Name] || col.Type != "text" && col.Type != "number" && col.Type != "boolean" {
			return fmt.Errorf("column %q needs a unique lower-case name and a type of text, number or boolean", col.Name)
		}
		seen[col.Name] = true
	}
	plan, err := tb.RulePlan()
	if err != nil {
		return err
	}
	if err := b.checkRuleCollections(plan.Reads, "table "+tb.Name); err != nil {
		return err
	}
	for i, row := range plan.Rows {
		if row.Condition.Kind != "boolean" {
			return fmt.Errorf("row %d needs boolean", i+1)
		}
		if _, err := platform.RuleSchema(row.Condition, plan.Reads, b.conditionLookup, engines...); err != nil {
			return fmt.Errorf("row %d: %w", i+1, err)
		}
	}
	for i, row := range tb.Rows {
		if (len(row.When) != len(tb.Inputs) && len(row.Expressions) != len(tb.Inputs)) || len(row.Then) != len(tb.Outputs) {
			return fmt.Errorf("row %d does not fill every column", i+1)
		}
		for j, cell := range row.When {
			if _, err := ValueParseCondition(cell, tb.Inputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Inputs[j].Name, err)
			}
		}
		for j, cell := range row.Then {
			if _, err := platform.CastCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Outputs[j].Name, err)
			}
		}
	}
	if len(tb.Default) > 0 {
		if len(tb.Default) != len(tb.Outputs) {
			return fmt.Errorf("the default result fills every result column")
		}
		for j, cell := range tb.Default {
			if _, err := platform.CastCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("default, %s: %w", tb.Outputs[j].Name, err)
			}
		}
	}
	return nil
}

func (b *Build) Compute(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	tb, ok := b.tables[name]
	if !ok {
		return nil, fmt.Errorf("the builder has no native operation %s", name)
	}
	var values map[string]any
	if err := json.Unmarshal(input, &values); err != nil {
		return nil, fmt.Errorf("the input is not an object")
	}
	plan, err := tb.RulePlan()
	if err != nil {
		return nil, err
	}
	legacy := len(tb.Rows) > 0 && len(tb.Rows[0].Expressions) == 0
	return plan.Decide(platform.TableInput(plan.Reads, legacy, values), func(string) (platform.EntityInfo, bool) { return platform.EntityInfo{}, false })
}

func tableImage(image []byte) (Table, error) {
	var record Table
	if err := json.Unmarshal(image, &record); err != nil {
		return record, err
	}
	frozen, ok := ReadPublication[Table](record.Published)
	if !ok || record.State != "published" || frozen.ID != record.ID || frozen.Name != record.Name || frozen.Version < 1 || frozen.Version > record.Version || record.Version > 64 {
		return frozen, fmt.Errorf("accepted table has no matching immutable publication")
	}
	return frozen, nil
}

const TableType = "build.table"

type Table struct {
	platform.Record
	Name        string                 `json:"name" field:"required,search"`
	Title       string                 `json:"title" field:"required,search"`
	Description string                 `json:"description,omitempty" field:"search"`
	Reads       *platform.RuleReadSpec `json:"reads,omitempty" type:"json" field:"aside"`
	Inputs      []TableColumn          `json:"inputs" title:"Condition columns"`
	Outputs     []TableColumn          `json:"outputs" title:"Result columns"`
	Rows        []TableRow             `json:"rows"`
	Default     []string               `json:"default,omitempty" title:"Default result"`
	State       string                 `json:"state" field:"readonly"`
	Version     int                    `json:"version,omitempty" field:"readonly"`
	Versions    []string               `json:"versions,omitempty" field:"readonly" type:"json"`
	Published   string                 `json:"published,omitempty" field:"readonly" type:"json"`
}

type TableColumn struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type"`
}

type TableRow struct {
	Expressions []platform.Expr `json:"expressions,omitempty" type:"json" field:"aside"`
	When        []string        `json:"when,omitempty"`
	Then        []string        `json:"then"`
}

func ValueColumnSchema(cols []TableColumn) platform.ValueSchema {
	s := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, col := range cols {
		typ := map[string]string{"text": "string", "number": "number", "boolean": "boolean"}[col.Type]
		s.Properties[col.Name] = platform.ValueSchema{Type: typ, Description: col.Title}
		s.Required = append(s.Required, col.Name)
	}
	return s
}

func (tb Table) Definition() platform.Operation {
	plan, _ := tb.RulePlan()
	return platform.Operation{Rules: plan, Name: tb.Name, Title: tb.Title, Description: tb.Description, Input: ValueColumnSchema(tb.Inputs), Output: ValueColumnSchema(tb.Outputs), Roles: []string{Builder, User},
		Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 64 << 10, MaxOutputBytes: 16 << 10}}
}

type ValueCondition struct {
	expr *platform.Expr
	op   string
	a, b any
	set  []any
}

func ValueParseCondition(cell, typ string) (ValueCondition, error) {
	text := strings.TrimSpace(cell)
	if strings.HasPrefix(text, "{") {
		var e platform.Expr
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return ValueCondition{}, err
		}
		if e.Kind != "boolean" {
			return ValueCondition{}, fmt.Errorf("condition needs boolean Expr")
		}
		return ValueCondition{expr: &e}, nil
	}
	if text == "" || text == "*" {
		return ValueCondition{}, nil
	}
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		if rest, ok := strings.CutPrefix(text, op); ok {
			if (op == "<" || op == ">" || op == "<=" || op == ">=") && typ != "number" {
				return ValueCondition{}, fmt.Errorf("%q compares, which only a number column does", text)
			}
			v, err := ruleCellLiteral(rest, typ)
			return ValueCondition{op: op, a: v}, err
		}
	}
	if lo, hi, ok := strings.Cut(text, ".."); ok && typ == "number" {
		a, errA := ruleCellLiteral(lo, typ)
		b, errB := ruleCellLiteral(hi, typ)
		if errA != nil || errB != nil {
			return ValueCondition{}, fmt.Errorf("%q is not a range of numbers", text)
		}
		return ValueCondition{op: "range", a: a, b: b}, nil
	}
	if strings.Contains(text, "|") {
		c := ValueCondition{op: "in"}
		for _, part := range strings.Split(text, "|") {
			v, err := ruleCellLiteral(part, typ)
			if err != nil {
				return ValueCondition{}, err
			}
			c.set = append(c.set, v)
		}
		return c, nil
	}
	v, err := ruleCellLiteral(text, typ)
	return ValueCondition{op: "=", a: v}, err
}

func (c ValueCondition) Expr(name, kind string) platform.Expr {
	if c.expr != nil {
		return *c.expr
	}
	if c.op == "" {
		return platform.ExprLiteralValue("boolean", true)
	}
	e := platform.Expr{Op: c.op, Kind: "boolean", Args: []platform.Expr{platform.ExprReference(kind, "input."+name)}}
	if c.op == "in" {
		e.Args = append(e.Args, platform.Expr{Kind: "set", Lit: &platform.ExprLiteral{Element: kind, Value: platform.Raw(c.set)}})
	} else {
		e.Args = append(e.Args, platform.ExprLiteralValue(kind, c.a))
		if c.op == "range" {
			e.Args = append(e.Args, platform.ExprLiteralValue(kind, c.b))
		}
	}
	return e
}
func (tb Table) RulePlan() (*platform.RuleTablePlan, error) {
	s := platform.RuleReadSpec{Inputs: map[string]platform.ExprType{}}
	if tb.Reads != nil {
		s.Collections = tb.Reads.Collections
	}
	for _, col := range tb.Inputs {
		s.Inputs[col.Name] = platform.ExprType{Kind: col.Type}
	}
	p := &platform.RuleTablePlan{Reads: s}
	result := func(cells []string) (json.RawMessage, error) {
		if len(cells) != len(tb.Outputs) {
			return nil, fmt.Errorf("table result columns differ")
		}
		out := map[string]any{}
		for i, col := range tb.Outputs {
			v, err := platform.CastCell(cells[i], col.Type)
			if err != nil {
				return nil, err
			}
			out[col.Name] = v
		}
		return json.Marshal(out)
	}
	for _, row := range tb.Rows {
		args := row.Expressions
		if len(args) == 0 {
			if len(row.When) != len(tb.Inputs) {
				return nil, fmt.Errorf("table condition columns differ")
			}
			for j, col := range tb.Inputs {
				cond, err := ValueParseCondition(row.When[j], col.Type)
				if err != nil {
					return nil, err
				}
				args = append(args, cond.Expr(col.Name, col.Type))
			}
		}
		if len(args) != len(tb.Inputs) {
			return nil, fmt.Errorf("table condition columns differ")
		}
		e := args[0]
		if len(args) > 1 {
			e = platform.Expr{Op: "and", Kind: "boolean", Args: args}
		}
		raw, err := result(row.Then)
		if err != nil {
			return nil, err
		}
		p.Rows = append(p.Rows, platform.RuleTableRow{Condition: e, Result: raw})
	}
	if len(tb.Default) > 0 {
		raw, err := result(tb.Default)
		if err != nil {
			return nil, err
		}
		p.Default = raw
	}
	return p, nil
}
func (tb *Table) NormalizeRules() error {
	for i := range tb.Rows {
		row := &tb.Rows[i]
		if len(row.Expressions) > 0 {
			if len(row.When) > 0 {
				return fmt.Errorf("use one table condition representation")
			}
			continue
		}
		if len(row.When) != len(tb.Inputs) {
			return fmt.Errorf("table condition columns differ")
		}
		for j, col := range tb.Inputs {
			cond, err := ValueParseCondition(row.When[j], col.Type)
			if err != nil {
				return err
			}
			row.Expressions = append(row.Expressions, cond.Expr(col.Name, col.Type))
		}
		row.When = nil
	}
	return nil
}

func ruleCellLiteral(raw, kind string) (any, error) {
	if kind != "number" {
		return platform.CastCell(raw, kind)
	}
	text := strings.TrimSpace(raw)
	e := platform.ExprLiteralValue("number", text)
	_, err := platform.CompileExpr(e, platform.ExprSchema{})
	if err != nil {
		return nil, err
	}
	return text, nil
}
