package business

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Runtime) txnPost(c platform.Caller, r *pb.ChangeRecord, step platform.TxnStep, scope platform.RuleReadSpec, subject any, inputs map[string]any, now time.Time, reserve func(string) *kernel.Error) (string, *kernel.Error) {
	p := step.Post
	entity, found := b.Entities[step.Target.Type]
	if !found {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction balance object is unavailable")
	}
	info, _ := platform.Describe(b.App, entity, func(reflect.Type) string { return "" })
	patch := map[string]any{}
	keys := []string{}
	for _, match := range p.Match {
		field, _ := info.Field(match.Field)
		expr, err := platform.AssignmentExpr(field.Type, match, scope)
		if err != nil {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		value, problem := platform.RuleAssignment(expr, scope, c, subject, inputs, now, b.Lookup)
		if problem != nil {
			return "", problem
		}
		patch[match.Field] = value
		keys = append(keys, constructionText(value))
	}
	id := platform.BalanceID(entity.Type, keys)
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
	amount, problem := platform.EvaluateRule(p.Amount, scope, c, subject, inputs, now, b.Lookup)
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
	next, problem := txnMath(c.Rules(), kind, op, current, amount.Value, now)
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
		comparison, err := c.Rules().CompareExprValues(next, zero)
		if err != nil || comparison < 0 {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The balance cannot fall below zero")
		}
	}
	value, err := platform.ExprGoValue(next)
	if err != nil {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	patch[p.Field] = value
	return id, b.Ledger.ApplyTxnRecord(c, r, entity, verb, id, platform.Raw(patch), now)
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

func (b *Runtime) txnParameters(c platform.Caller, sets []platform.TxnSet, scope platform.RuleReadSpec, subject any, inputs map[string]any, now time.Time) (json.RawMessage, *kernel.Error) {
	values := map[string]any{}
	for _, set := range sets {
		expr, err := platform.DispatchParameter(set, scope)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		value, problem := platform.RuleAssignment(expr, scope, c, subject, inputs, now, b.Lookup)
		if problem != nil {
			return nil, problem
		}
		values[set.Field] = value
	}
	return platform.Raw(values), nil
}
