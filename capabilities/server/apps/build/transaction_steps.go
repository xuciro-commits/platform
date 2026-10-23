package build

import "platformserver/internal/construction"

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
	"platformserver/internal/host"
	"platformserver/platform"
)

func (b *Build) checkTypedTxnStep(step platform.TxnStep, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool)) error {
	check := func(expr *platform.Expr, kinds ...string) error {
		if expr == nil {
			return nil
		}
		if len(kinds) > 0 && !slices.Contains(kinds, expr.Kind) {
			return fmt.Errorf("Transaction expression has an incompatible result type")
		}
		_, err := platform.RuleSchema(*expr, scope, lookup)
		return err
	}
	parameters := func(sets []platform.TxnSet) error {
		seen := map[string]bool{}
		for _, set := range sets {
			if !construction.Named(set.Field) || seen[set.Field] {
				return fmt.Errorf("Transaction parameters need distinct names")
			}
			seen[set.Field] = true
			expr, err := dispatchParameter(set, scope)
			if err != nil {
				return err
			}
			if err = check(&expr); err != nil {
				return err
			}
		}
		return nil
	}
	switch step.Kind {
	case "intervals":
		return b.checkTxnIntervals(step, scope, lookup)
	case "post":
		p := step.Post
		info, found := lookup(step.Target.Type)
		if p == nil || !found || info.App != construction.ID || len(p.Match) == 0 || step.Target.ID != "" || step.Target.Via != "" {
			return fmt.Errorf("Post steps need their owner's balance object and declared match fields")
		}
		field, found := info.Field(p.Field)
		if !found || !slices.Contains([]string{"integer", "decimal", "money"}, field.Type) || field.ReadOnly {
			return fmt.Errorf("Post steps need a writable numeric or money balance field")
		}
		if err := check(&p.Amount, platform.RuleFieldKind(field.Type)); err != nil {
			if field.Type != "decimal" || p.Amount.Kind != "integer" {
				return err
			}
			if _, err = platform.RuleSchema(p.Amount, scope, lookup); err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		for _, match := range p.Match {
			f, found := info.Field(match.Field)
			if !found || f.ReadOnly || seen[f.Name] || f.Name == p.Field {
				return fmt.Errorf("Post match fields must be distinct writable balance identifiers")
			}
			seen[f.Name] = true
			expr, err := assignmentExpr(f.Type, match, scope)
			if err != nil {
				return err
			}
			if err = check(&expr); err != nil {
				return err
			}
		}
	case "journal":
		j := step.Journal
		if j != nil && j.Reverses != "" {
			if !construction.Named(j.Reverses) || len(j.Lines) > 0 || j.Set != "" || j.ClosedPeriod != "" {
				return fmt.Errorf("A journal reversal names one original action and declares no new lines")
			}
			return check(j.Date, "date", "text")
		}
		if j == nil || len(j.Lines) < 2 || len(j.Lines) > 256 {
			return fmt.Errorf("Journal steps need two or more bounded accounting lines")
		}
		if j.ClosedPeriod != "" && j.ClosedPeriod != "reject" && j.ClosedPeriod != "next-open" {
			return fmt.Errorf("Journal steps need a known closed period policy")
		}
		if err := check(j.Date, "date", "text"); err != nil {
			return err
		}
		if err := check(j.Text, "text"); err != nil {
			return err
		}
		if err := check(j.Currency, "text"); err != nil {
			return err
		}
		if j.Set != "" {
			for name := range scope.Inputs {
				if strings.HasPrefix(name, "row.") {
					return fmt.Errorf("Journal collections cannot nest an iteration")
				}
			}
			info, _ := lookup(scope.Object)
			index := slices.IndexFunc(info.Sets, func(set platform.RecordSet) bool { return set.Name == j.Set })
			if index < 0 {
				return fmt.Errorf("Journal collection is undeclared")
			}
			item, found := lookup(info.Sets[index].Object)
			if !found || item.App != construction.ID {
				return fmt.Errorf("Journal collection must belong to its authority")
			}
			scope = withTxnRow(scope, item)
		}
		for _, line := range j.Lines {
			if err := check(line.When, "boolean"); err != nil {
				return err
			}
			if (line.Debit == nil) == (line.Credit == nil) {
				return fmt.Errorf("Each accounting line needs exactly one debit or credit")
			}
			if err := check(&line.Account, "text", "reference", "choice"); err != nil {
				return err
			}
			for _, expr := range []*platform.Expr{line.Debit, line.Credit} {
				if err := check(expr, "integer", "decimal"); err != nil {
					return err
				}
			}
			for _, expr := range []*platform.Expr{line.Text, line.Object, line.Partner} {
				if err := check(expr, "text", "reference", "choice"); err != nil {
					return err
				}
			}
		}
	case "effect":
		if step.Effect == nil || step.Effect.Writeback == "" {
			return fmt.Errorf("Effect steps need a declared transaction writeback")
		}
		if err := parameters(step.Effect.Payload); err != nil {
			return err
		}
		if b.host != nil {
			record, found := b.host.Record(construction.WritebackType + "/" + step.Effect.Writeback)
			w, valid := record.(construction.Writeback)
			if !found || !valid {
				return fmt.Errorf("Transaction writeback is unavailable")
			}
			w, err := b.ExecutionWriteback(w)
			if err != nil {
				return err
			}
			object := step.Target.Type
			if object == "" {
				object = scope.Object
			}
			if w.State != "published" || w.On != "transaction" || w.Object != object || len(w.Result) > 0 {
				return fmt.Errorf("Effect steps require a published transaction-only writeback without answer mutations")
			}
		}
	case "work":
		w := step.Work
		if w == nil || !w.Close && strings.TrimSpace(w.Title) == "" || (w.Role == "") == (w.Member == nil) && !w.Close {
			return fmt.Errorf("Work steps need a title and one declared recipient")
		}
		if (w.Structure == "") != (w.Unit == nil) || w.Unit != nil && (w.Role == "" || w.Member != nil) {
			return fmt.Errorf("Organizational work needs its original structure, unit and role")
		}
		if err := check(w.Unit, "text", "reference"); err != nil {
			return err
		}
		if err := check(w.Body, "text"); err != nil {
			return err
		}
		if err := check(w.Member, "text", "reference"); err != nil {
			return err
		}
		if err := check(w.Due, "datetime"); err != nil {
			return err
		}
		if err := check(w.Key, "text", "reference"); err != nil {
			return err
		}
	case "call":
		call := step.Call
		if call == nil || call.Ref.Check() != nil || call.Ref.Kind != platform.AssetQuery || call.Version < 0 || !slices.Contains([]string{"", "snapshot", "current"}, call.View) || !slices.Contains([]string{"text", "reference", "choice", "integer", "decimal", "boolean", "date", "datetime", "money"}, call.ResultType.Kind) {
			return fmt.Errorf("Call steps require a synchronous pure query, a snapshot/current view and a typed scalar result")
		}
		if err := parameters(call.Inputs); err != nil {
			return err
		}
		if call.Ref.App == construction.ID && call.Version > 0 {
			if _, selected := b.jointDeclarations[call.Ref]; selected {
				if _, version, valid := b.QueryDefinition(call.Ref.Name, call.Version); valid && version == call.Version {
					return nil
				}
			}
		}
		if b.host != nil {
			owner, ok := b.host.(host.TransactionHost)
			if !ok {
				return fmt.Errorf("The transaction query owner is unavailable")
			}
			if err := owner.CheckTxnCall(*call); err != nil {
				return err
			}
		}
	}
	return nil
}

func txnMath(kind, op string, left, right platform.ExprValue, now time.Time) (platform.ExprValue, *kernel.Error) {
	expr := platform.Expr{Op: op, Kind: kind, Args: []platform.Expr{platform.ExprReference(left.Kind, "left"), platform.ExprReference(right.Kind, "right")}}
	program, err := platform.CompileExpr(expr, platform.ExprSchema{Refs: map[string]platform.ExprType{"left": {Kind: left.Kind}, "right": {Kind: right.Kind}}})
	if err != nil {
		return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	result, err := program.Eval(platform.ExprContext{Refs: map[string]platform.ExprValue{"left": left, "right": right}, Now: now})
	if err != nil {
		return result.Value, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	return result.Value, nil
}

func (b *Build) txnPost(c platform.Caller, r *pb.ChangeRecord, step platform.TxnStep, scope platform.RuleReadSpec, subject any, inputs map[string]any, now time.Time, reserve func(string) *kernel.Error) (string, *kernel.Error) {
	p := step.Post
	entity, found := b.installed[step.Target.Type]
	if !found {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction balance object is unavailable")
	}
	info, _ := platform.Describe(construction.ID, entity, func(reflect.Type) string { return "" })
	patch := map[string]any{}
	keys := []string{}
	for _, match := range p.Match {
		field, _ := info.Field(match.Field)
		expr, err := assignmentExpr(field.Type, match, scope)
		if err != nil {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		value, problem := ruleAssignment(expr, scope, c, subject, inputs, now, b.conditionLookup)
		if problem != nil {
			return "", problem
		}
		patch[match.Field] = value
		keys = append(keys, constructionText(value))
	}
	id := BalanceID(entity.Type, keys)
	if err := reserve(entity.Type + "/" + id); err != nil {
		return "", err
	}
	rows, _, problem := c.FindOf(reflect.TypeOf(entity.Model), platform.Query{Domain: platform.Raw([]any{[]any{"id", "=", id}}), Limit: 1})
	if problem != nil {
		return "", problem
	}
	kind := platform.RuleFieldKind(func() string { field, _ := info.Field(p.Field); return field.Type }())
	verb := "create"
	current := platform.ExprPresent(kind, json.Number("0"))
	if len(rows) > 0 {
		verb = "edit"
		raw, problem := c.ReadRecord(entity.Type, id, now)
		if problem != nil {
			return "", problem
		}
		var values map[string]json.RawMessage
		_ = json.Unmarshal(raw, &values)
		if value := values[p.Field]; len(value) > 0 && string(value) != "null" {
			current = platform.ExprValue{Kind: kind, Status: "value", Data: value}
		}
	}
	amount, problem := evalRule(p.Amount, scope, c, subject, inputs, now, b.conditionLookup)
	if problem != nil {
		return "", problem
	}
	if kind == "money" && verb == "create" {
		var money map[string]json.RawMessage
		_ = json.Unmarshal(amount.Value.Data, &money)
		current = platform.ExprPresent("money", map[string]any{"amount": 0, "currency": json.RawMessage(money["currency"])})
	}
	op := "+"
	if p.Subtract {
		op = "-"
	}
	next, problem := txnMath(kind, op, current, amount.Value, now)
	if problem != nil {
		return "", problem
	}
	if p.Floor {
		zero := platform.ExprPresent(kind, 0)
		if kind == "money" {
			var money map[string]json.RawMessage
			_ = json.Unmarshal(next.Data, &money)
			zero = platform.ExprPresent("money", map[string]any{"amount": 0, "currency": json.RawMessage(money["currency"])})
		}
		comparison, err := platform.CompareExprValues(next, zero)
		if err != nil || comparison < 0 {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The balance cannot fall below zero")
		}
	}
	value, err := platform.ExprGoValue(next)
	if err != nil {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	patch[p.Field] = value
	return id, b.ledger.ApplyTxnRecord(c, r, entity, verb, id, platform.Raw(patch), now)
}

func constructionText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

func txnCallValue(call platform.TxnCall, raw json.RawMessage) (platform.ExprValue, *kernel.Error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The transaction query returned an invalid result")
	}
	for _, part := range call.ResultPath {
		switch current := value.(type) {
		case map[string]any:
			value = current[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(current) {
				value = nil
			} else {
				value = current[index]
			}
		default:
			value = nil
		}
	}
	if value == nil {
		return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The transaction query result path is missing")
	}
	return platform.ExprPresent(call.ResultType.Kind, value), nil
}

func (b *Build) txnParameters(c platform.Caller, sets []platform.TxnSet, scope platform.RuleReadSpec, subject any, inputs map[string]any, now time.Time) (json.RawMessage, *kernel.Error) {
	values := map[string]any{}
	for _, set := range sets {
		expr, err := dispatchParameter(set, scope)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		value, problem := ruleAssignment(expr, scope, c, subject, inputs, now, b.conditionLookup)
		if problem != nil {
			return nil, problem
		}
		values[set.Field] = value
	}
	return platform.Raw(values), nil
}

func (b *Build) txnJournal(c platform.Caller, r *pb.ChangeRecord, key string, spec platform.TxnJournal, scope platform.RuleReadSpec, subject any, inputs map[string]any, iterations *int, now time.Time) *kernel.Error {
	eval := func(expr *platform.Expr) (any, *kernel.Error) {
		if expr == nil {
			return "", nil
		}
		return ruleAssignment(*expr, scope, c, subject, inputs, now, b.conditionLookup)
	}
	entry := platform.Journal{Record: platform.Record{ID: "jnl-" + r.GetChangeId() + "-" + key}, Date: c.Today(now), Source: construction.ID + "/" + r.GetChangeId(), ClosedPeriod: spec.ClosedPeriod}
	if spec.Reverses != "" {
		if spec.Date != nil {
			value, err := eval(spec.Date)
			if err != nil {
				return err
			}
			entry.Date = constructionText(value)
		}
		return c.TxnTransport().QueueTxnJournal(c, r, key, entry, spec.Reverses, now)
	}
	for _, item := range []struct {
		expr  *platform.Expr
		value *string
	}{{spec.Date, &entry.Date}, {spec.Text, &entry.Text}, {spec.Currency, &entry.Currency}} {
		if item.expr != nil {
			value, err := eval(item.expr)
			if err != nil {
				return err
			}
			text := constructionText(value)
			if item.value != &entry.Date || text != "" {
				*item.value = text
			}
		}
	}
	rows := []map[string]any{inputs}
	if spec.Set != "" {
		id, _ := inputs["$subject"].(string)
		set, info, _, failure := b.txnRecordRows(c, r, scope.Object, id, spec.Set, key, iterations, now)
		if failure != nil {
			return failure
		}
		rows = make([]map[string]any, len(set.Records))
		for i, raw := range set.Records {
			rows[i], failure = txnRowInputs(c, info, raw, inputs)
			if failure != nil {
				return failure
			}
		}
		scope = withTxnRow(scope, info)
	}
	debit, credit := platform.ExprPresent("decimal", 0), platform.ExprPresent("decimal", 0)
	for _, inputs = range rows {
		for _, line := range spec.Lines {
			if line.When != nil {
				value, failure := evalRule(*line.When, scope, c, subject, inputs, now, b.conditionLookup)
				if failure != nil {
					return failure
				}
				if string(value.Value.Data) != "true" {
					continue
				}
			}
			if len(entry.Lines) == 256 {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Journal lines exceed their declared limit")
			}
			value := platform.JournalLine{}
			account, err := eval(&line.Account)
			if err != nil {
				return err
			}
			value.Account = constructionText(account)
			for _, item := range []struct {
				expr  *platform.Expr
				value *string
			}{{line.Text, &value.Text}, {line.Object, &value.Object}, {line.Partner, &value.Partner}} {
				if item.expr != nil {
					v, err := eval(item.expr)
					if err != nil {
						return err
					}
					*item.value = constructionText(v)
				}
			}
			amountExpr := line.Debit
			if amountExpr == nil {
				amountExpr = line.Credit
			}
			amount, err := evalRule(*amountExpr, scope, c, subject, inputs, now, b.conditionLookup)
			if err != nil {
				return err
			}
			zero := platform.ExprPresent(amount.Value.Kind, 0)
			cmp, compareErr := platform.CompareExprValues(amount.Value, zero)
			if compareErr != nil || cmp < 0 {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Accounting amounts cannot be negative")
			}
			v, convertErr := platform.ExprGoValue(amount.Value)
			if convertErr != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid accounting amount")
			}
			number, numberErr := json.Number(constructionText(v)).Float64()
			if numberErr != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid accounting amount")
			}
			if line.Debit != nil {
				value.Debit = number
				debit, err = txnMath("decimal", "+", debit, amount.Value, now)
			} else {
				value.Credit = number
				credit, err = txnMath("decimal", "+", credit, amount.Value, now)
			}
			if err != nil {
				return err
			}
			entry.Lines = append(entry.Lines, value)
		}
	}
	if len(entry.Lines) < 2 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A journal entry needs at least two accounting lines")
	}
	if cmp, err := platform.CompareExprValues(debit, credit); err != nil || cmp != 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Accounting debits and credits must balance")
	}
	return c.TxnTransport().QueueTxnJournal(c, r, key, entry, "", now)
}
