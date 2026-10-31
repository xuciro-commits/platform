package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) transactionSequences() []platform.Sequence {
	byName := map[string]platform.Sequence{}
	for _, typ := range sortedTypes(b.installed) {
		if txn := b.installed[typ].CreateTxn; txn != nil {
			platform.WalkBusinessTxn(txn, func(step platform.TxnStep) {
				if step.Sequence != nil {
					byName[step.Sequence.Name] = *step.Sequence
				}
			})
		}
		if life := b.installed[typ].Lifecycle; life != nil {
			for _, transition := range life.Transitions {
				if transition.Txn != nil {
					platform.WalkBusinessTxn(transition.Txn, func(step platform.TxnStep) {
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

func txnUsesLegacyRows(steps []platform.TxnStep) bool {
	needed := false
	platform.WalkTxnSteps(steps, func(step platform.TxnStep) {
		sets := step.Changes
		if step.Post != nil {
			sets = append(slices.Clone(sets), step.Post.Match...)
		}
		if step.Effect != nil {
			sets = append(slices.Clone(sets), step.Effect.Payload...)
		}
		if step.Call != nil {
			sets = append(slices.Clone(sets), step.Call.Inputs...)
		}
		for _, set := range sets {
			needed = needed || strings.HasPrefix(set.From, "row.")
		}
	})
	return needed
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
				for j, match := range step.Post.Match {
					if match.Expr != nil && match.From == "" {
						continue
					}
					info, ok := lookup(step.Target.Type)
					if !ok {
						return fmt.Errorf("Transaction balance object is unavailable")
					}
					field, ok := info.Field(match.Field)
					if !ok {
						return fmt.Errorf("Post match field is unavailable")
					}
					expr, err := platform.AssignmentExpr(field.Type, match, scope)
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
						expr, err := platform.DispatchParameter(parameter, scope)
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
				if step.ForEach.Range != nil {
					if err := lower(step.ForEach.Steps, platform.WithTxnRange(scope, step.ForEach.Range.From.Kind)); err != nil {
						return err
					}
					continue
				}
				if !txnUsesLegacyRows(step.ForEach.Steps) {
					if err := lower(step.ForEach.Steps, scope); err != nil {
						return err
					}
					continue
				}
				source, err := platform.TxnIterationSource(ID, TypeOf(object.Name), step.ForEach.Of, lookup)
				if err != nil {
					return err
				}
				index := slices.IndexFunc(source.Sets, func(set platform.RecordSet) bool { return set.Name == step.ForEach.Set })
				if index < 0 {
					return fmt.Errorf("iteration set %s is undeclared", step.ForEach.Set)
				}
				info, ok := lookup(source.Sets[index].Object)
				if !ok {
					return fmt.Errorf("iteration object is unavailable")
				}
				if err := lower(step.ForEach.Steps, platform.WithTxnRow(scope, info)); err != nil {
					return err
				}
				continue
			}
			if len(step.Changes) > 0 {
				if !strings.HasPrefix(step.Target.Type, ID+".") {
					return fmt.Errorf("transaction cannot change another application")
				}
				for i := range step.Changes {
					assignment := &step.Changes[i]
					if assignment.Expr != nil && assignment.From == "" {
						continue
					}
					info, ok := lookup(step.Target.Type)
					if !ok {
						return fmt.Errorf("transaction object is unavailable")
					}
					field, ok := info.Field(assignment.Field)
					if !ok {
						return fmt.Errorf("transaction field is unavailable")
					}
					expr, err := platform.AssignmentExpr(field.Type, *assignment, scope)
					if err != nil {
						return err
					}
					*assignment = platform.TxnSet{Field: assignment.Field, Expr: &expr}
				}
			}
			platform.TxnResult(&scope, *step)
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
			expr, err := platform.DispatchParameter(parameter, scope)
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
	if asset, selected := b.jointDeclarations[binding.Ref]; selected && asset.SourceVersion == binding.SourceVersion {
		var link platform.LinkType
		if b.definitionAssetVersion(asset, platform.AssetLinkType, "link") != version || json.Unmarshal(asset.Body, &link) != nil || link.Name != binding.Ref.Name || link.Check() != nil || asset.ContractVersion != link.Contract() {
			return platform.LinkType{}, fmt.Errorf("transaction relationship candidate is invalid")
		}
		return link, nil
	}
	if declaration, retained, found := linkKind.definition(b, binding.Ref.Name, version); found && retained == version {
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

func (b *Build) checkTransactions(object Object, engines ...platform.RuleEngine) error {
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
		proposalFields := map[string]bool{}
		if action.Adoption != nil {
			for _, field := range object.Fields {
				if field.ReadOnly && field.SourceType == "generated" && field.Formula == "" && field.FormulaExpr == nil && field.AliasOf == "" {
					proposalFields[field.Name] = true
				}
			}
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
		spec := ruleSpec(object, action, txn.Reads)
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
		ownInfo, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		if err != nil {
			return err
		}
		lookup := func(typ string) (platform.EntityInfo, bool) {
			if typ == subject {
				return ownInfo, true
			}
			return b.conditionLookup(typ)
		}
		if err := b.checkRefusalDeclarations(object, action, txn.Refusals, spec, "", lookup, engines...); err != nil {
			return err
		}
		if err := b.checkRuleCollections(spec, "transaction "+action.Name, lookup); err != nil {
			return err
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
				if err := b.checkRefusalDeclarations(object, action, step.Refusals, scope, row, lookup, engines...); err != nil {
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
					source, err := platform.TxnIterationSource(ID, subject, step.ForEach.Of, lookup)
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
					if !ok || item.App != ID {
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
				typed := map[string]bool{"post": step.Post != nil, "journal": step.Journal != nil, "effect": step.Effect != nil, "work": step.Work != nil, "call": step.Call != nil, "intervals": step.Intervals != nil}
				count := 0
				for _, present := range typed {
					if present {
						count++
					}
				}
				if count > 0 || slices.Contains([]string{"post", "journal", "effect", "work", "call", "intervals"}, step.Kind) {
					if count != 1 || !typed[step.Kind] || step.Link != nil || step.Sequence != nil || len(step.Changes) > 0 {
						return fmt.Errorf("%s needs exactly its typed payload", where)
					}
					if err := b.checkTypedTxnStep(step, scope, lookup, engines...); err != nil {
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
						if err := checkTxnRef(step.Target, subject, row, scope, lookup); err != nil {
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
					for _, installed := range b.transactionSequences() {
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

func (b *Build) CanonicalObject(object Object) (Object, error) {
	var canonical Object
	if err := json.Unmarshal(platform.Raw(object), &canonical); err != nil {
		return object, err
	}
	if err := b.normalizeObjectRules(&canonical); err != nil {
		return object, err
	}
	return canonical, nil
}

func (b *Build) businessRuntime() platform.TransactionExecutor {
	return b.host.TransactionRuntime(platform.BusinessRuntimeContext{App: ID, Entities: b.installed, Ledger: b.ledger, Lookup: b.conditionLookup, Link: b.transactionLink, Dispatch: b.openDispatch, Confirm: b.openConfirmation,
		Prepare: func(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) (platform.TransactionProposal, *kernel.Error) {
			p, err := b.prepareProposal(c, r, txn, now)
			if err != nil {
				return platform.TransactionProposal{}, err
			}
			return platform.TransactionProposal{Apply: func(entity platform.Entity, info platform.EntityInfo, patch map[string]any, sources []string) (platform.Entity, map[string]any) {
				return proposalEntity(entity, info, patch, p, sources)
			}, Finish: func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error {
				return b.finishProposal(c, r, p, now)
			}}, nil
		}, Effect: func(c platform.Caller, id string) (platform.TransactionEffectDefinition, *kernel.Error) {
			w, found := platform.Get[Writeback](c, id)
			if !found {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction writeback is unavailable")
			}
			w, err := b.ExecutionWriteback(w)
			if err != nil {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			conn, found := platform.Get[Connection](c, w.Connection)
			if !found {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction effect connection is unavailable")
			}
			conn, err = b.ExecutionConnection(conn)
			if err != nil {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return platform.TransactionEffectDefinition{Request: func(id string, payload json.RawMessage) (json.RawMessage, *kernel.Error) {
				request, err := w.Request(conn, id, payload)
				if err != nil {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
				}
				return platform.Raw(request), nil
			}}, nil
		}})
}
