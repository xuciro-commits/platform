package build

import (
	"encoding/json"
	"fmt"

	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"

	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
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

type JournalPost struct {
	ClosedPeriod string            `json:"closedPeriod,omitempty" choices:"reject,next-open" title:"Closed period policy"`
	Date         string            `json:"date,omitempty" example:"record.receivedOn"`
	Text         string            `json:"text,omitempty" help:"A literal or a source for the entry's text"`
	Lines        []JournalPostLine `json:"lines" title:"Lines"`
}

type JournalPostLine struct {
	Account string `json:"account" help:"=<account code> or a source holding one" example:"=1403"`
	Debit   string `json:"debit,omitempty" example:"amount"`
	Credit  string `json:"credit,omitempty" example:"amount"`
	Text    string `json:"text,omitempty"`
	Object  string `json:"object,omitempty" help:"What the line is about, e.g. record.material"`
	Partner string `json:"partner,omitempty" example:"record.supplier"`
}

const BooksEndpoint = "core:books"

func (a Action) Reversal(of Action) ([]Post, *JournalPost) {
	posts := make([]Post, len(of.Posts))
	for i, p := range of.Posts {
		p.Subtract, p.Floor = !p.Subtract, false
		posts[i] = p
	}
	var journal *JournalPost
	if of.Journal != nil {
		journal = &JournalPost{Text: "Reversal: " + of.Journal.Text, ClosedPeriod: of.Journal.ClosedPeriod}
		for _, l := range of.Journal.Lines {
			journal.Lines = append(journal.Lines, JournalPostLine{Account: l.Account, Debit: l.Credit, Credit: l.Debit, Text: l.Text, Object: l.Object, Partner: l.Partner})
		}
	}
	return posts, journal
}

func (j JournalPost) Entry(changeID string, record map[string]any, inputs map[string]any, me string, now time.Time, today string) (map[string]any, error) {
	source := func(from string) any {
		switch {
		case from == "$me":
			return me
		case strings.HasPrefix(from, "="):
			return strings.TrimPrefix(from, "=")
		case strings.HasPrefix(from, "record."):
			return record[strings.TrimPrefix(from, "record.")]
		case from == "":
			return nil
		}
		if v, ok := inputs[from]; ok {
			return v
		}
		return record[from]
	}
	entry := map[string]any{"id": "jnl-" + changeID, "date": today, "source": ID + "/" + changeID}
	if j.ClosedPeriod != "" {
		entry["closedPeriod"] = j.ClosedPeriod
	}
	if j.Date != "" {
		if d := platform.TextOf(source(j.Date)); len(d) >= 10 {
			entry["date"] = d[:10]
		}
	}
	if j.Text != "" {
		entry["text"] = platform.TextOf(source(j.Text))
	}
	lines := []map[string]any{}
	for i, l := range j.Lines {
		from, side := l.Debit, "debit"
		if from == "" {
			from, side = l.Credit, "credit"
		}
		amount, err := ValueNumber(source(from))
		if err != nil {
			return entry, fmt.Errorf("line %d: the amount is not a number", i+1)
		}
		if amount == 0 {
			continue
		}
		lines = append(lines, map[string]any{"account": platform.TextOf(source(l.Account)), side: amount, "text": l.Text, "object": platform.TextOf(source(l.Object)), "partner": platform.TextOf(source(l.Partner))})
	}
	if len(lines) == 0 {
		return entry, fmt.Errorf("nothing to book")
	}
	entry["lines"] = lines
	return entry, nil
}

func EntryBody(entry map[string]any) string { raw, _ := json.Marshal(entry); return string(raw) }

func (o Object) EffectivePosts(a Action) []Post {
	if a.Reverses == "" {
		return a.Posts
	}
	for _, of := range o.Actions {
		if of.Name == a.Reverses {
			posts, _ := a.Reversal(of)
			return posts
		}
	}
	return nil
}

func (o Object) EffectiveJournal(a Action) *JournalPost {
	if a.Reverses == "" {
		return a.Journal
	}
	for _, of := range o.Actions {
		if of.Name == a.Reverses {
			_, journal := a.Reversal(of)
			return journal
		}
	}
	return nil
}

type Numbering struct {
	Field  string `json:"field" help:"The text field that takes the number" example:"number"`
	Prefix string `json:"prefix,omitempty" example:"GR"`
	Yearly bool   `json:"yearly,omitempty" help:"Restart at 1 every year and put the year in the number"`
	Width  int    `json:"width,omitempty" help:"Digits, zero-padded; default 6"`
}

func (n Numbering) Check(o Object) error {
	if n.Field == "" {
		return fmt.Errorf("the numbering names the field that takes the number")
	}
	for _, f := range o.Fields {
		if f.Name == n.Field {
			if f.Type != "text" {
				return fmt.Errorf("the numbering field %q is not a text field", n.Field)
			}
			return nil
		}
	}
	return fmt.Errorf("the numbering field %q is not a field of the object", n.Field)
}

func (n Numbering) Format(seq, year int) string {
	width := n.Width
	if width <= 0 {
		width = 6
	}
	if n.Yearly {
		return fmt.Sprintf("%s%d-%0*d", n.Prefix, year, width, seq)
	}
	return fmt.Sprintf("%s%0*d", n.Prefix, width, seq)
}

type Post struct {
	Object   string `json:"object" field:"required" help:"A defined object of this builder that keeps the balance" example:"build.stockbalance"`
	Match    []Set  `json:"match" title:"Balance identified by"`
	Field    string `json:"field" field:"required" help:"The balance's integer, decimal or money field" example:"onhand"`
	Amount   string `json:"amount" field:"required" example:"quantity"`
	Subtract bool   `json:"subtract,omitempty" title:"Subtract"`
	Floor    bool   `json:"floor,omitempty" title:"Never below zero"`
}

func ValueNumber(x any) (float64, error) {
	switch n := x.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	case map[string]any:
		if amount, ok := n["amount"]; ok {
			return ValueNumber(amount)
		}
	case platform.Money:
		return float64(n.Amount), nil
	}
	return 0, fmt.Errorf("not a number")
}

func (b *Build) checkRefusalDeclarations(object Object, action Action, declarations []platform.Refusal, scope platform.RuleReadSpec, row string, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	if len(declarations) > 16 {
		return fmt.Errorf("transaction refusals exceed their limit")
	}
	codes := map[string]bool{}
	for _, declaration := range declarations {
		if declaration.When == nil || declaration.When.Kind != "boolean" || !businessRefusalCode(declaration.Code) || codes[declaration.Code] || strings.TrimSpace(declaration.Message) == "" || len(declaration.Message) > 512 || len(declaration.Translations) != 1 || strings.TrimSpace(declaration.Translations["zh-CN"]) == "" || len(declaration.Translations["zh-CN"]) > 512 {
			return fmt.Errorf("refusal needs a unique stable code, boolean rule and English/Chinese reason")
		}
		codes[declaration.Code] = true
		if declaration.Escalation != nil {
			if err := declaration.Escalation.Check(); err != nil {
				return err
			}
			if declaration.Escalation.ReviewerRole != Builder && !slices.ContainsFunc(object.Access, func(access Access) bool { return access.Role == declaration.Escalation.ReviewerRole }) {
				return fmt.Errorf("Refusal escalation reviewer role must be declared on the original object")
			}
		}
		if _, err := platform.RuleSchema(*declaration.When, scope, lookup, engines...); err != nil {
			return err
		}
		switch declaration.Level {
		case "block":
			if declaration.Record != nil || declaration.Route != nil {
				return fmt.Errorf("blocked refusals cannot leave business writes or choose a route")
			}
		case "flag":
			if declaration.Record == nil || declaration.Route != nil {
				return fmt.Errorf("flagged refusals must declare their visible business record")
			}
		case "route":
			if declaration.Route == nil {
				return fmt.Errorf("routed refusals must declare their target transaction")
			}
			target := *declaration.Route
			if target.Type != scope.Object || target.ID != "$subject" || target.Via != "" || !Named(target.Action) || target.Action == action.Name {
				return fmt.Errorf("refusal route needs another declared transaction on its subject")
			}
			index := slices.IndexFunc(object.Actions, func(candidate Action) bool { return candidate.Name == target.Action })
			if index < 0 || object.Actions[index].Transaction == nil || object.Actions[index].Approval != nil || object.Actions[index].Adoption != nil {
				return fmt.Errorf("refusal route target must be an ordinary declared transaction")
			}
			alternative := object.Actions[index]
			created, related := false, false
			platform.WalkTxnSteps(alternative.Transaction.Steps, func(step platform.TxnStep) {
				if step.Kind != "create" {
					return
				}
				created = true
				info, ok := lookup(step.Target.Type)
				if !ok {
					return
				}
				for _, assignment := range step.Changes {
					field, ok := info.Field(assignment.Field)
					if ok && field.Type == "reference" && field.Ref == scope.Object && (assignment.From == "$subject" || assignment.Expr != nil && assignment.Expr.Ref == "input.$subject") {
						related = true
					}
				}
			})
			if created && !related {
				return fmt.Errorf("refusal route created records must declare their original subject relationship")
			}
			for _, input := range alternative.Inputs {
				if !slices.ContainsFunc(action.Inputs, func(source Input) bool {
					return source.Name == input.Name && source.Type == input.Type && source.Ref == input.Ref
				}) {
					return fmt.Errorf("refusal route target inputs must come from the original action")
				}
			}
			for _, state := range action.From {
				if !slices.Contains(alternative.From, state) {
					return fmt.Errorf("refusal route target must accept the original source states")
				}
			}
		default:
			return fmt.Errorf("refusal level must be block, flag or route")
		}
		if record := declaration.Record; record != nil {
			if record.Kind != "create" || !Named(record.Name) || record.Target.ID != "" || record.Target.Via != "" || record.Target.Action != "" || record.Target.Type == scope.Object || record.When != nil || record.Link != nil || record.ForEach != nil || record.Sequence != nil || len(record.Refusals) != 0 || len(record.Changes) == 0 {
				return fmt.Errorf("refusal records must be an unconditional create on a distinct object")
			}
			info, ok := lookup(record.Target.Type)
			if !ok || info.App != ID || !strings.HasPrefix(record.Target.Type, ID+".") {
				return fmt.Errorf("refusal record must belong to its transaction authority")
			}
			installed, exists := b.installed[record.Target.Type]
			_, joint := b.jointEntities[record.Target.Type]
			if !exists && !joint || exists && installed.ValidateAt != nil {
				return fmt.Errorf("refusal record needs a saved builder object with explicit transaction numbering")
			}
			seen, linked := map[string]bool{}, false
			recordScope := scope
			if declaration.Level == "route" {
				recordScope.Inputs = map[string]platform.ExprType{}
				for name, input := range scope.Inputs {
					if !strings.HasPrefix(name, "step.") && !strings.HasPrefix(name, "row.") {
						recordScope.Inputs[name] = input
					}
				}
			}
			for _, assignment := range record.Changes {
				field, ok := info.Field(assignment.Field)
				if !ok || field.ReadOnly || seen[field.Name] {
					return fmt.Errorf("refusal record needs distinct writable fields")
				}
				seen[field.Name] = true
				expr, err := platform.AssignmentExpr(field.Type, assignment, recordScope)
				if err != nil {
					return err
				}
				if _, err := platform.RuleSchema(expr, recordScope, lookup, engines...); err != nil {
					return err
				}
				linked = linked || field.Type == "reference" && field.Ref == scope.Object && expr.Kind == "reference" && expr.Ref == "input.$subject"
			}
			if !linked {
				return fmt.Errorf("refusal record must declare a reference to its original subject using $subject")
			}
		}
	}
	return nil
}

func businessRefusalCode(code string) bool {
	if len(code) == 0 || len(code) > 64 || !unicode.IsLetter(rune(code[0])) {
		return false
	}
	for _, char := range code {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func checkRefusalCycles(object Object) error {
	graph := map[string][]string{}
	for _, action := range object.Actions {
		if action.Transaction == nil {
			continue
		}
		add := func(declarations []platform.Refusal) {
			for _, declaration := range declarations {
				if declaration.Route != nil {
					graph[action.Name] = append(graph[action.Name], declaration.Route.Action)
				}
			}
		}
		add(action.Transaction.Refusals)
		var walk func([]platform.TxnStep, int) error
		walk = func(steps []platform.TxnStep, depth int) error {
			if depth > 1 {
				return fmt.Errorf("transaction refusal traversal cannot contain nested iteration")
			}
			for _, step := range steps {
				add(step.Refusals)
				if step.ForEach != nil {
					if err := walk(step.ForEach.Steps, depth+1); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := walk(action.Transaction.Steps, 0); err != nil {
			return err
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("refusal routes cannot form a cycle")
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		for _, target := range graph[name] {
			if err := visit(target); err != nil {
				return err
			}
		}
		delete(visiting, name)
		visited[name] = true
		return nil
	}
	for name := range graph {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) checkTxnIntervals(step platform.TxnStep, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool)) error {
	p := step.Intervals
	if p == nil || (platform.AssetRef{App: ID, Kind: platform.AssetObservation, Name: p.Source}).Check() != nil || !Named(p.Set) || step.Target != (platform.TxnRef{}) {
		return fmt.Errorf("Interval projection needs its logical source and original collection")
	}
	subject, found := lookup(scope.Object)
	if !found {
		return fmt.Errorf("Interval projection subject is unavailable")
	}
	var collection *platform.RecordSet
	for i := range subject.Sets {
		if subject.Sets[i].Name == p.Set {
			collection = &subject.Sets[i]
			break
		}
	}
	if collection == nil || len(collection.Order) == 0 || collection.Object == scope.Object {
		return fmt.Errorf("Interval projection needs a complete ordered collection of another owned object")
	}
	output, found := lookup(collection.Object)
	if !found || output.App != ID {
		return fmt.Errorf("Interval projection output must belong to its authority")
	}
	fields := platform.IntervalFields(p.Fields)
	if len(fields) != 11 {
		return fmt.Errorf("Interval projection fields must be distinct")
	}
	for name, kind := range fields {
		field, found := output.Field(name)
		if name == "" || !found || field.ReadOnly || field.Type != kind || kind == "reference" && field.Ref != scope.Object {
			return fmt.Errorf("Interval projection field %q needs a writable %s of its subject", name, kind)
		}
	}
	link, err := b.transactionLink(p.Predecessors)
	if err != nil {
		return err
	}
	if link.Storage != "edge" || link.Parent.Name != output.Type || link.Child.Name != output.Type || link.Cardinality != "many-to-many" {
		return fmt.Errorf("Interval predecessors need the output object's owned many-to-many relation")
	}
	return nil
}

func (b *Build) checkTypedTxnStep(step platform.TxnStep, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	check := func(expr *platform.Expr, kinds ...string) error {
		if expr == nil {
			return nil
		}
		if len(kinds) > 0 && !slices.Contains(kinds, expr.Kind) {
			return fmt.Errorf("Transaction expression has an incompatible result type")
		}
		_, err := platform.RuleSchema(*expr, scope, lookup, engines...)
		return err
	}
	parameters := func(sets []platform.TxnSet) error {
		seen := map[string]bool{}
		for _, set := range sets {
			if !Named(set.Field) || seen[set.Field] {
				return fmt.Errorf("Transaction parameters need distinct names")
			}
			seen[set.Field] = true
			expr, err := platform.DispatchParameter(set, scope)
			if err != nil {
				return err
			}
			if err = check(&expr); err != nil {
				return err
			}
		}
		return nil
	}
	switch step.Kind {
	case "intervals":
		return b.checkTxnIntervals(step, scope, lookup)
	case "post":
		p := step.Post
		info, found := lookup(step.Target.Type)
		if p == nil || !found || info.App != ID || len(p.Match) == 0 || step.Target.ID != "" || step.Target.Via != "" {
			return fmt.Errorf("Post steps need their owner's balance object and declared match fields")
		}
		field, found := info.Field(p.Field)
		if !found || !slices.Contains([]string{"integer", "decimal", "money"}, field.Type) || field.ReadOnly {
			return fmt.Errorf("Post steps need a writable numeric or money balance field")
		}
		if err := check(&p.Amount, platform.RuleFieldKind(field.Type)); err != nil {
			if field.Type != "decimal" || p.Amount.Kind != "integer" {
				return err
			}
			if _, err = platform.RuleSchema(p.Amount, scope, lookup, engines...); err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		for _, match := range p.Match {
			f, found := info.Field(match.Field)
			if !found || f.ReadOnly || seen[f.Name] || f.Name == p.Field {
				return fmt.Errorf("Post match fields must be distinct writable balance identifiers")
			}
			seen[f.Name] = true
			expr, err := platform.AssignmentExpr(f.Type, match, scope)
			if err != nil {
				return err
			}
			if err = check(&expr); err != nil {
				return err
			}
		}
	case "journal":
		j := step.Journal
		if j != nil && j.Reverses != "" {
			if !Named(j.Reverses) || len(j.Lines) > 0 || j.Set != "" || j.ClosedPeriod != "" {
				return fmt.Errorf("A journal reversal names one original action and declares no new lines")
			}
			return check(j.Date, "date", "text")
		}
		if j == nil || len(j.Lines) < 2 || len(j.Lines) > 256 {
			return fmt.Errorf("Journal steps need two or more bounded accounting lines")
		}
		if j.ClosedPeriod != "" && j.ClosedPeriod != "reject" && j.ClosedPeriod != "next-open" {
			return fmt.Errorf("Journal steps need a known closed period policy")
		}
		if err := check(j.Date, "date", "text"); err != nil {
			return err
		}
		if err := check(j.Text, "text"); err != nil {
			return err
		}
		if err := check(j.Currency, "text"); err != nil {
			return err
		}
		if j.Set != "" {
			for name := range scope.Inputs {
				if strings.HasPrefix(name, "row.") {
					return fmt.Errorf("Journal collections cannot nest an iteration")
				}
			}
			info, _ := lookup(scope.Object)
			index := slices.IndexFunc(info.Sets, func(set platform.RecordSet) bool { return set.Name == j.Set })
			if index < 0 {
				return fmt.Errorf("Journal collection is undeclared")
			}
			item, found := lookup(info.Sets[index].Object)
			if !found || item.App != ID {
				return fmt.Errorf("Journal collection must belong to its authority")
			}
			scope = platform.WithTxnRow(scope, item)
		}
		for _, line := range j.Lines {
			if err := check(line.When, "boolean"); err != nil {
				return err
			}
			if (line.Debit == nil) == (line.Credit == nil) {
				return fmt.Errorf("Each accounting line needs exactly one debit or credit")
			}
			if err := check(&line.Account, "text", "reference", "choice"); err != nil {
				return err
			}
			for _, expr := range []*platform.Expr{line.Debit, line.Credit} {
				if err := check(expr, "integer", "decimal"); err != nil {
					return err
				}
			}
			for _, expr := range []*platform.Expr{line.Text, line.Object, line.Partner} {
				if err := check(expr, "text", "reference", "choice"); err != nil {
					return err
				}
			}
		}
	case "effect":
		if step.Effect == nil || step.Effect.Writeback == "" {
			return fmt.Errorf("Effect steps need a declared transaction writeback")
		}
		if err := parameters(step.Effect.Payload); err != nil {
			return err
		}
		if b.host != nil {
			record, found := b.host.Record(WritebackType + "/" + step.Effect.Writeback)
			w, valid := record.(Writeback)
			if !found || !valid {
				return fmt.Errorf("Transaction writeback is unavailable")
			}
			w, err := b.ExecutionWriteback(w)
			if err != nil {
				return err
			}
			object := step.Target.Type
			if object == "" {
				object = scope.Object
			}
			if w.State != "published" || w.On != "transaction" || w.Object != object || len(w.Result) > 0 {
				return fmt.Errorf("Effect steps require a published transaction-only writeback without answer mutations")
			}
		}
	case "work":
		w := step.Work
		if w == nil || !w.Close && strings.TrimSpace(w.Title) == "" || (w.Role == "") == (w.Member == nil) && !w.Close {
			return fmt.Errorf("Work steps need a title and one declared recipient")
		}
		if (w.Structure == "") != (w.Unit == nil) || w.Unit != nil && (w.Role == "" || w.Member != nil) {
			return fmt.Errorf("Organizational work needs its original structure, unit and role")
		}
		if err := check(w.Unit, "text", "reference"); err != nil {
			return err
		}
		if err := check(w.Body, "text"); err != nil {
			return err
		}
		if err := check(w.Member, "text", "reference"); err != nil {
			return err
		}
		if err := check(w.Due, "datetime"); err != nil {
			return err
		}
		if err := check(w.Key, "text", "reference"); err != nil {
			return err
		}
	case "call":
		call := step.Call
		if call == nil || call.Ref.Check() != nil || call.Ref.Kind != platform.AssetQuery || call.Version < 0 || !slices.Contains([]string{"", "snapshot", "current"}, call.View) || !slices.Contains([]string{"text", "reference", "choice", "integer", "decimal", "boolean", "date", "datetime", "money"}, call.ResultType.Kind) {
			return fmt.Errorf("Call steps require a synchronous pure query, a snapshot/current view and a typed scalar result")
		}
		if err := parameters(call.Inputs); err != nil {
			return err
		}
		if call.Ref.App == ID && call.Version > 0 {
			if _, selected := b.jointDeclarations[call.Ref]; selected {
				if _, version, valid := b.QueryDefinition(call.Ref.Name, call.Version); valid && version == call.Version {
					return nil
				}
			}
		}
		if b.host != nil {
			owner, ok := b.host.(platform.TransactionHost)
			if !ok {
				return fmt.Errorf("The transaction query owner is unavailable")
			}
			if err := owner.CheckTxnCall(*call); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkTxnRange(span platform.TxnRange, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	if span.From.Kind != "date" && span.From.Kind != "datetime" && span.From.Kind != "integer" || span.Until.Kind != span.From.Kind || span.Unit != "day" && span.Unit != "hour" && span.Unit != "item" || (span.From.Kind == "integer") != (span.Unit == "item") || span.Unit == "hour" && span.From.Kind != "datetime" || span.Step < 1 || span.Step > platform.TxnMaxIterations || span.Limit < 1 || span.Limit > platform.TxnMaxIterations {
		return fmt.Errorf("Range iteration needs matching integers, dates or timestamps, a compatible step and a bounded limit")
	}
	for _, expr := range []platform.Expr{span.From, span.Until} {
		if _, err := platform.RuleSchema(expr, scope, lookup, engines...); err != nil {
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

func legacyTxnExpr(object Object, action Action, from, kind string, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
	if strings.HasPrefix(from, "record.") {
		return legacyOperand(object, action, strings.TrimPrefix(from, "record."), lookup)
	}
	if from == "$now" {
		op := "$now"
		if kind == "date" {
			op = "$today"
		}
		return platform.Expr{Op: op, Kind: kind}, nil
	}
	if strings.HasPrefix(from, "=") {
		value, err := platform.Literal(kind, strings.TrimPrefix(from, "="))
		return platform.ExprLiteralValue(platform.RuleFieldKind(kind), value), err
	}
	if from == "$me" {
		return platform.ExprReference("text", "caller.id"), nil
	}
	for _, input := range action.Inputs {
		if input.Name == from {
			return platform.ExprReference(platform.RuleFieldKind(input.Type), "input."+from), nil
		}
	}
	for _, field := range object.Fields {
		if field.Name == from {
			return platform.ExprReference(platform.RuleFieldKind(field.Type), from), nil
		}
	}
	return platform.Expr{}, fmt.Errorf("Legacy transaction source %q is unavailable", from)
}

func compileLegacyTransaction(object Object, action Action, lookup func(string) (platform.EntityInfo, bool)) (*platform.BusinessTxn, error) {
	if action.Transaction != nil {
		return action.Transaction, nil
	}
	subject := "build." + object.Name
	txn := &platform.BusinessTxn{Subject: subject, Reads: action.Reads}
	for _, condition := range action.Conditions {
		expr, err := conditionExpr(object, action, condition, lookup)
		if err != nil {
			return nil, err
		}
		if txn.Guard == nil {
			txn.Guard = &expr
		} else {
			combined := platform.Expr{Op: "and", Kind: "boolean", Args: []platform.Expr{*txn.Guard, expr}}
			txn.Guard = &combined
		}
		if txn.Message != "" {
			txn.Message += "; "
		}
		txn.Message += condition.Message
	}
	if txn.Guard != nil && txn.Message == "" {
		txn.Message = "The action's business conditions are not satisfied"
	}
	changes := slices.Clone(action.Sets)
	if action.To != "" && len(object.States) > 0 {
		expr := platform.ExprLiteralValue("choice", action.To)
		changes = append(changes, platform.TxnSet{Field: "state", Expr: &expr})
	}
	if action.ToInput != "" && len(object.States) > 0 {
		expr := platform.ExprReference("choice", "input."+action.ToInput)
		changes = append(changes, platform.TxnSet{Field: "state", Expr: &expr})
	}
	txn.Steps = append(txn.Steps, platform.TxnStep{Name: "subject", Kind: "update", Target: platform.TxnRef{Type: subject, ID: "$subject"}, Changes: changes})
	for i, create := range action.Creates {
		changes := slices.Clone(create.Sets)
		parent := platform.ExprReference("reference", "input.$subject")
		changes = append(changes, platform.TxnSet{Field: create.Via, Expr: &parent})
		txn.Steps = append(txn.Steps, platform.TxnStep{Name: fmt.Sprintf("create%d", i+1), Kind: "create", Target: platform.TxnRef{Type: create.Object}, Changes: changes})
	}
	for i, post := range object.EffectivePosts(action) {
		p := &platform.TxnPost{Field: post.Field, Subtract: post.Subtract, Floor: post.Floor}
		info, _ := lookup(post.Object)
		for _, match := range post.Match {
			field, _ := info.Field(match.Field)
			expr, err := legacyTxnExpr(object, action, match.From, field.Type, lookup)
			if err != nil {
				return nil, err
			}
			p.Match = append(p.Match, platform.TxnSet{Field: match.Field, Expr: &expr})
		}
		field, _ := info.Field(post.Field)
		kind := field.Type
		if kind == "" {
			kind = "decimal"
		}
		amount, err := legacyTxnExpr(object, action, post.Amount, kind, lookup)
		if err != nil {
			return nil, err
		}
		p.Amount = amount
		txn.Steps = append(txn.Steps, platform.TxnStep{Name: fmt.Sprintf("post%d", i+1), Kind: "post", Target: platform.TxnRef{Type: post.Object}, Post: p})
	}
	if journal := object.EffectiveJournal(action); journal != nil {
		j := &platform.TxnJournal{}
		if action.Reverses != "" {
			j.Reverses = action.Reverses
			txn.Steps = append(txn.Steps, platform.TxnStep{Name: "journal", Kind: "journal", Journal: j})
			return txn, nil
		}
		j.ClosedPeriod = journal.ClosedPeriod
		for _, field := range []struct {
			source, kind string
			value        **platform.Expr
		}{{journal.Date, "date", &j.Date}, {journal.Text, "text", &j.Text}} {
			if field.source != "" {
				expr, err := legacyTxnExpr(object, action, field.source, field.kind, lookup)
				if err != nil {
					return nil, err
				}
				fallback := platform.ExprLiteralValue(expr.Kind, "")
				if expr.Kind == "date" {
					fallback = platform.Expr{Op: "$today", Kind: "date"}
				}
				expr = platform.Expr{Op: "coalesce", Kind: expr.Kind, Args: []platform.Expr{expr, fallback}}
				*field.value = &expr
			}
		}
		for _, line := range journal.Lines {
			l := platform.TxnJournalLine{}
			account, err := legacyTxnExpr(object, action, line.Account, "text", lookup)
			if err != nil {
				return nil, err
			}
			l.Account = account
			for _, field := range []struct {
				source, kind string
				value        **platform.Expr
			}{{line.Debit, "decimal", &l.Debit}, {line.Credit, "decimal", &l.Credit}, {line.Object, "text", &l.Object}, {line.Partner, "text", &l.Partner}} {
				if field.source != "" {
					expr, err := legacyTxnExpr(object, action, field.source, field.kind, lookup)
					if err != nil {
						return nil, err
					}
					if field.kind == "text" {
						expr = platform.Expr{Op: "coalesce", Kind: expr.Kind, Args: []platform.Expr{expr, platform.ExprLiteralValue(expr.Kind, "")}}
					}
					*field.value = &expr
				}
			}
			if line.Text != "" {
				expr := platform.ExprLiteralValue("text", line.Text)
				l.Text = &expr
			}
			j.Lines = append(j.Lines, l)
		}
		txn.Steps = append(txn.Steps, platform.TxnStep{Name: "journal", Kind: "journal", Journal: j})
	}
	return txn, nil
}

func (b *Build) migrateObjectTransactions(object *Object) error {
	original := *object
	original.Actions = slices.Clone(object.Actions)
	lookup := ruleLookup(original, b.conditionLookup)
	for i := range object.Actions {
		action := &object.Actions[i]
		if action.Transaction == nil {
			txn, err := compileLegacyTransaction(original, *action, lookup)
			if err != nil {
				return err
			}
			action.Transaction = txn
		} else if len(action.Sets) > 0 || len(action.Creates) > 0 || len(action.Posts) > 0 || len(action.Conditions) > 0 || action.Journal != nil || action.Reverses != "" || action.To != "" || action.ToInput != "" {
			legacy := *action
			legacy.Transaction = nil
			compiled, err := compileLegacyTransaction(original, legacy, lookup)
			if err != nil {
				return err
			}
			txn := action.Transaction
			if compiled.Guard != nil {
				if txn.Guard == nil {
					txn.Guard = compiled.Guard
					txn.Message = compiled.Message
				} else {
					expr := platform.Expr{Op: "and", Kind: "boolean", Args: []platform.Expr{*txn.Guard, *compiled.Guard}}
					txn.Guard = &expr
					txn.Message += "; " + compiled.Message
				}
			}
			if txn.Reads == nil {
				txn.Reads = compiled.Reads
			}
			for _, step := range compiled.Steps {
				if step.Kind == "update" && step.Target.ID == "$subject" {
					found := false
					for j := range txn.Steps {
						existing := &txn.Steps[j]
						if existing.Kind == "update" && existing.Target.Type == txn.Subject && existing.Target.ID == "$subject" {
							for _, change := range step.Changes {
								index := slices.IndexFunc(existing.Changes, func(set platform.TxnSet) bool { return set.Field == change.Field })
								if index >= 0 {
									existing.Changes[index] = change
								} else {
									existing.Changes = append(existing.Changes, change)
								}
							}
							found = true
							break
						}
					}
					if found || len(step.Changes) == 0 {
						continue
					}
				}
				name := step.Name
				for suffix := 1; slices.ContainsFunc(txn.Steps, func(existing platform.TxnStep) bool { return existing.Name == step.Name }); suffix++ {
					step.Name = fmt.Sprintf("%s%d", name, suffix)
				}
				txn.Steps = append(txn.Steps, step)
			}
		}
		action.Sets, action.Creates, action.Posts, action.Conditions = nil, nil, nil, nil
		action.Journal = nil
		action.Reverses, action.To, action.ToInput = "", "", ""
		action.Reads = nil
	}
	if number := object.Numbering; number != nil {
		creation, err := compileLegacyNumbering(*object)
		if err != nil {
			return err
		}
		object.CreateTransaction = creation
		object.Numbering = nil
	}
	return nil
}

func compileLegacyNumbering(object Object) (*platform.BusinessTxn, error) {
	number := object.Numbering
	if number == nil {
		return nil, nil
	}
	if err := number.Check(object); err != nil {
		return nil, err
	}
	width := number.Width
	if width <= 0 {
		width = 6
	}
	pattern := number.Prefix
	if number.Yearly {
		pattern += "{year}-"
	}
	pattern += fmt.Sprintf("{n:%d}", width)
	sequence := &platform.Sequence{Name: "build." + object.Name + ".number", Pattern: pattern, Yearly: number.Yearly, SeedRecord: "build." + object.Name, SeedField: number.Field}
	if err := sequence.Check(); err != nil {
		return nil, err
	}
	expr := platform.ExprReference("text", "step.number")
	return &platform.BusinessTxn{Subject: "build." + object.Name, Steps: []platform.TxnStep{{Name: "number", Kind: "sequence", Sequence: sequence}, {Name: "subject", Kind: "update", Target: platform.TxnRef{Type: "build." + object.Name, ID: "$subject"}, Changes: []platform.TxnSet{{Field: number.Field, Expr: &expr}}}}}, nil
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
