package platformserver

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func (h hostView) ValidateInstallOperation(o platform.Operation) error {
	if err := o.Check(); err != nil {
		return err
	}
	if o.Rules != nil {
		if o.Binding.Kind != "native" || h.app.Manifest().ID != "build" || len(o.Rules.Rows) < 1 || len(o.Rules.Rows) > 500 || o.Rules.Reads.Object != "" {
			return fmt.Errorf("rule tables need a bounded builder native operation")
		}
		for name, set := range o.Rules.Reads.Collections {
			q, version, ok := h.RuleQuery(set.App, set.Query, set.Version)
			if !ok || version != set.Version || q.Object != set.Object || (q.By == "") != (set.Of == "") {
				return fmt.Errorf("rule collection %s is not bound", name)
			}
		}
		for i, row := range o.Rules.Rows {
			if row.Condition.Kind != "boolean" {
				return fmt.Errorf("row %d requires boolean", i+1)
			}
			if _, err := platform.RuleSchema(row.Condition, o.Rules.Reads, h.t.entity); err != nil {
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
		if !slices.Contains(h.app.Manifest().AllRoles(), role) {
			return fmt.Errorf("operation %s names an undeclared role", o.Name)
		}
	}
	if o.Binding.Kind == "wasm" && !h.t.files().Exists(context.Background(), artifactKey(h.t.ID, o.Binding.Module)) {
		return fmt.Errorf("operation %s has no retained module", o.Name)
	}
	if o.Binding.Kind == "native" {
		if _, ok := h.app.(platform.OperationExecutor); !ok {
			return fmt.Errorf("operation %s has no native executor", o.Name)
		}
	}
	return nil
}
func (h hostView) InstallOperation(c platform.Caller, o platform.Operation, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 {
		return fmt.Errorf("operation requires a published owner version")
	}
	if err := h.ValidateInstallOperation(o); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetCompute, Name: o.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".compute-" + strconv.Itoa(version), ContractVersion: 1, Operation: &o}
	def, err := h.t.describeDefinition(def)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		if h.t.definitions[i].Source != "tenant" {
			return fmt.Errorf("operation %s is owned by code", o.Name)
		}
		h.t.definitions[i] = def
	} else {
		h.t.definitions = append(h.t.definitions, def)
	}
	slices.SortFunc(h.t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}
