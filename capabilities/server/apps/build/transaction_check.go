package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func walkTxnSteps(steps []platform.TxnStep, visit func(platform.TxnStep)) {
	for _, step := range steps {
		visit(step)
		if step.ForEach != nil {
			walkTxnSteps(step.ForEach.Steps, visit)
		}
		for _, refusal := range step.Refusals {
			if refusal.Record != nil {
				walkTxnSteps([]platform.TxnStep{*refusal.Record}, visit)
			}
		}
	}
}

func walkBusinessTxn(txn *platform.BusinessTxn, visit func(platform.TxnStep)) {
	walkTxnSteps(txn.Steps, visit)
	for _, refusal := range txn.Refusals {
		if refusal.Record != nil {
			walkTxnSteps([]platform.TxnStep{*refusal.Record}, visit)
		}
	}
}

func (b *Build) transactionSequences() []platform.Sequence {
	byName := map[string]platform.Sequence{}
	for _, typ := range sortedTypes(b.installed) {
		if txn := b.installed[typ].CreateTxn; txn != nil {
			walkBusinessTxn(txn, func(step platform.TxnStep) {
				if step.Sequence != nil {
					byName[step.Sequence.Name] = *step.Sequence
				}
			})
		}
		if life := b.installed[typ].Lifecycle; life != nil {
			for _, transition := range life.Transitions {
				if transition.Txn != nil {
					walkBusinessTxn(transition.Txn, func(step platform.TxnStep) {
						if step.Sequence != nil {
							byName[step.Sequence.Name] = *step.Sequence
						}
					})
				}
			}
		}
	}
	names := slices.Sorted(maps.Keys(byName))
	out := make([]platform.Sequence, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

func withTxnRow(spec platform.RuleReadSpec, info platform.EntityInfo) platform.RuleReadSpec {
	spec.Inputs = maps.Clone(spec.Inputs)
	for _, field := range platform.ConstraintEntity(info).Fields {
		spec.Inputs["row."+field.Name] = platform.ExprType{Kind: platform.RuleFieldKind(field.Type), Object: field.Ref}
	}
	return spec
}

func txnResult(spec *platform.RuleReadSpec, step platform.TxnStep) {
	if step.Call != nil {
		spec.Inputs["step."+step.Name] = step.Call.ResultType
	}
	if step.Kind == "sequence" {
		spec.Inputs["step."+step.Name] = platform.ExprType{Kind: "text"}
	}
	if step.Kind == "create" {
		spec.Inputs["step."+step.Name] = platform.ExprType{Kind: "reference", Object: step.Target.Type}
	}
}

func (b *Build) normalizeTransaction(object Object, action *Action, lookup func(string) (platform.EntityInfo, bool)) error {
	var lower func([]platform.TxnStep, platform.RuleReadSpec) error
	lower = func(steps []platform.TxnStep, scope platform.RuleReadSpec) error {
		scope.Inputs = maps.Clone(scope.Inputs)
		for i := range steps {
			step := &steps[i]
			if step.Call != nil && step.Call.View == "" {
				step.Call.View = "snapshot"
			}
			if step.Post != nil {
				info, ok := lookup(step.Target.Type)
				if !ok {
					return fmt.Errorf("Transaction balance object is unavailable")
				}
				for j, match := range step.Post.Match {
					field, ok := info.Field(match.Field)
					if !ok {
						return fmt.Errorf("Post match field is unavailable")
					}
					expr, err := assignmentExpr(field.Type, match, scope)
					if err != nil {
						return err
					}
					step.Post.Match[j] = platform.TxnSet{Field: match.Field, Expr: &expr}
				}
			}
			for _, parameters := range []*[]platform.TxnSet{func() *[]platform.TxnSet {
				if step.Effect != nil {
					return &step.Effect.Payload
				}
				return nil
			}(), func() *[]platform.TxnSet {
				if step.Call != nil {
					return &step.Call.Inputs
				}
				return nil
			}()} {
				if parameters != nil {
					for j, parameter := range *parameters {
						expr, err := dispatchParameter(parameter, scope)
						if err != nil {
							return err
						}
						(*parameters)[j] = platform.TxnSet{Field: parameter.Field, Expr: &expr}
					}
				}
			}
			for j := range step.Refusals {
				if record := step.Refusals[j].Record; record != nil {
					if err := lower([]platform.TxnStep{*record}, scope); err != nil {
						return err
					}
				}
			}
			if step.ForEach != nil {
				index := slices.IndexFunc(object.Sets, func(set platform.RecordSet) bool { return set.Name == step.ForEach.Set })
				if index < 0 {
					return fmt.Errorf("iteration set %s is undeclared", step.ForEach.Set)
				}
				info, ok := lookup(object.Sets[index].Object)
				if !ok {
					return fmt.Errorf("iteration object is unavailable")
				}
				if err := lower(step.ForEach.Steps, withTxnRow(scope, info)); err != nil {
					return err
				}
				continue
			}
			if len(step.Changes) > 0 {
				if !strings.HasPrefix(step.Target.Type, ID+".") {
					return fmt.Errorf("transaction cannot change another application")
				}
				info, ok := lookup(step.Target.Type)
				if !ok {
					return fmt.Errorf("transaction object is unavailable")
				}
				for i := range step.Changes {
					assignment := &step.Changes[i]
					field, ok := info.Field(assignment.Field)
					if !ok {
						return fmt.Errorf("transaction field is unavailable")
					}
					expr, err := assignmentExpr(field.Type, *assignment, scope)
					if err != nil {
						return err
					}
					*assignment = platform.TxnSet{Field: assignment.Field, Expr: &expr}
				}
			}
			txnResult(&scope, *step)
		}
		return nil
	}
	scope := ruleSpec(object, *action, action.Transaction.Reads)
	for i := range action.Transaction.Refusals {
		if record := action.Transaction.Refusals[i].Record; record != nil {
			if err := lower([]platform.TxnStep{*record}, scope); err != nil {
				return err
			}
		}
	}
	if dispatch := action.Transaction.Dispatch; dispatch != nil {
		for i, parameter := range dispatch.Payload {
			expr, err := dispatchParameter(parameter, scope)
			if err != nil {
				return err
			}
			dispatch.Payload[i] = platform.TxnSet{Field: parameter.Field, Expr: &expr}
		}
	}
	return lower(action.Transaction.Steps, scope)
}

func (b *Build) transactionLink(binding platform.AssetBinding) (platform.LinkType, error) {
	ordinal, ok := strings.CutPrefix(binding.SourceVersion, b.Manifest().Version+".link-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 || strconv.Itoa(version) != ordinal || binding.Ref.App != ID || binding.Ref.Kind != platform.AssetLinkType {
		return platform.LinkType{}, fmt.Errorf("transaction needs its owner's retained relationship")
	}
	if declaration, retained, found := b.LinkTypeDefinition(binding.Ref.Name, version); found && retained == version {
		return declaration, nil
	}
	return platform.LinkType{}, fmt.Errorf("transaction relationship %s version %s is unavailable", binding.Ref.Name, binding.SourceVersion)
}

func checkTxnRef(ref platform.TxnRef, subject, row string, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool)) error {
	if ref.Action != "" {
		return fmt.Errorf("record references cannot name a transaction action")
	}
	info, ok := lookup(ref.Type)
	if !ok || info.App != ID || !strings.HasPrefix(ref.Type, ID+".") {
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

// A pair of exact complements proves that only one branch can write an item.
// This uses the closed language's comparison semantics, not a new evaluator.
func complementaryTxnWhen(a, b *platform.Expr) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Op == "not" && len(a.Args) == 1 && string(platform.Raw(a.Args[0])) == string(platform.Raw(b)) {
		return true
	}
	if b.Op == "not" && len(b.Args) == 1 && string(platform.Raw(b.Args[0])) == string(platform.Raw(a)) {
		return true
	}
	opposite := map[string]string{"=": "!=", "!=": "=", "<": ">=", ">=": "<", "<=": ">", ">": "<="}
	return opposite[a.Op] == b.Op && len(a.Args) == 2 && string(platform.Raw(a.Args)) == string(platform.Raw(b.Args))
}

func (b *Build) checkTransactions(object Object) error {
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
		return b.checkTransactions(clone)
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
	subject := "build." + object.Name
	sequences := map[string]platform.Sequence{}
	for _, action := range object.Actions {
		txn := action.Transaction
		if txn == nil {
			continue
		}
		if err := b.checkConfirmation(txn.Confirmation, object); err != nil {
			return err
		}
		if err := b.checkDispatch(txn.Dispatch, object, action); err != nil {
			return err
		}
		if object.Numbering != nil {
			return fmt.Errorf("transaction sequence steps replace legacy record-count numbering")
		}
		if txn.Subject != subject || len(txn.Steps) == 0 {
			return fmt.Errorf("transaction needs its subject and steps")
		}
		if len(action.Sets) > 0 || len(action.Creates) > 0 || len(action.Posts) > 0 || action.Journal != nil || action.Reverses != "" || len(action.Conditions) > 0 || action.To != "" || action.ToInput != "" {
			return fmt.Errorf("transaction replaces legacy mutations and state routing")
		}
		ownInfo, _ := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		lookup := func(typ string) (platform.EntityInfo, bool) {
			if typ == subject {
				return ownInfo, true
			}
			return b.conditionLookup(typ)
		}
		spec := ruleSpec(object, action, txn.Reads)
		if err := b.checkRefusalDeclarations(object, action, txn.Refusals, spec, "", lookup); err != nil {
			return err
		}
		if err := b.checkRuleCollections(spec, "transaction "+action.Name, lookup); err != nil {
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
		names, writes, total, subjectWrite := map[string]bool{}, []txnWrite{}, 0, false
		reserve := func(ref platform.TxnRef, loop string, when *platform.Expr) error {
			key := ref.ID
			if ref.Via != "" {
				key = "via:" + ref.Via
			}
			if strings.HasPrefix(key, "step.") {
				return fmt.Errorf("an earlier create cannot be written again")
			}
			fixed := func(key string) bool {
				return !strings.HasPrefix(key, "$") && !strings.Contains(key, ":") && !strings.HasPrefix(key, "input.")
			}
			for _, prior := range writes {
				if prior.typ != ref.Type {
					continue
				}
				if loop == "" && prior.loop == "" && fixed(key) && fixed(prior.key) && key != prior.key {
					continue
				}
				if loop != "" && loop == prior.loop && key == "$item" && prior.key == key && complementaryTxnWhen(when, prior.when) {
					continue
				}
				return fmt.Errorf("transaction writes of %s must be statically disjoint", ref.Type)
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
				if !named(step.Name) || names[step.Name] {
					return fmt.Errorf("%s needs a unique name", where)
				}
				names[step.Name] = true
				if step.When != nil {
					if step.When.Kind != "boolean" {
						return fmt.Errorf("%s when must be boolean", where)
					}
					if _, err := platform.RuleSchema(*step.When, scope, lookup); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
				}
				if err := b.checkRefusalDeclarations(object, action, step.Refusals, scope, row, lookup); err != nil {
					return fmt.Errorf("%s: %w", where, err)
				}
				if step.Kind == "for-each" {
					if row != "" || step.ForEach == nil || len(step.ForEach.Steps) == 0 || step.Link != nil || step.Sequence != nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs one unnested declared iteration", where)
					}
					index := slices.IndexFunc(ownInfo.Sets, func(set platform.RecordSet) bool { return set.Name == step.ForEach.Set })
					if index < 0 {
						return fmt.Errorf("%s set is undeclared", where)
					}
					set := ownInfo.Sets[index]
					if len(set.Order) == 0 {
						return fmt.Errorf("%s needs stable iteration order", where)
					}
					item, ok := lookup(set.Object)
					if !ok || item.App != ID {
						return fmt.Errorf("%s cannot iterate writes in another authority", where)
					}
					if err := check(step.ForEach.Steps, withTxnRow(scope, item), set.Object, step.Name); err != nil {
						return err
					}
					continue
				}
				if step.ForEach != nil {
					return fmt.Errorf("%s has unexpected iteration fields", where)
				}
				typed := map[string]bool{"post": step.Post != nil, "journal": step.Journal != nil, "effect": step.Effect != nil, "work": step.Work != nil, "call": step.Call != nil}
				count := 0
				for _, present := range typed {
					if present {
						count++
					}
				}
				if count > 0 || slices.Contains([]string{"post", "journal", "effect", "work", "call"}, step.Kind) {
					if count != 1 || !typed[step.Kind] || step.Link != nil || step.Sequence != nil || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs exactly its typed payload", where)
					}
					if err := b.checkTypedTxnStep(step, scope, lookup); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					if step.Post != nil {
						if err := reserve(platform.TxnRef{Type: step.Target.Type, ID: "post:" + string(platform.Raw(step.Post.Match))}, loop, step.When); err != nil {
							return err
						}
					} else if step.Target.Type != "" {
						if step.Kind != "effect" && step.Kind != "work" {
							return fmt.Errorf("%s cannot change a target", where)
						}
						if err := checkTxnRef(step.Target, subject, row, scope, lookup); err != nil {
							return err
						}
					}
					txnResult(&scope, step)
					continue
				}
				if step.Kind == "sequence" {
					if step.Sequence == nil || step.Sequence.Check() != nil || step.Link != nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs only a named sequence", where)
					}
					for _, installed := range b.transactionSequences() {
						if installed.Name == step.Sequence.Name && installed != *step.Sequence {
							return fmt.Errorf("%s changes an installed sequence", where)
						}
					}
					if prior, ok := sequences[step.Sequence.Name]; ok && prior != *step.Sequence {
						return fmt.Errorf("%s has conflicting sequence declarations", where)
					}
					sequences[step.Sequence.Name] = *step.Sequence
					txnResult(&scope, step)
					continue
				}
				if step.Sequence != nil {
					return fmt.Errorf("%s has unexpected sequence fields", where)
				}
				if step.Kind == "link" || step.Kind == "unlink" {
					if step.Link == nil || step.Target != (platform.TxnRef{}) || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs only typed relationship endpoints", where)
					}
					link, err := b.transactionLink(step.Link.Binding)
					if err != nil {
						return err
					}
					if step.Link.Parent.Type != link.Parent.Name || step.Link.Child.Type != link.Child.Name {
						return fmt.Errorf("%s relationship endpoint types differ", where)
					}
					for _, endpoint := range []platform.TxnRef{step.Link.Parent, step.Link.Child} {
						if err := checkTxnRef(endpoint, subject, row, scope, lookup); err != nil {
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
				if !ok || info.App != ID || !strings.HasPrefix(info.Type, ID+".") {
					return fmt.Errorf("%s needs its owner's object", where)
				}
				if step.Target.Type != subject {
					installed, exists := b.installed[step.Target.Type]
					_, joint := b.jointEntities[step.Target.Type]
					if !exists && !joint {
						return fmt.Errorf("%s needs a saved builder object", where)
					}
					if exists && installed.ValidateAt != nil {
						return fmt.Errorf("%s needs explicit transaction numbering", where)
					}
				}
				if step.Kind == "create" {
					if step.Target.ID != "" || step.Target.Via != "" {
						return fmt.Errorf("%s create identities are generated", where)
					}
				} else {
					if err := checkTxnRef(step.Target, subject, row, scope, lookup); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					if row != "" && (step.Target.ID != "$item" || step.Target.Type != row) {
						return fmt.Errorf("%s iteration writes must address their unique item", where)
					}
					if err := reserve(step.Target, loop, step.When); err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					if step.Target.ID == "$subject" {
						if step.When != nil || row != "" {
							return fmt.Errorf("subject write cannot be skipped or iterated")
						}
						subjectWrite = true
					}
				}
				if step.Kind == "archive" && len(step.Changes) > 0 {
					return fmt.Errorf("%s archive has no assignments", where)
				}
				used := map[string]bool{}
				for _, assignment := range step.Changes {
					field, found := info.Field(assignment.Field)
					if !found || field.ReadOnly && !(field.Name == "state" && step.Target.Type == subject) || used[field.Name] || assignment.Expr == nil && assignment.From == "" {
						return fmt.Errorf("%s needs distinct writable Expr assignments", where)
					}
					used[field.Name] = true
					expr, err := assignmentExpr(field.Type, assignment, scope)
					if err != nil {
						return fmt.Errorf("%s: %w", where, err)
					}
					if field.Name == "state" && step.Target.Type == subject && expr.Lit != nil {
						var state string
						if json.Unmarshal(expr.Lit.Value, &state) != nil || !slices.ContainsFunc(object.States, func(s State) bool { return s.Name == state }) {
							return fmt.Errorf("the action %q leaves records in %q, which is not a state of the object", action.Name, state)
						}
					}
					if !ruleAssignmentFits(field.Type, expr.Kind) {
						return fmt.Errorf("%s assignment type differs", where)
					}
					if _, err := platform.RuleSchema(expr, scope, lookup); err != nil {
						return fmt.Errorf("%s field %s: %w", where, field.Name, err)
					}
				}
				txnResult(&scope, step)
			}
			return nil
		}
		if err := check(txn.Steps, spec, "", ""); err != nil {
			return err
		}
		if !subjectWrite {
			return fmt.Errorf("transaction must write its subject exactly once")
		}
	}
	return nil
}
