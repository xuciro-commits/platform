package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/construction"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func txnIterationSource(subject, of string, lookup func(string) (platform.EntityInfo, bool)) (platform.EntityInfo, error) {
	typ := subject
	if of != "" {
		field, err := platform.RecordPathField(subject, strings.Split(of, "."), lookup)
		if err != nil || field.Type != "reference" || field.Ref == "" {
			return platform.EntityInfo{}, fmt.Errorf("Iteration source must be a declared record reference")
		}
		typ = field.Ref
	}
	info, ok := lookup(typ)
	if !ok || info.App != construction.ID {
		return platform.EntityInfo{}, fmt.Errorf("Iteration source must belong to its transaction owner")
	}
	return info, nil
}

func (b *Build) txnRecordRows(c platform.Caller, receipt *pb.ChangeRecord, subject, id, name, step string, iterations *int, now time.Time) (platform.RecordSetResult, platform.EntityInfo, []string, *kernel.Error) {
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
	info, found := b.conditionLookup(set.Object)
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
		if !field.ReadsAny(c.RolesIn(info.App)) {
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
