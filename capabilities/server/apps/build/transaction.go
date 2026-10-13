package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
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

func (b *Build) transactionSequences() []platform.Sequence {
	byName := map[string]platform.Sequence{}
	for _, typ := range sortedTypes(b.installed) {
		if life := b.installed[typ].Lifecycle; life != nil {
			for _, act := range life.Transitions {
				if act.Txn != nil {
					for _, step := range act.Txn.Steps {
						if step.Sequence != nil {
							byName[step.Sequence.Name] = *step.Sequence
						}
					}
				}
			}
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.Sequence, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

func (b *Build) checkTransactions(o Object) error {
	subject := "build." + o.Name
	seqs := map[string]platform.Sequence{}
	for _, a := range o.Actions {
		txn := a.Transaction
		if txn == nil {
			continue
		}
		if o.Numbering != nil {
			return fmt.Errorf("transaction sequence steps replace legacy record-count numbering")
		}
		if txn.Subject != subject || len(txn.Steps) == 0 || len(txn.Steps) > 32 {
			return fmt.Errorf("transaction needs its subject and 1-32 steps")
		}
		if len(a.Sets) > 0 || len(a.Creates) > 0 || len(a.Posts) > 0 || a.Journal != nil || a.Reverses != "" || len(a.Conditions) > 0 || a.Approval != nil || a.To != "" || a.ToInput != "" {
			return fmt.Errorf("transaction replaces legacy mutations; rules, approval and state routing are outside this slice")
		}
		ownInfo, _ := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
		lookup := func(typ string) (platform.EntityInfo, bool) {
			if typ == subject {
				return ownInfo, true
			}
			return b.conditionLookup(typ)
		}
		spec := ruleSpec(o, a, txn.Reads)
		if err := b.checkRuleCollections(spec, "transaction "+a.Name); err != nil {
			return err
		}
		if txn.Guard != nil {
			if txn.Guard.Kind != "boolean" || txn.Message == "" {
				return fmt.Errorf("transaction guard needs a boolean and message")
			}
			if _, err := platform.RuleSchema(*txn.Guard, spec, lookup); err != nil {
				return err
			}
		}
		prior := map[string]platform.TxnStep{}
		updates := map[string]bool{}
		creates := map[string]bool{}
		subjectWrite := false
		for i, step := range txn.Steps {
			where := fmt.Sprintf("transaction %s step %d", a.Name, i+1)
			if !named(step.Name) || prior[step.Name].Name != "" {
				return fmt.Errorf("%s needs a unique step name", where)
			}
			if step.When != nil {
				ws := ruleSpec(o, a, txn.Reads)
				for name, prev := range prior {
					kind := "reference"
					if prev.Kind == "sequence" {
						kind = "text"
					}
					ws.Inputs["step."+name] = platform.ExprType{Kind: kind, Object: prev.Target.Type}
				}
				if step.When.Kind != "boolean" {
					return fmt.Errorf("%s when must be boolean", where)
				}
				if _, err := platform.RuleSchema(*step.When, ws, lookup); err != nil {
					return err
				}
			}
			if step.Kind == "sequence" {
				if step.Sequence == nil || step.Sequence.Check() != nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 {
					return fmt.Errorf("%s needs only a named sequence", where)
				}
				for _, seq := range b.transactionSequences() {
					if seq.Name == step.Sequence.Name && seq != *step.Sequence {
						return fmt.Errorf("%s changes an installed sequence", where)
					}
				}
				if seq, ok := seqs[step.Sequence.Name]; ok && seq != *step.Sequence {
					return fmt.Errorf("%s has conflicting sequence declarations", where)
				}
				seqs[step.Sequence.Name] = *step.Sequence
				prior[step.Name] = step
				continue
			}
			if step.Kind != "create" && step.Kind != "update" || step.Sequence != nil {
				return fmt.Errorf("%s supports only create, update and sequence", where)
			}
			if !strings.HasPrefix(step.Target.Type, ID+".") {
				return fmt.Errorf("%s cannot change another application's object %s", where, step.Target.Type)
			}
			info, ok := lookup(step.Target.Type)
			if !ok || info.App != ID {
				return fmt.Errorf("%s needs a declared object %s", where, step.Target.Type)
			}
			if step.Target.Type != subject {
				_, installed := b.installed[step.Target.Type]
				_, joint := b.jointEntities[step.Target.Type]
				if !installed && !joint {
					return fmt.Errorf("%s needs a saved builder object", where)
				}
				if installed && b.installed[step.Target.Type].ValidateAt != nil {
					return fmt.Errorf("%s requires explicit sequence steps instead of legacy numbering", where)
				}
			}
			if step.Kind == "create" {
				if step.Target.ID != "" || step.Target.Via != "" {
					return fmt.Errorf("%s create IDs are generated by the platform", where)
				}
				creates[step.Target.Type] = true
			} else {
				// One update per type is this slice's conservative static non-alias proof.
				// Updates precede creates of their type, so they cannot rewrite a new row.
				if updates[step.Target.Type] || creates[step.Target.Type] {
					return fmt.Errorf("%s may write a record only once; update targets must be statically disjoint", where)
				}
				updates[step.Target.Type] = true
				if (step.Target.ID == "") == (step.Target.Via == "") {
					return fmt.Errorf("%s needs one fixed ID, $subject or declared reference path", where)
				}
				if strings.HasPrefix(step.Target.ID, "$") && step.Target.ID != "$subject" || step.Target.ID == "$subject" && step.Target.Type != subject {
					return fmt.Errorf("%s has an invalid update identity", where)
				}
				if step.Target.Via != "" {
					path := strings.Split(step.Target.Via, ".")
					f, err := platform.RecordPathField(subject, path, lookup)
					if err != nil || f.Type != "reference" || f.Ref != step.Target.Type {
						return fmt.Errorf("%s must resolve a declared reference to its target", where)
					}
					typ := subject
					for _, name := range path {
						inf, _ := lookup(typ)
						f, _ := inf.Field(name)
						if !strings.HasPrefix(f.Ref, ID+".") {
							return fmt.Errorf("%s cannot traverse another application", where)
						}
						typ = f.Ref
					}
				}
				if step.Target.Type == subject {
					if step.Target.ID != "$subject" {
						return fmt.Errorf("%s must write its subject explicitly", where)
					}
					if step.When != nil {
						return fmt.Errorf("the subject update cannot be skipped")
					}
					subjectWrite = true
				}
			}
			used := map[string]bool{}
			for _, set := range step.Changes {
				f, found := info.Field(set.Field)
				if !found || f.ReadOnly || used[set.Field] || set.From == "" && set.Expr == nil {
					return fmt.Errorf("%s needs distinct writable fields", where)
				}
				used[set.Field] = true
				if set.Expr != nil {
					spec := ruleSpec(o, a, txn.Reads)
					for name, prev := range prior {
						kind := "reference"
						if prev.Kind == "sequence" {
							kind = "text"
						}
						spec.Inputs["step."+name] = platform.ExprType{Kind: kind, Object: prev.Target.Type}
					}
					if !ruleAssignmentFits(f.Type, set.Expr.Kind) {
						return fmt.Errorf("%s assignment result type differs", where)
					}
					if _, err := platform.RuleSchema(*set.Expr, spec, lookup); err != nil {
						return fmt.Errorf("%s set %s: %w", where, set.Field, err)
					}
					continue
				}
				if name, stepValue := strings.CutPrefix(set.From, "step."); stepValue {
					prev, ok := prior[name]
					if !ok || prev.Kind != "create" && prev.Kind != "sequence" || prev.Kind == "sequence" && f.Type != "text" && f.Type != "longtext" || prev.Kind == "create" && (f.Type != "reference" || f.Ref != prev.Target.Type) {
						return fmt.Errorf("%s needs a compatible earlier step result", where)
					}
				} else if strings.HasPrefix(set.From, "=") {
					if _, err := literal(f.Type, strings.TrimPrefix(set.From, "=")); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
				} else if set.From == "$me" {
					if f.Type != "text" {
						return fmt.Errorf("%s needs a text member field", where)
					}
				} else if set.From == "$now" {
					if f.Type != "date" && f.Type != "datetime" {
						return fmt.Errorf("%s needs a temporal field", where)
					}
				} else {
					ix := slices.IndexFunc(a.Inputs, func(in Input) bool { return in.Name == set.From })
					if ix < 0 || !inputFits(a.Inputs[ix].Type, f.Type) || a.Inputs[ix].Type == "reference" && a.Inputs[ix].Ref != f.Ref {
						return fmt.Errorf("%s has an incompatible input assignment", where)
					}
				}
			}
			prior[step.Name] = step
		}
		if !subjectWrite {
			return fmt.Errorf("transaction must include exactly one update of its subject")
		}
	}
	return nil
}

func (b *Build) transact(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) *kernel.Error {
	values := map[string]any{}
	if json.Unmarshal(r.GetSubmission().GetPayload(), &values) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction inputs / 事务输入无效")
	}
	subjectRaw, refusal := c.ReadRecord(txn.Subject, r.GetSubmission().GetTarget().GetId(), now)
	if refusal != nil {
		return refusal
	}
	var subject map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(subjectRaw)))
	decoder.UseNumber()
	if err := decoder.Decode(&subject); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction subject")
	}
	spec := platform.RuleReadSpec{Object: txn.Subject, Inputs: map[string]platform.ExprType{}}
	if txn.Reads != nil {
		spec.Collections = txn.Reads.Collections
	}
	entity := b.installed[txn.Subject]
	life := entity.Lifecycle
	for _, transition := range life.Transitions {
		if transition.Txn != nil && transition.Name == strings.TrimPrefix(r.GetSubmission().GetSchema().GetName(), txn.Subject+".") {
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
	for _, step := range txn.Steps {
		if step.When != nil {
			result, err := evalRule(*step.When, spec, c, subject, values, now, b.conditionLookup)
			if err != nil {
				return err
			}
			if string(result.Value.Data) != "true" {
				continue
			}
		}
		if step.Kind == "sequence" {
			number, err := c.Next(r, step.Sequence.Name, now)
			if err != nil {
				return err
			}
			values["step."+step.Name] = number
			spec.Inputs["step."+step.Name] = platform.ExprType{Kind: "text"}
			continue
		}
		entity, ok := b.installed[step.Target.Type]
		if !ok {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction object is unavailable / 事务对象不可用")
		}
		id := step.Target.ID
		if id == "$subject" {
			id = r.GetSubmission().GetTarget().GetId()
		}
		if step.Target.Via != "" {
			raw, _, err := c.ReadRecordPath(txn.Subject, r.GetSubmission().GetTarget().GetId(), strings.Split(step.Target.Via, "."), now)
			if err != nil {
				return err
			}
			if json.Unmarshal(raw, &id) != nil || id == "" {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE, "Transaction target is unavailable / 事务目标不可用")
			}
		}
		verb := "edit"
		if step.Kind == "create" {
			verb = "create"
			id = r.GetChangeId() + "-" + step.Name
		}
		info, _ := platform.Describe(ID, entity, func(reflect.Type) string { return "" })
		patch := map[string]any{}
		for _, set := range step.Changes {
			field, _ := info.Field(set.Field)
			var value any
			present := true
			var err error
			if set.Expr != nil {
				value, refusal = ruleAssignment(*set.Expr, spec, c, subject, values, now, b.conditionLookup)
				if refusal != nil {
					return refusal
				}
			} else {
				value, present, err = setSource(c, field.Type, set.From, values, now)
			}
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction assignment / 事务赋值无效")
			}
			if present {
				patch[set.Field] = value
			}
		}
		if err := b.ledger.ApplyTxnRecord(c, r, entity, verb, id, platform.Raw(patch), now); err != nil {
			return err
		}
		if step.Kind == "create" {
			values["step."+step.Name] = id
			spec.Inputs["step."+step.Name] = platform.ExprType{Kind: "reference", Object: step.Target.Type}
		}
	}
	return nil
}
