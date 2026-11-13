package compute

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

func ValidateOperation(o platform.Operation, app platform.App, engine platform.RuleEngine, ruleQuery func(app, name string, version int) (platform.NamedQuery, int, bool), entity func(string) (platform.EntityInfo, bool), retained func(module string) bool) error {
	if err := o.Check(); err != nil {
		return err
	}
	if o.Rules != nil {
		if o.Binding.Kind != "native" || app.Manifest().ID != "build" || len(o.Rules.Rows) < 1 || len(o.Rules.Rows) > 500 || o.Rules.Reads.Object != "" {
			return fmt.Errorf("rule tables need a bounded builder native operation")
		}
		for name, set := range o.Rules.Reads.Collections {
			q, version, ok := ruleQuery(set.App, set.Query, set.Version)
			if !ok || version != set.Version || q.Object != set.Object || (q.By == "") != (set.Of == "") {
				return fmt.Errorf("rule collection %s is not bound", name)
			}
		}
		for i, row := range o.Rules.Rows {
			if row.Condition.Kind != "boolean" {
				return fmt.Errorf("row %d requires boolean", i+1)
			}
			if _, err := platform.RuleSchema(row.Condition, o.Rules.Reads, entity, engine); err != nil {
				return err
			}
			if err := o.Output.Validate(row.Result, o.Limits.MaxOutputBytes); err != nil {
				return err
			}
		}
		if len(o.Rules.Default) > 0 {
			if err := o.Output.Validate(o.Rules.Default, o.Limits.MaxOutputBytes); err != nil {
				return err
			}
		}
	}
	for _, role := range o.Roles {
		if !slices.Contains(app.Manifest().AllRoles(), role) {
			return fmt.Errorf("operation %s names an undeclared role", o.Name)
		}
	}
	if o.Binding.Kind == "wasm" && !retained(o.Binding.Module) {
		return fmt.Errorf("operation %s has no retained module", o.Name)
	}
	if o.Binding.Kind == "native" {
		if _, ok := app.(platform.OperationExecutor); !ok {
			return fmt.Errorf("operation %s has no native executor", o.Name)
		}
	}
	return nil
}
