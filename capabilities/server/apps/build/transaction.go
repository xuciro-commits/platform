package build

import (
	"encoding/json"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

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
	return b.transactDepth(c, receipt, txn, now, 0, receipt.GetSubmission().GetSchema().GetName())
}

func (b *Build) transactDepth(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time, depth int, action string) *kernel.Error {
	if depth > 16 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Refusal routes exceed their limit / 拒绝改路超出上限")
	}
	var result *txnRefusalResult
	var failure *kernel.Error
	_, rollback := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		failure = b.transactAttempt(c, receipt, txn, now, &result, action)
		if result != nil && result.fact.Level == "route" {
			return receipt, failure
		}
		// Other refusals reach the original top-level owner, which discards
		// business state while retaining that decision's field diagnostics.
		return receipt, nil
	})
	if result == nil {
		if failure != nil {
			return failure
		}
		return rollback
	}
	if result.fact.Level == "block" {
		return c.RefuseBusiness(result.fact)
	}
	// The refused branch's private rows, numbers and intents have been discarded
	// by the original savepoint owner. Only the chosen branch can be accepted.
	if result.declaration.Record != nil {
		if failure := b.refusalRecord(c, receipt, *result.declaration.Record, result.scope, result.subject, result.inputs, now, result.fact.Record); failure != nil {
			return failure
		}
	}
	if failure := c.CaptureBusinessRefusal(receipt, result.fact); failure != nil {
		return failure
	}
	target := *result.fact.Route
	entity, ok := b.installed[target.Type]
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Refusal route object is unavailable / 拒绝改路目标对象不可用")
	}
	return b.ledger.ApplyTxnRoute(c, receipt, entity, target, now, func(c platform.Caller, routed *pb.ChangeRecord, alternative *platform.BusinessTxn, at time.Time) *kernel.Error {
		return b.transactDepth(c, routed, alternative, at, depth+1, target.Type+"."+target.Action)
	})
}

func (b *Build) transactAttempt(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time, refused **txnRefusalResult, action string) *kernel.Error {
	values, err := decodeTxnValues(receipt.GetSubmission().GetPayload())
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction inputs / 事务输入无效")
	}
	contextReceipt := proto.Clone(receipt).(*pb.ChangeRecord)
	contextReceipt.Submission.Schema.Name = action
	proposal, refusal := b.prepareProposal(c, contextReceipt, txn, now)
	if refusal != nil {
		return refusal
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
	original := platform.Raw(subject)
	spec := platform.RuleReadSpec{Object: txn.Subject, Inputs: map[string]platform.ExprType{"$subject": {Kind: "reference", Object: txn.Subject}}}
	values["$subject"] = subjectID
	if txn.Reads != nil {
		spec.Collections = txn.Reads.Collections
	}
	for _, transition := range b.installed[txn.Subject].Lifecycle.Transitions {
		if transition.Txn != nil && transition.Name == strings.TrimPrefix(action, txn.Subject+".") {
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
			return c.RefuseBusinessCondition("GUARD_NOT_SATISFIED", txn.Message, subject, values, now)
		}
	}
	originalSubject := maps.Clone(subject)
	originalInputs := maps.Clone(values)
	originalScope := spec
	originalScope.Inputs = maps.Clone(spec.Inputs)
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
		return platform.TxnRef{Type: ref.Type, ID: id, Action: ref.Action}, nil
	}
	reserve := func(ref string) *kernel.Error {
		if written[ref] {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction wrote {record} more than once / 事务重复写入 {record}", ref, ref)
		}
		written[ref] = true
		return nil
	}
	refusals := func(declarations []platform.Refusal, scope platform.RuleReadSpec, facts map[string]any, rowID, frame string) *kernel.Error {
		for _, declaration := range declarations {
			if declaration.When == nil || declaration.Level != "block" && declaration.Level != "flag" && declaration.Level != "route" || declaration.Level == "route" && declaration.Route == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business refusal declaration is incomplete / 业务拒绝声明不完整")
			}
			result, err := evalRule(*declaration.When, scope, c, subject, facts, now, b.conditionLookup)
			if err != nil {
				return err
			}
			if string(result.Value.Data) != "true" {
				continue
			}
			fact := platform.BusinessRefusalFact{Level: declaration.Level, Code: declaration.Code, Message: declaration.Message, Translations: maps.Clone(declaration.Translations), Step: frame, Subject: txn.Subject + "/" + subjectID, Action: receipt.GetSubmission().GetSchema().GetName(), Member: c.ID, At: now.UTC(), Original: original}
			if declaration.Escalation != nil {
				escalation := *declaration.Escalation
				fact.Escalation = &escalation
			}
			_ = json.Unmarshal(receipt.GetSubmission().GetPayload(), &fact.Inputs)
			if declaration.Record != nil {
				fact.Record = declaration.Record.Target.Type + "/" + receipt.GetChangeId() + "-refusal-" + strings.TrimPrefix(action, txn.Subject+".") + "-" + declaration.Code
				if frame != "" {
					fact.Record += "-" + strings.ReplaceAll(frame, "/", "-")
				}
			}
			if declaration.Route != nil {
				target, err := resolve(*declaration.Route, facts, rowID)
				if err != nil {
					return err
				}
				fact.Route = &target
			}
			if declaration.Level != "flag" {
				*refused = &txnRefusalResult{fact: fact, declaration: declaration, scope: scope, subject: subject, inputs: maps.Clone(facts)}
				if declaration.Level == "route" {
					(*refused).scope = originalScope
					(*refused).subject = originalSubject
					(*refused).inputs = originalInputs
				}
				return txnRefusalSignal
			}
			if declaration.Record == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A flagged refusal must leave a business record / 标记拒绝须留下业务记录")
			}
			if err := b.refusalRecord(c, receipt, *declaration.Record, scope, subject, facts, now, fact.Record); err != nil {
				return err
			}
			if err := c.CaptureBusinessRefusal(receipt, fact); err != nil {
				return err
			}
		}
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
			if err := refusals(step.Refusals, scope, facts, rowID, strings.TrimPrefix(frame+"/"+step.Name, "/")); err != nil {
				return err
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
			sources := []string{}
			for _, assignment := range step.Changes {
				field, _ := info.Field(assignment.Field)
				expr, err := assignmentExpr(field.Type, assignment, scope)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				value, failure := ruleAssignment(expr, scope, c, subject, facts, now, b.conditionLookup, &sources)
				if failure != nil {
					return failure
				}
				patch[assignment.Field] = value
			}
			if err := b.ledger.ApplyTxnRecord(c, receipt, proposalEntity(entity, patch, proposal, sources), verb, id, platform.Raw(patch), now); err != nil {
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
	if err := refusals(txn.Refusals, spec, values, "", ""); err != nil {
		return err
	}
	if err := run(txn.Steps, spec, values, "", ""); err != nil {
		return err
	}
	if err := b.finishProposal(c, receipt, proposal, now); err != nil {
		return err
	}
	return b.openConfirmation(c, receipt, txn, now)
}
