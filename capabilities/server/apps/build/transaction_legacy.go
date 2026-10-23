package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func (b *Build) CanonicalObject(object construction.Object) (construction.Object, error) {
	var canonical construction.Object
	if err := json.Unmarshal(platform.Raw(object), &canonical); err != nil {
		return object, err
	}
	if err := b.normalizeObjectRules(&canonical); err != nil {
		return object, err
	}
	return canonical, nil
}

func legacyTxnExpr(object construction.Object, action construction.Action, from, kind string, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
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
		value, err := literal(kind, strings.TrimPrefix(from, "="))
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

func compileLegacyTransaction(object construction.Object, action construction.Action, lookup func(string) (platform.EntityInfo, bool)) (*platform.BusinessTxn, error) {
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

// New saves replace the old parallel action fields. Historical publication
// images are compiled in memory and retain their original immutable bytes.
func (b *Build) migrateObjectTransactions(object *construction.Object) error {
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

func compileLegacyNumbering(object construction.Object) (*platform.BusinessTxn, error) {
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
