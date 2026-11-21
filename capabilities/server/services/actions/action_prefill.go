package actions

import (
	"encoding/json"
	"time"

	ruleprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ontology"
)

type Matcher func(c platform.Caller, object string, fields map[string]any) string

func linkOrCreate(txn *platform.BusinessTxn, in platform.Field) (platform.TxnStep, bool) {
	for _, step := range txn.Steps {
		if when := step.When; step.Kind == "create" && step.Target.Type == in.Ref && when != nil && when.Op == "empty" && len(when.Args) == 1 && when.Args[0].Ref == "input."+in.Name {
			return step, true
		}
	}
	return platform.TxnStep{}, false
}

func prefill(c platform.Caller, transition platform.ActionType, id string, subject map[string]any, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool), match Matcher) map[string]any {
	out := map[string]any{}
	for _, rule := range transition.Rules {
		if rule.Transaction == nil || rule.Transaction.Spec == nil {
			continue
		}
		txn := rule.Transaction.Spec
		spec := transactionReadSpec(txn, transition.Parameters)
		inputs := map[string]any{"$subject": id}
		for _, in := range transition.Parameters {
			step, found := linkOrCreate(txn, in)
			info, known := lookup(in.Ref)
			if !found || !known {
				continue
			}
			fields := map[string]any{}
			for _, set := range step.Changes {
				field, _ := info.Field(set.Field)
				expr, err := ontology.AssignmentExpr(field.Type, set, spec)
				if err != nil {
					continue
				}
				if value, refusal := ruleprotocol.RuleAssignment(expr, spec, c, subject, inputs, now, lookup); refusal == nil {
					fields[set.Field] = value
				}
			}
			raw, _ := json.Marshal(fields)
			json.Unmarshal(raw, &fields)
			if existing := match(c, in.Ref, fields); existing != "" {
				out[in.Name] = existing
			}
		}
	}
	return out
}
