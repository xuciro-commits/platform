package business

import (
	"encoding/json"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"strconv"
	"time"
)

func (b *Runtime) txnIntervals(c platform.Caller, receipt *pb.ChangeRecord, subject, subjectID, step string, p platform.TxnIntervals, iterations *int, now time.Time, reserve func(string) *kernel.Error) *kernel.Error {
	proof := c.ObservationContext()
	if proof == nil || proof.Intervals == nil || proof.Record != subject+"/"+subjectID || proof.Definition.Ref != (platform.AssetRef{App: b.App, Kind: platform.AssetObservation, Name: p.Source}) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Interval projection requires its accepted observation / 区间写入须有对应观测依据")
	}
	set, info, ids, failure := b.txnRecordRows(c, receipt, subject, subjectID, p.Set, step, iterations, now)
	if failure != nil {
		return failure
	}
	entity, installed := b.Entities[info.Type]
	if !installed {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Interval output object is unavailable")
	}
	for name := range platform.IntervalFields(p.Fields) {
		field, found := info.Field(name)
		if !found || !field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Interval projection contains withheld fields / 区间集合含不可见字段")
		}
	}
	previous := make([]platform.IntervalIdentity, 0, len(ids))
	reviews := map[string]bool{}
	for i, raw := range set.Records {
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Invalid interval projection row")
		}
		var owner, source string
		var retired, review bool
		var interval platform.ObservationInterval
		for name, into := range map[string]any{p.Fields.Subject: &owner, p.Fields.Source: &source, p.Fields.Retired: &retired, p.Fields.Review: &review, p.Fields.Since: &interval.Since, p.Fields.Until: &interval.Until, p.Fields.OpenStart: &interval.OpenStart, p.Fields.OpenEnd: &interval.OpenEnd} {
			if len(row[name]) == 0 || string(row[name]) == "null" || json.Unmarshal(row[name], into) != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Invalid interval projection field {field}", name)
			}
		}
		if owner != subjectID || source != p.Source || retired {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Interval collection contains another source or retired identity")
		}
		previous = append(previous, platform.IntervalIdentity{ID: ids[i], Interval: interval})
		reviews[ids[i]] = review
	}
	plan, err := platform.ReconcileObservationIntervals(info.Type, previous, *proof.Intervals, func(i int) string { return receipt.GetChangeId() + "-" + step + "-" + strconv.Itoa(i) })
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	write := func(id, verb string, patch map[string]any) *kernel.Error {
		if failure := c.TxnTransport().TakeTxnStep(); failure != nil {
			return failure
		}
		if failure := reserve(info.Type + "/" + id); failure != nil {
			return failure
		}
		return b.Ledger.ApplyTxnRecord(c, receipt, entity, verb, id, platform.Raw(patch), now)
	}
	for _, event := range plan.Events {
		if !event.Changed && (!event.Review || reviews[event.ID]) {
			continue
		}
		patch := map[string]any{p.Fields.Subject: subjectID, p.Fields.Source: p.Source, p.Fields.SourceVersion: proof.Definition.SourceVersion, p.Fields.SourceRevision: proof.Intervals.Revision, p.Fields.ProcessingVersion: proof.Intervals.Version, p.Fields.Since: event.Interval.Since, p.Fields.Until: event.Interval.Until, p.Fields.OpenStart: event.Interval.OpenStart, p.Fields.OpenEnd: event.Interval.OpenEnd, p.Fields.Retired: false}
		verb := "edit"
		if event.New {
			verb = "create"
			patch[p.Fields.Review] = event.Review
		} else if event.Review {
			patch[p.Fields.Review] = true
		}
		if failure := write(event.ID, verb, patch); failure != nil {
			return failure
		}
	}
	for _, id := range plan.Retired {
		if failure := write(id, "edit", map[string]any{p.Fields.Retired: true}); failure != nil {
			return failure
		}
	}
	for _, redirect := range plan.Redirects {
		for _, to := range redirect.To {
			if failure := c.TxnTransport().TakeTxnStep(); failure != nil {
				return failure
			}
			link := platform.TxnLink{Binding: p.Predecessors, Parent: platform.TxnRef{Type: info.Type, ID: redirect.From.Id}, Child: platform.TxnRef{Type: info.Type, ID: to.Id}}
			if failure := c.PutOwnedRelation(receipt, link, false, now); failure != nil {
				return failure
			}
		}
	}
	return nil
}

func txnMath(engine platform.RuleEngine, kind, op string, left, right platform.ExprValue, now time.Time) (platform.ExprValue, *kernel.Error) {
	expr := platform.Expr{Op: op, Kind: kind, Args: []platform.Expr{platform.ExprReference(left.Kind, "left"), platform.ExprReference(right.Kind, "right")}}
	program, err := engine.CompileExpr(expr, platform.ExprSchema{Refs: map[string]platform.ExprType{"left": {Kind: left.Kind}, "right": {Kind: right.Kind}}})
	if err != nil {
		return platform.ExprValue{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	result, err := program.Eval(platform.ExprContext{Refs: map[string]platform.ExprValue{"left": left, "right": right}, Now: now})
	if err != nil {
		return result.Value, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	return result.Value, nil
}

func (b *Runtime) txnRangeRows(c platform.Caller, span platform.TxnRange, scope platform.RuleReadSpec, subject any, inputs map[string]any, now time.Time) ([]map[string]any, *kernel.Error) {
	invalid := func() ([]map[string]any, *kernel.Error) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Range iteration needs an ordered, aligned interval within its declared limit / 范围迭代需要顺序正确、步长对齐且未超出上限的区间")
	}
	if span.Step < 1 || span.Step > platform.TxnMaxIterations || span.Limit < 1 || span.Limit > platform.TxnMaxIterations {
		return invalid()
	}
	if span.From.Kind == "integer" {
		var bounds [2]int64
		for i, expr := range []platform.Expr{span.From, span.Until} {
			value, failure := platform.RuleAssignment(expr, scope, c, subject, inputs, now, b.Lookup)
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
		value, failure := platform.RuleAssignment(expr, scope, c, subject, inputs, now, b.Lookup)
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

func (b *Runtime) txnRecordRows(c platform.Caller, receipt *pb.ChangeRecord, subject, id, name, step string, iterations *int, now time.Time) (platform.RecordSetResult, platform.EntityInfo, []string, *kernel.Error) {
	set, failure := c.ReadRecordSet(subject, id, name, now)
	if failure != nil {
		return set, platform.EntityInfo{}, nil, failure
	}
	if set.Partial {
		return set, platform.EntityInfo{}, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Iteration {step} contains withheld records / 迭代 {step} 含不可见记录", step, step)
	}
	*iterations += len(set.Records)
	if *iterations > platform.TxnMaxIterations {
		return set, platform.EntityInfo{}, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction iteration exceeds its limit / 事务迭代超出上限")
	}
	info, found := b.Lookup(set.Object)
	if !found {
		return set, info, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Iteration object is unavailable")
	}
	ids := make([]string, len(set.Records))
	for i, raw := range set.Records {
		var identity struct{ ID string }
		if json.Unmarshal(raw, &identity) != nil {
			return set, info, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Iteration row is invalid")
		}
		ids[i] = identity.ID
	}
	failure = c.CaptureTxnIteration(receipt, platform.TxnIteration{Step: step, Set: name, Object: set.Object, Definition: set.Definition, IDs: ids})
	return set, info, ids, failure
}

func txnRowInputs(c platform.Caller, info platform.EntityInfo, raw json.RawMessage, inputs map[string]any) (map[string]any, *kernel.Error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Iteration row is invalid")
	}
	values := maps.Clone(inputs)
	values["row.object"] = info.Type
	for _, field := range platform.ConstraintEntity(info).Fields {
		fact := platform.ExprValue{Kind: platform.RuleFieldKind(field.Type), Status: "missing"}
		if !field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
			fact.Status = "withheld"
		} else if value, ok := fields[field.Name]; ok {
			fact.Status, fact.Data = "value", value
			if string(value) == "null" {
				fact.Status = "null"
			}
		}
		values["row."+field.Name] = fact
	}
	values["row.$subject"] = platform.ExprValue{Kind: "reference", Status: "value", Data: fields["id"]}
	return values, nil
}
