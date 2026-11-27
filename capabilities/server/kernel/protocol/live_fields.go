package protocol

import (
	"encoding/json"
	"errors"
	"platformserver/platform"
	"time"
)

// LiveField uses the same captured context and expression engine as business
// guards. Nothing produced here is part of the accepted record image.
func LiveField(c platform.Caller, info platform.RecordTypeInfo, field platform.FieldInfo, record any, lookup func(string) (platform.RecordTypeInfo, bool), at time.Time) (platform.ExprValue, []string) {
	kind := platform.RuleFieldKind(field.Type)
	value := platform.ExprValue{Kind: kind, Status: "withheld"}
	if !field.ReadsAny(c.RolesIn(info.Type), c.Rules()) {
		return value, nil
	}
	expr, ok := info.LiveRules[field.Name]
	if !ok {
		return value, nil
	}
	var header struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(platform.Raw(record), &header)
	if header.ID != "" && c.HasRuntime() && !c.Replaying {
		d := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: info.Type, Record: header.ID, Fields: []string{field.Name}}, at)
		if !d.Allow || len(d.Withheld) > 0 {
			return value, nil
		}
	}
	// Share the caller's bounded nesting budget with creation transactions.
	if c.CreationDepth() >= 16 {
		return value, nil
	}
	c = c.Deeper()
	spec := platform.RuleReadSpec{Object: info.Type}
	if info.LiveReads != nil {
		spec.Collections = info.LiveReads.Collections
	}
	if refusal := ruleSubjectPermissions(expr, spec, c, record, at); refusal != nil {
		return value, nil
	}
	schema, err := RuleSchema(expr, spec, lookup, c.Rules())
	if err != nil {
		return value, nil
	}
	program, err := c.Rules().CompileExpr(expr, schema)
	if err != nil {
		return value, nil
	}
	ctx, err := RuleContext(c, expr, spec, lookup, record, nil, at)
	if err != nil {
		return value, nil
	}
	if ruleWithheld(expr, ctx) {
		return value, ctx.Sources
	}
	result, err := program.Eval(ctx)
	if err != nil {
		var issue *platform.ExprIssue
		if errors.As(err, &issue) && issue.Code == "missing" {
			value.Status = "null"
		}
		return value, ctx.Sources
	}
	value = result.Value
	if value.Status == "missing" {
		value.Status = "null"
	}
	return value, ctx.Sources
}
