package build

import "platformserver/internal/construction"

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) checkRefusalDeclarations(object construction.Object, action construction.Action, declarations []platform.Refusal, scope platform.RuleReadSpec, row string, lookup func(string) (platform.EntityInfo, bool)) error {
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
			if declaration.Escalation.ReviewerRole != construction.Builder && !slices.ContainsFunc(object.Access, func(access construction.Access) bool { return access.Role == declaration.Escalation.ReviewerRole }) {
				return fmt.Errorf("Refusal escalation reviewer role must be declared on the original object")
			}
		}
		if _, err := platform.RuleSchema(*declaration.When, scope, lookup); err != nil {
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
			if target.Type != scope.Object || target.ID != "$subject" || target.Via != "" || !construction.Named(target.Action) || target.Action == action.Name {
				return fmt.Errorf("refusal route needs another declared transaction on its subject")
			}
			index := slices.IndexFunc(object.Actions, func(candidate construction.Action) bool { return candidate.Name == target.Action })
			if index < 0 || object.Actions[index].Transaction == nil || object.Actions[index].Approval != nil || object.Actions[index].Adoption != nil {
				return fmt.Errorf("refusal route target must be an ordinary declared transaction")
			}
			alternative := object.Actions[index]
			created, related := false, false
			walkTxnSteps(alternative.Transaction.Steps, func(step platform.TxnStep) {
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
				if !slices.ContainsFunc(action.Inputs, func(source construction.Input) bool {
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
			if record.Kind != "create" || !construction.Named(record.Name) || record.Target.ID != "" || record.Target.Via != "" || record.Target.Action != "" || record.Target.Type == scope.Object || record.When != nil || record.Link != nil || record.ForEach != nil || record.Sequence != nil || len(record.Refusals) != 0 || len(record.Changes) == 0 {
				return fmt.Errorf("refusal records must be an unconditional create on a distinct object")
			}
			info, ok := lookup(record.Target.Type)
			if !ok || info.App != construction.ID || !strings.HasPrefix(record.Target.Type, construction.ID+".") {
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
				expr, err := assignmentExpr(field.Type, assignment, recordScope)
				if err != nil {
					return err
				}
				if _, err := platform.RuleSchema(expr, recordScope, lookup); err != nil {
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

var txnRefusalSignal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transaction chose a refusal branch")

func checkRefusalCycles(object construction.Object) error {
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

type txnRefusalResult struct {
	fact        platform.BusinessRefusalFact
	declaration platform.Refusal
	scope       platform.RuleReadSpec
	subject     map[string]any
	inputs      map[string]any
}

// A refusal record uses the same generated create permission, validation and
// constraint path as every other transaction row. Its audit fact is captured
// separately so the actor, time, original values and reason cannot be omitted.
func (b *Build) refusalRecord(c platform.Caller, receipt *pb.ChangeRecord, step platform.TxnStep, scope platform.RuleReadSpec, subject map[string]any, inputs map[string]any, now time.Time, ref string) *kernel.Error {
	entity, ok := b.installed[step.Target.Type]
	if !ok || step.Kind != "create" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Refusal records need a declared create step / 拒绝记录须为已声明的创建步骤")
	}
	_, id, ok := strings.Cut(ref, "/")
	if !ok || id == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Refusal record identity is unavailable / 拒绝记录标识不可用")
	}
	info, _ := platform.Describe(construction.ID, entity, func(reflect.Type) string { return "" })
	patch := map[string]any{}
	for _, assignment := range step.Changes {
		field, _ := info.Field(assignment.Field)
		expr, err := assignmentExpr(field.Type, assignment, scope)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		value, failure := ruleAssignment(expr, scope, c, subject, inputs, now, b.conditionLookup)
		if failure != nil {
			return failure
		}
		patch[assignment.Field] = value
	}
	return b.ledger.ApplyTxnRecord(c, receipt, entity, "create", id, platform.Raw(patch), now)
}
