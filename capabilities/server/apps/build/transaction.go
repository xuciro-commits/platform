package build

import (
	"encoding/json"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// setSource adapts only frozen legacy assignment inputs. New saved actions
// and transactions carry Expr; step facts remain named, typed private values.
func setSource(c platform.Caller, kind, from string, inputs map[string]any, now time.Time) (any, bool, error) {
	switch {
	case from == "$me":
		return c.ID, true, nil
	case from == "$now":
		if kind == "date" {
			return c.Today(now), true, nil
		}
		return now.UTC(), true, nil
	case strings.HasPrefix(from, "="):
		v, err := literal(kind, strings.TrimPrefix(from, "="))
		return v, true, err
	default:
		v, ok := inputs[from]
		return v, ok && v != nil, nil
	}
}

func decodeTxnValues(raw json.RawMessage) (map[string]any, error) {
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	err := decoder.Decode(&value)
	if value == nil {
		value = map[string]any{}
	}
	return value, err
}

func (b *Build) transact(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) *kernel.Error {
	values, err := decodeTxnValues(receipt.GetSubmission().GetPayload())
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction inputs / 事务输入无效")
	}
	subjectID := receipt.GetSubmission().GetTarget().GetId()
	readSubject := func() (map[string]any, *kernel.Error) {
		raw, refusal := c.ReadRecord(txn.Subject, subjectID, now)
		if refusal != nil {
			return nil, refusal
		}
		value, err := decodeTxnValues(raw)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction subject")
		}
		return value, nil
	}
	subject, refusal := readSubject()
	if refusal != nil {
		return refusal
	}
	spec := platform.RuleReadSpec{Object: txn.Subject, Inputs: map[string]platform.ExprType{}}
	if txn.Reads != nil {
		spec.Collections = txn.Reads.Collections
	}
	for _, transition := range b.installed[txn.Subject].Lifecycle.Transitions {
		if transition.Txn != nil && transition.Name == strings.TrimPrefix(receipt.GetSubmission().GetSchema().GetName(), txn.Subject+".") {
			for _, in := range transition.Payload {
				kind := platform.RuleFieldKind(in.Type)
				if in.Ref != "" {
					kind = "reference"
				} else if len(in.Choices) > 0 {
					kind = "choice"
				}
				if kind == "string" {
					kind = "text"
				}
				spec.Inputs[in.Name] = platform.ExprType{Kind: kind, Object: in.Ref}
			}
		}
	}
	if txn.Guard != nil {
		result, err := evalRule(*txn.Guard, spec, c, subject, values, now, b.conditionLookup)
		if err != nil {
			return err
		}
		if string(result.Value.Data) != "true" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{message}", txn.Message)
		}
	}
	written := map[string]bool{}
	iterations := 0
	resolve := func(ref platform.TxnRef, facts map[string]any, rowID string) (platform.TxnRef, *kernel.Error) {
		id := ref.ID
		switch {
		case id == "$subject":
			id = subjectID
		case id == "$item":
			id = rowID
		case strings.HasPrefix(id, "input.") || strings.HasPrefix(id, "step."):
			id, _ = facts[strings.TrimPrefix(id, "input.")].(string)
		}
		if ref.Via != "" {
			base, start, path := txn.Subject, subjectID, ref.Via
			if rest, row := strings.CutPrefix(path, "row."); row {
				path, start = rest, rowID
				base, _ = facts["row.object"].(string)
			}
			raw, _, err := c.ReadRecordPath(base, start, strings.Split(path, "."), now)
			if err != nil {
				return ref, err
			}
			_ = json.Unmarshal(raw, &id)
		}
		if id == "" {
			return ref, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE, "Transaction target is unavailable / 事务目标不可用")
		}
		return platform.TxnRef{Type: ref.Type, ID: id}, nil
	}
	reserve := func(ref string) *kernel.Error {
		if written[ref] {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction wrote {record} more than once / 事务重复写入 {record}", ref, ref)
		}
		written[ref] = true
		return nil
	}
	var run func([]platform.TxnStep, platform.RuleReadSpec, map[string]any, string, string) *kernel.Error
	run = func(steps []platform.TxnStep, scope platform.RuleReadSpec, facts map[string]any, rowID, frame string) *kernel.Error {
		for _, step := range steps {
			c.Trace(platform.DecisionTrace{Kind: "transaction-step", Name: strings.TrimPrefix(frame+"/"+step.Name, "/"), Operation: step.Kind})
			if step.When != nil {
				result, err := evalRule(*step.When, scope, c, subject, facts, now, b.conditionLookup)
				if err != nil {
					return err
				}
				if string(result.Value.Data) != "true" {
					continue
				}
			}
			if step.ForEach != nil {
				set, err := c.ReadRecordSet(txn.Subject, subjectID, step.ForEach.Set, now)
				if err != nil {
					return err
				}
				if set.Partial {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Iteration {step} contains withheld records / 迭代 {step} 含不可见记录", step.Name, step.Name)
				}
				iterations += len(set.Records)
				if iterations > platform.TxnMaxIterations {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Iteration {step} has {count} records; limit is {limit} / 迭代 {step} 超出上限", step.Name, iterations, platform.TxnMaxIterations, step.Name)
				}
				info, _ := b.conditionLookup(set.Object)
				rowScope := withTxnRow(scope, info)
				ids := make([]string, len(set.Records))
				for i, raw := range set.Records {
					var identity struct{ ID string }
					_ = json.Unmarshal(raw, &identity)
					ids[i] = identity.ID
				}
				if err := c.CaptureTxnIteration(receipt, platform.TxnIteration{Step: step.Name, Set: step.ForEach.Set, Object: set.Object, Definition: set.Definition, IDs: ids}); err != nil {
					return err
				}
				for i, raw := range set.Records {
					var fields map[string]json.RawMessage
					if json.Unmarshal(raw, &fields) != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Iteration row is invalid")
					}
					rowFacts := maps.Clone(facts)
					rowFacts["row.object"] = set.Object
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
						rowFacts["row."+field.Name] = fact
					}
					if err := run(step.ForEach.Steps, platform.RuleReadSpec{Object: rowScope.Object, Inputs: maps.Clone(rowScope.Inputs), Collections: rowScope.Collections}, rowFacts, ids[i], step.Name+"-"+strconv.Itoa(i)); err != nil {
						return err
					}
				}
				continue
			}
			if step.Kind == "sequence" {
				number, err := c.Next(receipt, step.Sequence.Name, now)
				if err != nil {
					return err
				}
				facts["step."+step.Name] = number
				txnResult(&scope, step)
				continue
			}
			if step.Link != nil {
				request := *step.Link
				request.Parent, refusal = resolve(request.Parent, facts, rowID)
				if refusal != nil {
					return refusal
				}
				request.Child, refusal = resolve(request.Child, facts, rowID)
				if refusal != nil {
					return refusal
				}
				link, err := b.transactionLink(request.Binding)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
				}
				if link.Storage == "edge" {
					identity := request.Binding.Ref.String() + "/" + request.Parent.ID + "/" + request.Child.ID
					if err := reserve(identity); err != nil {
						return err
					}
					if err := c.PutOwnedRelation(receipt, request, step.Kind == "unlink", now); err != nil {
						return err
					}
				} else {
					if err := reserve(request.Child.Type + "/" + request.Child.ID); err != nil {
						return err
					}
					value := request.Parent.ID
					if step.Kind == "unlink" {
						raw, err := c.ReadRecord(request.Child.Type, request.Child.ID, now)
						if err != nil {
							return err
						}
						var fields map[string]json.RawMessage
						_ = json.Unmarshal(raw, &fields)
						var parent string
						_ = json.Unmarshal(fields[link.Via], &parent)
						if parent != value {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Relationship does not exist")
						}
						value = ""
					}
					if err := b.ledger.ApplyTxnRecord(c, receipt, b.installed[request.Child.Type], "edit", request.Child.ID, platform.Raw(map[string]string{link.Via: value}), now); err != nil {
						return err
					}
				}
				continue
			}
			entity, ok := b.installed[step.Target.Type]
			if !ok {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction object is unavailable / 事务对象不可用")
			}
			id, verb := "", "edit"
			if step.Kind == "create" {
				verb, id = "create", receipt.GetChangeId()+"-"+step.Name
				if frame != "" {
					id += "-" + frame
				}
			} else {
				ref, err := resolve(step.Target, facts, rowID)
				if err != nil {
					return err
				}
				id = ref.ID
				if step.Kind == "archive" {
					verb = "archive"
				}
			}
			if err := reserve(entity.Type + "/" + id); err != nil {
				return err
			}
			info, _ := platform.Describe(ID, entity, func(reflect.Type) string { return "" })
			patch := map[string]any{}
			for _, assignment := range step.Changes {
				field, _ := info.Field(assignment.Field)
				expr, err := assignmentExpr(field.Type, assignment, scope)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				value, failure := ruleAssignment(expr, scope, c, subject, facts, now, b.conditionLookup)
				if failure != nil {
					return failure
				}
				patch[assignment.Field] = value
			}
			if err := b.ledger.ApplyTxnRecord(c, receipt, entity, verb, id, platform.Raw(patch), now); err != nil {
				return err
			}
			if step.Kind == "create" {
				facts["step."+step.Name] = id
				txnResult(&scope, step)
			}
			if entity.Type == txn.Subject && id == subjectID {
				var err *kernel.Error
				subject, err = readSubject()
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	return run(txn.Steps, spec, values, "", "")
}
