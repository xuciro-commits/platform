package ontology

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
)

type TransactionContext struct {
	App               string
	BuilderRole       string
	Lookup            func(string) (platform.EntityInfo, bool)
	ObjectInfo        func() (platform.EntityInfo, error)
	Saved             func(string) bool
	ExplicitNumbering func(string) bool
	Sequences         func() []platform.Sequence
	Link              func(platform.AssetBinding) (platform.LinkType, error)
	Confirm           func(*platform.Confirmation, Object) error
	Dispatch          func(*platform.Dispatch, Object, Action, ...platform.RuleEngine) error
	Writeback         func(platform.TxnStep, string) error
	Call              func(platform.TxnCall) error
	Reads             func(platform.RuleReadSpec, string, func(string) (platform.EntityInfo, bool)) error
}

func checkTxnRef(ref platform.TxnRef, subject, row string, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool), app string) error {
	if ref.Action != "" {
		return fmt.Errorf("record references cannot name a transaction action")
	}
	info, ok := lookup(ref.Type)
	if !ok || info.App != app || !strings.HasPrefix(ref.Type, app+".") {
		return fmt.Errorf("transaction target must belong to its authority")
	}
	if (ref.ID == "") == (ref.Via == "") {
		return fmt.Errorf("transaction target needs exactly one identity or reference path")
	}
	if ref.Via != "" {
		base, path := subject, ref.Via
		if rest, isRow := strings.CutPrefix(path, "row."); isRow {
			base, path = row, rest
		}
		field, err := platform.RecordPathField(base, strings.Split(path, "."), lookup)
		if err != nil || field.Type != "reference" || field.Ref != ref.Type {
			return fmt.Errorf("transaction target path must resolve its declared object")
		}
		return nil
	}
	switch {
	case ref.ID == "$subject":
		if ref.Type != subject {
			return fmt.Errorf("subject identity has the wrong object")
		}
	case ref.ID == "$item":
		if row == "" || ref.Type != row {
			return fmt.Errorf("item identity needs its declared iteration object")
		}
	case strings.HasPrefix(ref.ID, "input.") || strings.HasPrefix(ref.ID, "step."):
		fact := scope.Inputs[strings.TrimPrefix(ref.ID, "input.")]
		if fact.Kind != "reference" || fact.Object != ref.Type {
			return fmt.Errorf("transaction identity needs a compatible input or earlier create")
		}
	case strings.HasPrefix(ref.ID, "$") || strings.Contains(ref.ID, "/") || len(ref.ID) > 256:
		return fmt.Errorf("transaction identity is invalid")
	}
	return nil
}

type txnWrite struct {
	typ, key, loop string
	when           *platform.Expr
}

func CheckTransactions(object Object, ctx TransactionContext, engines ...platform.RuleEngine) error {
	if object.CreateTransaction != nil {
		clone := object
		clone.Actions = slices.Clone(object.Actions)
		action := Action{Name: "creation", Transaction: object.CreateTransaction}
		if len(object.States) > 0 {
			action.From = []string{object.States[0].Name}
		}
		for _, field := range object.Fields {
			action.Inputs = append(action.Inputs, Input{Name: field.Name, Title: field.Title, Type: field.Type, Ref: field.Ref, Choices: field.Choices})
		}
		clone.Actions = append(clone.Actions, action)
		clone.CreateTransaction = nil
		return CheckTransactions(clone, ctx)
	}
	commands := map[string]bool{}
	for _, command := range object.DispatchCommands {
		if err := command.Check(); err != nil {
			return err
		}
		if commands[command.Name] {
			return fmt.Errorf("Dispatch command names must be unique")
		}
		commands[command.Name] = true
	}
	if err := checkRefusalCycles(object); err != nil {
		return err
	}
	subject := ctx.App + "." + object.Name
	sequences := map[string]platform.Sequence{}
	var ownInfo platform.EntityInfo
	var ownInfoErr error
	haveOwnInfo := false
	for _, action := range object.Actions {
		txn := action.Transaction
		if txn == nil {
			continue
		}
		proposalFields := map[string]bool{}
		if action.Adoption != nil {
			for _, field := range object.Fields {
				if field.ReadOnly && field.SourceType == "generated" && field.Formula == "" && field.FormulaExpr == nil && field.AliasOf == "" {
					proposalFields[field.Name] = true
				}
			}
		}
		if ctx.Confirm != nil {
			if err := ctx.Confirm(txn.Confirmation, object); err != nil {
				return err
			}
		}
		if ctx.Dispatch != nil {
			if err := ctx.Dispatch(txn.Dispatch, object, action, engines...); err != nil {
				return err
			}
		}
		if object.Numbering != nil {
			return fmt.Errorf("transaction sequence steps replace legacy record-count numbering")
		}
		if txn.Subject != subject || len(txn.Steps) == 0 {
			return fmt.Errorf("transaction needs its subject and steps")
		}
		if len(action.Sets) > 0 || len(action.Creates) > 0 || len(action.Posts) > 0 || action.Reverses != "" || len(action.Conditions) > 0 || action.To != "" || action.ToInput != "" {
			return fmt.Errorf("transaction replaces legacy mutations and state routing")
		}
		spec := RuleSpec(object, ctx.App, action, txn.Reads)
		var stateError error
		platform.WalkBusinessTxn(txn, func(step platform.TxnStep) {
			if stateError != nil || step.Target.Type != subject {
				return
			}
			for _, assignment := range step.Changes {
				if assignment.Field != "state" {
					continue
				}
				expr, err := platform.AssignmentExpr("choice", assignment, spec)
				if err != nil {
					stateError = err
					return
				}
				if expr.Lit != nil {
					var state string
					if json.Unmarshal(expr.Lit.Value, &state) != nil || !slices.ContainsFunc(object.States, func(s State) bool { return s.Name == state }) {
						stateError = fmt.Errorf("the action %q leaves records in %q, which is not a state of the object", action.Name, state)
						return
					}
				}
			}
		})
		if stateError != nil {
			return stateError
		}
		if !haveOwnInfo {
			ownInfo, ownInfoErr = ctx.ObjectInfo()
			haveOwnInfo = true
		}
		if ownInfoErr != nil {
			return ownInfoErr
		}
		lookup := func(typ string) (platform.EntityInfo, bool) {
			if typ == subject {
				return ownInfo, true
			}
			return ctx.Lookup(typ)
		}
		if err := checkRefusalDeclarations(ctx, object, action, txn.Refusals, spec, "", lookup, engines...); err != nil {
			return err
		}
		if ctx.Reads != nil {
			if err := ctx.Reads(spec, "transaction "+action.Name, lookup); err != nil {
				return err
			}
		}
		if txn.Guard != nil {
			if txn.Guard.Kind != "boolean" || txn.Message == "" {
				return fmt.Errorf("transaction guard needs a boolean and message")
			}
			if _, err := platform.RuleSchema(*txn.Guard, spec, lookup, engines...); err != nil {
				return err
			}
		}
		names, writes, total := map[string]bool{}, []txnWrite{}, 0
		reserve := func(ref platform.TxnRef, loop string, when *platform.Expr) error {
			key := ref.ID
			if ref.Via != "" {
				key = "via:" + ref.Via
			}
			if strings.HasPrefix(key, "step.") {
				return fmt.Errorf("an earlier create cannot be written again")
			}
			for _, prior := range writes {
				if prior.typ == ref.Type && prior.key == key && prior.loop == loop && when == nil && prior.when == nil {
					return fmt.Errorf("transaction writes %s more than once", ref.Type)
				}
			}
			writes = append(writes, txnWrite{ref.Type, key, loop, when})
			return nil
		}
		var check func([]platform.TxnStep, platform.RuleReadSpec, string, string) error
		check = func(steps []platform.TxnStep, scope platform.RuleReadSpec, row, loop string) error {
			scope.Inputs = maps.Clone(scope.Inputs)
			for _, step := range steps {
				total++
				where := fmt.Sprintf("transaction %s step %s", action.Name, step.Name)
				if total > platform.TxnMaxSteps {
					return fmt.Errorf("%s has %d steps; limit is %d", where, total, platform.TxnMaxSteps)
				}
				if !Named(step.Name) || names[step.Name] {
					return fmt.Errorf("%s needs a unique name", where)
				}
				names[step.Name] = true
				if step.When != nil {
					if step.When.Kind != "boolean" {
						return fmt.Errorf("%s when must be boolean", where)
					}
					if _, err := platform.RuleSchema(*step.When, scope, lookup, engines...); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
				}
				if err := checkRefusalDeclarations(ctx, object, action, step.Refusals, scope, row, lookup, engines...); err != nil {
					return fmt.Errorf("%s: %w", where, err)
				}
				if step.Kind == "for-each" {
					if loop != "" || step.ForEach == nil || len(step.ForEach.Steps) == 0 || step.Link != nil || step.Sequence != nil || step.Intervals != nil || step.Post != nil || step.Journal != nil || step.Effect != nil || step.Work != nil || step.Call != nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 || (step.ForEach.Set == "") == (step.ForEach.Range == nil) {
						return fmt.Errorf("%s needs one unnested declared iteration", where)
					}
					if step.ForEach.Range != nil {
						if step.ForEach.Of != "" {
							return fmt.Errorf("%s interval cannot name a collection record", where)
						}
						if err := checkTxnRange(*step.ForEach.Range, scope, lookup, engines...); err != nil {
							return fmt.Errorf("%s: %w", where, err)
						}
						if err := check(step.ForEach.Steps, platform.WithTxnRange(scope, step.ForEach.Range.From.Kind), "", step.Name); err != nil {
							return err
						}
						continue
					}
					source, err := platform.TxnIterationSource(ctx.App, subject, step.ForEach.Of, lookup)
					if err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					index := slices.IndexFunc(source.Sets, func(set platform.RecordSet) bool { return set.Name == step.ForEach.Set })
					if index < 0 {
						return fmt.Errorf("%s set is undeclared", where)
					}
					set := source.Sets[index]
					if len(set.Order) == 0 {
						return fmt.Errorf("%s needs stable iteration order", where)
					}
					item, ok := lookup(set.Object)
					if !ok || item.App != ctx.App {
						return fmt.Errorf("%s cannot iterate writes in another authority", where)
					}
					if err := check(step.ForEach.Steps, platform.WithTxnRow(scope, item), set.Object, step.Name); err != nil {
						return err
					}
					continue
				}
				if step.ForEach != nil {
					return fmt.Errorf("%s has unexpected iteration fields", where)
				}
				if step.Kind == "journal" {
					if step.Journal == nil || step.Link != nil || step.Sequence != nil || step.Effect != nil || step.Work != nil || step.Call != nil || step.Intervals != nil || step.Post != nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 {
						return fmt.Errorf("%s has invalid retired journal fields", where)
					}
					continue
				}
				typed := map[string]bool{"post": step.Post != nil, "effect": step.Effect != nil, "work": step.Work != nil, "call": step.Call != nil, "intervals": step.Intervals != nil}
				count := 0
				for _, present := range typed {
					if present {
						count++
					}
				}
				if count > 0 || slices.Contains([]string{"post", "effect", "work", "call", "intervals"}, step.Kind) {
					if count != 1 || !typed[step.Kind] || step.Link != nil || step.Sequence != nil || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs exactly its typed payload", where)
					}
					if err := checkTypedTxnStep(ctx, step, scope, lookup, engines...); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					if step.Intervals != nil && loop != "" {
						return fmt.Errorf("%s interval projection cannot nest an iteration", where)
					}
					if step.Post != nil {
						if err := reserve(platform.TxnRef{Type: step.Target.Type, ID: "post:" + string(platform.Raw(step.Post.Match))}, loop, step.When); err != nil {
							return err
						}
					} else if step.Target.Type != "" {
						if step.Kind != "effect" && step.Kind != "work" {
							return fmt.Errorf("%s cannot change a target", where)
						}
						if err := checkTxnRef(step.Target, subject, row, scope, lookup, ctx.App); err != nil {
							return err
						}
					}
					platform.TxnResult(&scope, step)
					continue
				}
				if step.Kind == "sequence" {
					if step.Sequence == nil || step.Sequence.Check() != nil || step.Link != nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs only a named sequence", where)
					}
					var installedSequences []platform.Sequence
					if ctx.Sequences != nil {
						installedSequences = ctx.Sequences()
					}
					for _, installed := range installedSequences {
						if installed.Name == step.Sequence.Name && installed != *step.Sequence {
							return fmt.Errorf("%s changes an installed sequence", where)
						}
					}
					if prior, ok := sequences[step.Sequence.Name]; ok && prior != *step.Sequence {
						return fmt.Errorf("%s has conflicting sequence declarations", where)
					}
					sequences[step.Sequence.Name] = *step.Sequence
					platform.TxnResult(&scope, step)
					continue
				}
				if step.Sequence != nil {
					return fmt.Errorf("%s has unexpected sequence fields", where)
				}
				if step.Kind == "link" || step.Kind == "unlink" {
					if step.Link == nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs only typed relationship endpoints", where)
					}
					link, err := ctx.Link(step.Link.Binding)
					if err != nil {
						return err
					}
					if step.Link.Parent.Type != link.Parent.Name || step.Link.Child.Type != link.Child.Name {
						return fmt.Errorf("%s relationship endpoint types differ", where)
					}
					for _, endpoint := range []platform.TxnRef{step.Link.Parent, step.Link.Child} {
						if err := checkTxnRef(endpoint, subject, row, scope, lookup, ctx.App); err != nil {
							return fmt.Errorf("%s: %w", where, err)
						}
					}
					if link.Storage == "reference" {
						if err := reserve(step.Link.Child, loop, step.When); err != nil {
							return fmt.Errorf("%s: %w", where, err)
						}
					}
					continue
				}
				if step.Link != nil || !slices.Contains([]string{"create", "update", "archive"}, step.Kind) {
					return fmt.Errorf("%s has an unsupported kind or payload", where)
				}
				info, ok := lookup(step.Target.Type)
				if !ok || info.App != ctx.App || !strings.HasPrefix(info.Type, ctx.App+".") {
					return fmt.Errorf("%s needs its owner's object", where)
				}
				if step.Target.Type != subject {
					if !ctx.Saved(step.Target.Type) {
						return fmt.Errorf("%s needs a saved builder object", where)
					}
					if ctx.ExplicitNumbering(step.Target.Type) {
						return fmt.Errorf("%s needs explicit transaction numbering", where)
					}
				}
				if step.Kind == "create" {
					if step.Target.ID != "" || step.Target.Via != "" {
						return fmt.Errorf("%s create identities are generated", where)
					}
				} else {
					if err := checkTxnRef(step.Target, subject, row, scope, lookup, ctx.App); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					if row != "" && (step.Target.ID != "$item" || step.Target.Type != row) {
						return fmt.Errorf("%s iteration writes must address their unique item", where)
					}
					if err := reserve(step.Target, loop, step.When); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
				}
				if step.Kind == "archive" && len(step.Changes) > 0 {
					return fmt.Errorf("%s archive has no assignments", where)
				}
				used := map[string]bool{}
				for _, assignment := range step.Changes {
					field, found := info.Field(assignment.Field)
					proposalField := proposalFields[field.Name] && step.Kind == "update" && step.Target.Type == subject && step.Target.ID == "$subject" && step.Target.Via == "" && row == ""
					if !found || field.ReadOnly && !(field.Name == "state" && step.Target.Type == subject) && !proposalField || used[field.Name] || assignment.Expr == nil && assignment.From == "" {
						return fmt.Errorf("%s needs distinct writable Expr assignments", where)
					}
					used[field.Name] = true
					expr, err := platform.AssignmentExpr(field.Type, assignment, scope)
					if err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					if !platform.RuleAssignmentFits(field.Type, expr.Kind) {
						return fmt.Errorf("%s assignment type differs", where)
					}
					if _, err := platform.RuleSchema(expr, scope, lookup, engines...); err != nil {
						return fmt.Errorf("%s field %s: %w", where, field.Name, err)
					}
				}
				platform.TxnResult(&scope, step)
			}
			return nil
		}
		if err := check(txn.Steps, spec, "", ""); err != nil {
			return err
		}
	}
	return nil
}
