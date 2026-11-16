package build

import (
	"context"
	"encoding/json"
	"fmt"
	functionauthoring "platformserver/services/functions/authoring"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

const SchemaTablePublish = functionauthoring.TableType + ".publish"

func (b *Build) tableEntity() platform.Entity {
	return platform.Entity{Type: functionauthoring.TableType, Title: "Decision table", Plural: "Decision tables", Model: functionauthoring.Table{}, Display: "title", Description: "Condition columns to result columns; the first matching row decides. Published as a native operation.",
		Validate: func(c platform.Caller, record any) *platform.Error {
			if c.Replaying {
				return nil
			}
			tb := record.(*functionauthoring.Table)
			if err := tb.NormalizeRules(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if err := functionauthoring.CheckTableShape(*tb, b.conditionLookup, b.checkRuleCollections, c.Rules()); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "compute"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Description: "Check the table and install it as an operation at once.",
				Roles:       []string{Builder},
				Capability:  "compute",
				Parameters:  []platform.Field{},
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules:       []platform.ActionRule{{Function: b.publishTable}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}

func (b *Build) publishTable(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	tb, ok := record.(*functionauthoring.Table)
	if !ok {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if err := tb.NormalizeRules(); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	if err := b.checkTable(*tb, c.Rules()); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if tb.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A table may retain at most 64 published versions")
	}
	tb.Version++
	frozen := *tb
	frozen.Published = ""
	frozen.Versions = nil
	if c.Staging() {
		if err := b.host.ValidateInstallOperation(c, frozen.Definition()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := b.installTable(c, frozen); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	raw, _ := json.Marshal(frozen)
	if tb.Published != "" && !slices.Contains(tb.Versions, tb.Published) {
		tb.Versions = append(tb.Versions, tb.Published)
	}
	tb.Published = string(raw)
	tb.Versions = append(tb.Versions, tb.Published)
	return nil
}

func (b *Build) installTable(c platform.Caller, tb functionauthoring.Table) error {
	if err := b.host.InstallOperation(c, tb.Definition(), tb.Version); err != nil {
		return err
	}
	b.tables[tb.Name] = tb
	return nil
}

func (b *Build) installTables() error {
	list, err := readDefinitionInventory[functionauthoring.Table](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return err
	}
	for _, record := range list {
		if record.Published == "" || record.Archived {
			continue
		}
		tb, ok := ReadPublication[functionauthoring.Table](record.Published)
		if !ok || tb.Name != record.Name {
			return fmt.Errorf("decision table %s has a malformed publication", record.Name)
		}
		tb.Versions = record.Versions
		if err := b.installTable(platform.Caller{Replaying: true}, tb); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) checkTable(tb functionauthoring.Table, engines ...platform.RuleEngine) error {
	if err := b.checkName(tb.Name, tb.ID); err != nil {
		return err
	}
	return functionauthoring.CheckTableShape(tb, b.conditionLookup, b.checkRuleCollections, engines...)
}

func (b *Build) Compute(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	tb, ok := b.tables[name]
	if !ok {
		return nil, fmt.Errorf("the builder has no native operation %s", name)
	}
	var values map[string]any
	if err := json.Unmarshal(input, &values); err != nil {
		return nil, fmt.Errorf("the input is not an object")
	}
	plan, err := tb.RulePlan()
	if err != nil {
		return nil, err
	}
	legacy := len(tb.Rows) > 0 && len(tb.Rows[0].Expressions) == 0
	return plan.Decide(platform.TableInput(plan.Reads, legacy, values), func(string) (platform.EntityInfo, bool) { return platform.EntityInfo{}, false })
}

func tableImage(image []byte) (functionauthoring.Table, error) {
	var record functionauthoring.Table
	if err := json.Unmarshal(image, &record); err != nil {
		return record, err
	}
	frozen, ok := ReadPublication[functionauthoring.Table](record.Published)
	if !ok || record.State != "published" || frozen.ID != record.ID || frozen.Name != record.Name || frozen.Version < 1 || frozen.Version > record.Version || record.Version > 64 {
		return frozen, fmt.Errorf("accepted table has no matching immutable publication")
	}
	return frozen, nil
}
