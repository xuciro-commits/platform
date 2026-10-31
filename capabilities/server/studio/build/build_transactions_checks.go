package build

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"platformserver/platform"
)

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
