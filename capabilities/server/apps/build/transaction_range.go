package build

import (
	"fmt"
	"maps"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func withTxnRange(scope platform.RuleReadSpec, kind string) platform.RuleReadSpec {
	scope.Inputs = maps.Clone(scope.Inputs)
	scope.Inputs["row.start"] = platform.ExprType{Kind: kind}
	scope.Inputs["row.end"] = platform.ExprType{Kind: kind}
	scope.Inputs["row.index"] = platform.ExprType{Kind: "integer"}
	return scope
}

func checkTxnRange(span platform.TxnRange, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool)) error {
	if span.From.Kind != "date" && span.From.Kind != "datetime" && span.From.Kind != "integer" || span.Until.Kind != span.From.Kind || span.Unit != "day" && span.Unit != "hour" && span.Unit != "item" || (span.From.Kind == "integer") != (span.Unit == "item") || span.Unit == "hour" && span.From.Kind != "datetime" || span.Step < 1 || span.Step > platform.TxnMaxIterations || span.Limit < 1 || span.Limit > platform.TxnMaxIterations {
		return fmt.Errorf("Range iteration needs matching integers, dates or timestamps, a compatible step and a bounded limit")
	}
	for _, expr := range []platform.Expr{span.From, span.Until} {
		if _, err := platform.RuleSchema(expr, scope, lookup); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) txnRangeRows(c platform.Caller, span platform.TxnRange, scope platform.RuleReadSpec, subject any, inputs map[string]any, now time.Time) ([]map[string]any, *kernel.Error) {
	invalid := func() ([]map[string]any, *kernel.Error) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Range iteration needs an ordered, aligned interval within its declared limit / 范围迭代需要顺序正确、步长对齐且未超出上限的区间")
	}
	if span.Step < 1 || span.Step > platform.TxnMaxIterations || span.Limit < 1 || span.Limit > platform.TxnMaxIterations {
		return invalid()
	}
	if span.From.Kind == "integer" {
		var bounds [2]int64
		for i, expr := range []platform.Expr{span.From, span.Until} {
			value, failure := ruleAssignment(expr, scope, c, subject, inputs, now, b.conditionLookup)
			if failure != nil {
				return nil, failure
			}
			number, err := strconv.ParseInt(constructionText(value), 10, 64)
			if err != nil {
				return invalid()
			}
			bounds[i] = number
		}
		if bounds[0] >= bounds[1] {
			return invalid()
		}
		rows := make([]map[string]any, 0)
		for cursor := bounds[0]; cursor < bounds[1]; {
			next := cursor + int64(span.Step)
			if len(rows) == span.Limit || next <= cursor || next > bounds[1] {
				return invalid()
			}
			rows = append(rows, map[string]any{"row.start": cursor, "row.end": next, "row.index": len(rows)})
			cursor = next
		}
		return rows, nil
	}
	layout := time.DateOnly
	if span.From.Kind == "datetime" {
		layout = time.RFC3339Nano
	}
	var bounds [2]time.Time
	for i, expr := range []platform.Expr{span.From, span.Until} {
		value, failure := ruleAssignment(expr, scope, c, subject, inputs, now, b.conditionLookup)
		if failure != nil {
			return nil, failure
		}
		text, ok := value.(string)
		if !ok {
			return invalid()
		}
		bound, err := time.Parse(layout, text)
		if err != nil {
			return invalid()
		}
		bounds[i] = bound.UTC()
	}
	if !bounds[0].Before(bounds[1]) {
		return invalid()
	}
	rows := make([]map[string]any, 0)
	for cursor := bounds[0]; cursor.Before(bounds[1]); {
		next := cursor.AddDate(0, 0, span.Step)
		if span.Unit == "hour" {
			next = cursor.Add(time.Duration(span.Step) * time.Hour)
		}
		if len(rows) == span.Limit || next.After(bounds[1]) {
			return invalid()
		}
		rows = append(rows, map[string]any{"row.start": cursor.Format(layout), "row.end": next.Format(layout), "row.index": len(rows)})
		cursor = next
	}
	return rows, nil
}
