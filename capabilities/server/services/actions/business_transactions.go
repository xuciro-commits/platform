package business

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var txnRefusalSignal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transaction chose a refusal branch")

type txnRefusalResult struct {
	fact        platform.BusinessRefusalFact
	declaration platform.Refusal
	scope       platform.RuleReadSpec
	subject     map[string]any
	inputs      map[string]any
}

func (b *Runtime) refusalRecord(c platform.Caller, receipt *pb.ChangeRecord, step platform.TxnStep, scope platform.RuleReadSpec, subject map[string]any, inputs map[string]any, now time.Time, ref string) *kernel.Error {
	entity, ok := b.Entities[step.Target.Type]
	if !ok || step.Kind != "create" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Refusal records need a declared create step / 拒绝记录须为已声明的创建步骤")
	}
	_, id, ok := strings.Cut(ref, "/")
	if !ok || id == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Refusal record identity is unavailable / 拒绝记录标识不可用")
	}
	info, _ := platform.Describe(b.App, entity, func(reflect.Type) string { return "" })
	patch := map[string]any{}
	for _, assignment := range step.Changes {
		field, _ := info.Field(assignment.Field)
		expr, err := platform.AssignmentExpr(field.Type, assignment, scope)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		value, failure := platform.RuleAssignment(expr, scope, c, subject, inputs, now, b.Lookup)
		if failure != nil {
			return failure
		}
		patch[assignment.Field] = value
	}
	return b.Ledger.ApplyTxnRecord(c, receipt, entity, "create", id, platform.Raw(patch), now)
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

func (b *Runtime) Transact(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) *kernel.Error {
	return b.transactDepth(c, receipt, txn, now, 0, c.TransactionInput(receipt).GetSchema().GetName())
}

func (b *Runtime) transactDepth(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time, depth int, action string) *kernel.Error {
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
	if result.declaration.Record != nil {
		if failure := b.refusalRecord(c, receipt, *result.declaration.Record, result.scope, result.subject, result.inputs, now, result.fact.Record); failure != nil {
			return failure
		}
	}
	if failure := c.CaptureBusinessRefusal(receipt, result.fact); failure != nil {
		return failure
	}
	target := *result.fact.Route
	entity, ok := b.Entities[target.Type]
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Refusal route object is unavailable / 拒绝改路目标对象不可用")
	}
	return b.Ledger.ApplyTxnRoute(c, receipt, entity, target, now, func(c platform.Caller, routed *pb.ChangeRecord, alternative *platform.BusinessTxn, at time.Time) *kernel.Error {
		return b.transactDepth(c, routed, alternative, at, depth+1, target.Type+"."+target.Action)
	})
}

func (b *Runtime) transactAttempt(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time, refused **txnRefusalResult, action string) *kernel.Error {
	transport := c.TxnTransport()
	if transport == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction transport is unavailable")
	}
	invocation := c.TransactionInput(receipt)
	values, err := decodeTxnValues(invocation.GetPayload())
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction inputs / 事务输入无效")
	}
	contextReceipt := proto.Clone(receipt).(*pb.ChangeRecord)
	contextReceipt.Submission = invocation
	contextReceipt.Submission.Schema.Name = action
	proposal, refusal := b.Prepare(c, contextReceipt, txn, now)
	if refusal != nil {
		return refusal
	}
	subjectID := invocation.GetTarget().GetId()
	framePrefix := ""
	if invocation.GetTarget().GetType() != receipt.GetSubmission().GetTarget().GetType() || subjectID != receipt.GetSubmission().GetTarget().GetId() {
		digest := sha256.Sum256(platform.Raw([]string{txn.Subject, subjectID}))
		framePrefix = fmt.Sprintf("create-%x", digest[:8])
	}
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
	var calls platform.TxnCalls
	usesCalls := false
	platform.WalkBusinessTxn(txn, func(step platform.TxnStep) { usesCalls = usesCalls || step.Call != nil })
	if usesCalls {
		transport := c.TxnTransport()
		if transport == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction calls require their original owner")
		}
		var problem *kernel.Error
		calls, problem = transport.BeginTxnCalls(c, now)
		if problem != nil {
			return problem
		}
	}
	spec := platform.RuleReadSpec{Object: txn.Subject, Inputs: map[string]platform.ExprType{"$subject": {Kind: "reference", Object: txn.Subject}}}
	values["$subject"] = subjectID
	if txn.Reads != nil {
		spec.Collections = txn.Reads.Collections
	}
	var transitions []platform.Transition
	if life := b.Entities[txn.Subject].Lifecycle; life != nil {
		transitions = life.Transitions
	}
	for _, transition := range transitions {
		if transition.Txn != nil && transition.Name == strings.TrimPrefix(action, txn.Subject+".") {
			var submitted map[string]any
			if json.Unmarshal(invocation.GetPayload(), &submitted) != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction inputs / 事务输入无效")
			}
			if issues := platform.InputIssues(transition.Payload, submitted); len(issues) > 0 {
				refusal := c.RefuseFields(issues)
				for _, issue := range issues {
					if issue.Code == "required" && len(issue.Path) > 0 {
						label := issue.Path[0]
						for _, field := range transition.Payload {
							if field.Name == label && field.Description != "" {
								label = field.Description
								break
							}
						}
						refusal.Message = fmt.Sprintf("%s needs %s", transition.Title, label)
						break
					}
				}
				return refusal
			}
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
	if invocation.GetSchema().GetName() == txn.Subject+".create" {
		info, _ := platform.Describe(b.App, b.Entities[txn.Subject], func(reflect.Type) string { return "" })
		for _, field := range info.Fields {
			spec.Inputs[field.Name] = platform.ExprType{Kind: platform.RuleFieldKind(field.Type), Object: field.Ref}
		}
	}
	if txn.Guard != nil {
		result, err := platform.EvaluateRule(*txn.Guard, spec, c, subject, values, now, b.Lookup)
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
			result, err := platform.EvaluateRule(*declaration.When, scope, c, subject, facts, now, b.Lookup)
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
			if err := transport.TakeTxnStep(); err != nil {
				return err
			}
			c.Trace(platform.DecisionTrace{Kind: "transaction-step", Name: strings.TrimPrefix(frame+"/"+step.Name, "/"), Operation: step.Kind})
			if step.When != nil {
				result, err := platform.EvaluateRule(*step.When, scope, c, subject, facts, now, b.Lookup)
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
				if span := step.ForEach.Range; span != nil {
					rows, err := b.txnRangeRows(c, *span, scope, subject, facts, now)
					if err != nil {
						return err
					}
					iterations += len(rows)
					if iterations > platform.TxnMaxIterations {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction iteration exceeds its limit / 事务迭代超出上限")
					}
					for i, row := range rows {
						rowFacts := maps.Clone(facts)
						maps.Copy(rowFacts, row)
						if err := run(step.ForEach.Steps, platform.WithTxnRange(scope, span.From.Kind), rowFacts, "", strings.TrimPrefix(frame+"/"+step.Name, "/")+"-"+strconv.Itoa(i)); err != nil {
							return err
						}
					}
					continue
				}
				source, sourceErr := platform.TxnIterationSource(b.App, txn.Subject, step.ForEach.Of, b.Lookup)
				if sourceErr != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", sourceErr.Error())
				}
				sourceID := subjectID
				if step.ForEach.Of != "" {
					raw, _, failure := c.ReadRecordPath(txn.Subject, subjectID, strings.Split(step.ForEach.Of, "."), now)
					if failure != nil {
						return failure
					}
					if json.Unmarshal(raw, &sourceID) != nil || sourceID == "" {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Iteration source reference is empty")
					}
				}
				set, info, ids, err := b.txnRecordRows(c, receipt, source.Type, sourceID, step.ForEach.Set, strings.TrimPrefix(frame+"/"+step.Name, "/"), &iterations, now)
				if err != nil {
					return err
				}
				rowScope := platform.WithTxnRow(scope, info)
				for i, raw := range set.Records {
					rowFacts, failure := txnRowInputs(c, info, raw, facts)
					if failure != nil {
						return failure
					}
					if err := run(step.ForEach.Steps, platform.RuleReadSpec{Object: rowScope.Object, Inputs: maps.Clone(rowScope.Inputs), Collections: rowScope.Collections}, rowFacts, ids[i], strings.TrimPrefix(frame+"/"+step.Name, "/")+"-"+strconv.Itoa(i)); err != nil {
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
				platform.TxnResult(&scope, step)
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
				link, err := b.Link(request.Binding)
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
					if err := b.Ledger.ApplyTxnRecord(c, receipt, b.Entities[request.Child.Type], "edit", request.Child.ID, platform.Raw(map[string]string{link.Via: value}), now); err != nil {
						return err
					}
				}
				continue
			}
			stepKey := strings.TrimPrefix(frame+"/"+step.Name, "/")
			if step.Intervals != nil {
				if err := b.txnIntervals(c, receipt, txn.Subject, subjectID, stepKey, *step.Intervals, &iterations, now, reserve); err != nil {
					return err
				}
				continue
			}
			if step.Post != nil {
				id, err := b.txnPost(c, receipt, step, scope, subject, facts, now, reserve)
				if err != nil {
					return err
				}
				facts["step."+step.Name] = id
				platform.TxnResult(&scope, step)
				continue
			}
			if step.Call != nil {
				inputs, err := b.txnParameters(c, step.Call.Inputs, scope, subject, facts, now)
				if err != nil {
					return err
				}
				result, err := calls.Call(*step.Call, inputs, now)
				if err != nil {
					return err
				}
				value, err := txnCallValue(*step.Call, result)
				if err != nil {
					return err
				}
				captured, conversionErr := platform.ExprGoValue(value)
				if conversionErr != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", conversionErr.Error())
				}
				facts["step."+step.Name] = captured
				platform.TxnResult(&scope, step)
				continue
			}
			if step.Journal != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This package version still uses a retired journal step; upgrade the package before running the action")
			}
			if step.Effect != nil {
				definition, failure := b.Effect(c, step.Effect.Writeback)
				if failure != nil {
					return failure
				}
				payload, problem := b.txnParameters(c, step.Effect.Payload, scope, subject, facts, now)
				if problem != nil {
					return problem
				}
				target := step.Target
				if target.Type == "" {
					target = platform.TxnRef{Type: txn.Subject, ID: subjectID}
				} else {
					target, problem = resolve(target, facts, rowID)
					if problem != nil {
						return problem
					}
				}
				request, failure := definition.Request(target.ID, payload)
				if failure != nil {
					return failure
				}
				if c.TxnTransport() == nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction effect transport is unavailable")
				}
				if problem := c.TxnTransport().QueueTxnEffect(c, receipt, stepKey, target, request, now); problem != nil {
					return problem
				}
				continue
			}
			if step.Work != nil {
				w := step.Work
				assignment := platform.Assignment{Title: w.Title, Ref: txn.Subject + "/" + subjectID, Key: txn.Subject + "/" + subjectID + ":" + stepKey, Close: w.Close, Answers: w.Answers}
				if step.Target.Type != "" {
					target, err := resolve(step.Target, facts, rowID)
					if err != nil {
						return err
					}
					assignment.Ref = target.Type + "/" + target.ID
				}
				if w.Role != "" {
					assignment.To = []platform.Recipient{{AppRole: w.Role}}
				}
				if w.Unit != nil {
					value, err := platform.RuleAssignment(*w.Unit, scope, c, subject, facts, now, b.Lookup)
					if err != nil {
						return err
					}
					unit := constructionText(value)
					if unit == "" {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Organizational work needs a nonempty unit / 组织任务须指定单位")
					}
					assignment.To = []platform.Recipient{{Structure: w.Structure, Unit: unit, Role: w.Role}}
				}
				for _, item := range []struct {
					expr   *platform.Expr
					target *string
				}{{w.Body, &assignment.Body}, {w.Key, &assignment.Key}} {
					if item.expr != nil {
						value, err := platform.RuleAssignment(*item.expr, scope, c, subject, facts, now, b.Lookup)
						if err != nil {
							return err
						}
						*item.target = constructionText(value)
					}
				}
				if w.Member != nil {
					value, err := platform.RuleAssignment(*w.Member, scope, c, subject, facts, now, b.Lookup)
					if err != nil {
						return err
					}
					assignment.To = []platform.Recipient{{Member: constructionText(value)}}
				}
				if w.Due != nil {
					value, err := platform.RuleAssignment(*w.Due, scope, c, subject, facts, now, b.Lookup)
					if err != nil {
						return err
					}
					if err := json.Unmarshal(platform.Raw(value), &assignment.Due); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid work due time")
					}
				}
				if err := c.Assign(receipt, assignment); err != nil {
					return err
				}
				continue
			}
			entity, ok := b.Entities[step.Target.Type]
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
			info, _ := platform.Describe(b.App, entity, func(reflect.Type) string { return "" })
			patch := map[string]any{}
			sources := []string{}
			for _, assignment := range step.Changes {
				field, _ := info.Field(assignment.Field)
				expr, err := platform.AssignmentExpr(field.Type, assignment, scope)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				value, failure := platform.RuleAssignment(expr, scope, c, subject, facts, now, b.Lookup, &sources)
				if failure != nil {
					return failure
				}
				patch[assignment.Field] = value
			}
			entity, writable := proposal.Apply(entity, info, patch, sources)
			if err := b.Ledger.ApplyTxnRecord(c, receipt, entity, verb, id, platform.Raw(writable), now); err != nil {
				return err
			}
			if step.Kind == "create" {
				facts["step."+step.Name] = id
				platform.TxnResult(&scope, step)
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
	if err := run(txn.Steps, spec, values, "", framePrefix); err != nil {
		return err
	}
	if action != txn.Subject+".create" && !written[txn.Subject+"/"+subjectID] {
		if err := b.Ledger.ApplyTxnRecord(c, receipt, b.Entities[txn.Subject], "edit", subjectID, platform.Raw(map[string]any{}), now); err != nil {
			return err
		}
	}
	if err := proposal.Finish(c, receipt, now); err != nil {
		return err
	}
	if err := b.Dispatch(c, receipt, txn, spec, subject, values, now); err != nil {
		return err
	}
	return b.Confirm(c, receipt, txn, now)
}

type Runtime struct {
	platform.BusinessRuntimeContext
}

func NewRuntime(context platform.BusinessRuntimeContext) platform.TransactionExecutor {
	return &Runtime{BusinessRuntimeContext: context}
}
