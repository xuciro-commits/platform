package objects

import (
	"bytes"
	"encoding/json"
	"slices"
	"time"

	"platformserver/platform"
)

type RecordSetResult struct {
	platform.RecordSetResult
	Total     *int                       `json:"total,omitempty"`
	Sums      map[string]json.RawMessage `json:"sums,omitempty"`
	SumErrors map[string]string          `json:"sumErrors,omitempty"`
}

func (t *Owner) RecordSetPage(m platform.Member, root platform.RecordTypeInfo, id, name string, now time.Time) (RecordSetResult, *platform.Error) {
	c := t.caller(t.authorizationMember(m), root.App)
	result, refusal := t.ReadRecordSetFrom(t.Store, c, root.Type, id, name, now)
	page := RecordSetResult{RecordSetResult: result}
	if refusal != nil {
		return page, refusal
	}
	if !slices.ContainsFunc(root.Sets, func(set platform.RecordSet) bool { return set.Name == name && set.Direction == "inverse" }) {
		return page, nil
	}
	total := len(result.Records)
	page.Total = &total
	if total == 0 {
		return page, nil
	}
	info, _ := t.entity(result.Object)
	fields := make([]string, 0, len(info.Fields))
	for _, field := range info.Fields {
		switch field.Type {
		case "integer", "decimal", "money":
			fields = append(fields, field.Name)
		}
	}
	if len(fields) == 0 {
		return page, nil
	}
	rows := make([]map[string]json.RawMessage, 0, total)
	hidden := map[string]bool{}
	ref := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: info.Type, Fields: fields}
	t.Store.Lock()
	authorize := t.AuthorizeRecordSet(t.Store, c, now)
	for _, raw := range result.Records {
		var row map[string]json.RawMessage
		_ = json.Unmarshal(raw, &row)
		_ = json.Unmarshal(row["id"], &ref.Record)
		decision := authorize(ref)
		if !decision.Allow {
			t.Store.Unlock()
			return RecordSetResult{}, authorizationRefusal(decision)
		}
		for _, name := range decision.Withheld {
			hidden[name] = true
		}
		rows = append(rows, row)
	}
	t.Store.Unlock()
	rules := c.Rules()
	page.Sums = map[string]json.RawMessage{}
	page.SumErrors = map[string]string{}
	for _, name := range fields {
		if hidden[name] {
			continue
		}
		field, _ := info.Field(name)
		fold, err := rules.NewExprNumericAggregate("sum", field.Type, "")
		operands := false
		if err == nil {
			for _, row := range rows {
				raw := row[name]
				if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
					continue
				}
				operands = true
				if err = fold.Add(platform.ExprValue{Kind: field.Type, Status: "value", Data: raw}); err != nil {
					break
				}
			}
		}
		if err == nil && (operands || field.Type != "money") {
			var value platform.ExprValue
			value, err = fold.Result()
			if err == nil {
				if field.Type == "money" {
					page.Sums[name] = value.Data
				} else {
					var number string
					if err = json.Unmarshal(value.Data, &number); err == nil {
						page.Sums[name] = json.RawMessage(number)
					}
				}
			}
		}
		if err != nil {
			page.SumErrors[name] = "The visible values cannot be summed"
		}
	}
	return page, nil
}
