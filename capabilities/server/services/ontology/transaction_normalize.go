package ontology

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"platformserver/platform"
)

func NormalizeTransaction(object Object, action *Action, lookup func(string) (platform.RecordTypeInfo, bool)) error {
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
					expr, err := AssignmentExpr(field.Type, match, scope)
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
						expr, err := DispatchParameter(parameter, scope)
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
					if err := lower(step.ForEach.Steps, WithTxnRange(scope, step.ForEach.Range.From.Kind)); err != nil {
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
				source, err := TxnIterationSource(AppID, object.Type(), step.ForEach.Of, lookup)
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
				if err := lower(step.ForEach.Steps, WithTxnRow(scope, info)); err != nil {
					return err
				}
				continue
			}
			if len(step.Changes) > 0 {
				if platform.OwnerOfType(step.Target.Type) != AppID {
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
					expr, err := AssignmentExpr(field.Type, *assignment, scope)
					if err != nil {
						return err
					}
					*assignment = platform.TxnSet{Field: assignment.Field, Expr: &expr}
				}
			}
			TxnResult(&scope, *step)
		}
		return nil
	}
	scope := RuleSpec(object, AppID, *action, action.Transaction.Reads)
	for i := range action.Transaction.Refusals {
		if record := action.Transaction.Refusals[i].Record; record != nil {
			if err := lower([]platform.TxnStep{*record}, scope); err != nil {
				return err
			}
		}
	}
	if dispatch := action.Transaction.Dispatch; dispatch != nil {
		for i, parameter := range dispatch.Payload {
			expr, err := DispatchParameter(parameter, scope)
			if err != nil {
				return err
			}
			dispatch.Payload[i] = platform.TxnSet{Field: parameter.Field, Expr: &expr}
		}
	}
	return lower(action.Transaction.Steps, scope)
}

func txnUsesLegacyRows(steps []platform.TxnStep) bool {
	needed := false
	WalkTxnSteps(steps, func(step platform.TxnStep) {
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
