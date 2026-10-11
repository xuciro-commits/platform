package build

import (
	"context"
	"encoding/json"
	"fmt"

	buildapi "platformserver/internal/host/buildapi"
	"regexp"

	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Decision tables (ADR-0062, ADR-0057 block E): condition columns → result
// columns, first matching row wins, a default row when none does. A published
// table is a native operation of the builder - callable from a process's
// compute step, a page's compute widget or the capability API like any other
// operation - and it is the builder that evaluates it (OperationExecutor).
// A table is operational configuration: publishing installs it at once and
// keeps the published image; drafts never change what runs.
const (
	TableType = "build.table"
	tableRows = 500
	tableCols = 16
)

func (b *Build) tableEntity() platform.Entity {
	return platform.Entity{Type: TableType, Title: "Decision table", Plural: "Decision tables", Model: Table{}, Display: "title", Description: "Condition columns to result columns; the first matching row decides. Published as a native operation.",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "compute"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Check the table and install it as an operation at once.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "compute", Payload: []platform.Field{}, Do: b.publishTable}}}}
}

func (b *Build) publishTable(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	tb, ok := record.(*Table)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkTable(*tb); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if tb.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A table may retain at most 64 published versions")
	}
	tb.Version++
	frozen := *tb
	frozen.Published = ""
	if c.Staging() {
		if err := b.host.ValidateInstallOperation(frozen.Definition()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	} else if err := b.installTable(c, frozen); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	raw, _ := json.Marshal(frozen)
	tb.Published = string(raw)
	return nil
}

func (b *Build) installTable(c platform.Caller, tb Table) error {
	if err := b.host.InstallOperation(c, tb.Definition(), tb.Version); err != nil {
		return err
	}
	b.tables[tb.Name] = tb
	return nil
}

// installTables puts every published table back after a restart or snapshot.
func (b *Build) installTables() error {
	list, err := readDefinitionInventory[Table](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return err
	}
	for _, record := range list {
		if record.Published == "" || record.Archived {
			continue
		}
		tb, ok := wasPublished[Table](record.Published)
		if !ok || tb.Name != record.Name {
			return fmt.Errorf("decision table %s has a malformed publication", record.Name)
		}
		if err := b.installTable(platform.Caller{Replaying: true}, tb); err != nil {
			return err
		}
	}
	return nil
}

var columnName = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

func (b *Build) checkTable(tb Table) error {
	if err := b.checkName(tb.Name, tb.ID); err != nil {
		return err
	}
	return b.checkTableShape(tb)
}

// checkTableShape checks everything but the name's uniqueness.
func (b *Build) checkTableShape(tb Table) error {
	if strings.TrimSpace(tb.Title) == "" || len(tb.Inputs) == 0 || len(tb.Outputs) == 0 || len(tb.Inputs)+len(tb.Outputs) > tableCols || len(tb.Rows) == 0 || len(tb.Rows) > tableRows {
		return fmt.Errorf("a table needs a title, 1 to %d columns and 1 to %d rows", tableCols, tableRows)
	}
	seen := map[string]bool{}
	for _, col := range append(append([]TableColumn{}, tb.Inputs...), tb.Outputs...) {
		if !columnName.MatchString(col.Name) || seen[col.Name] || col.Type != "text" && col.Type != "number" && col.Type != "boolean" {
			return fmt.Errorf("column %q needs a unique lower-case name and a type of text, number or boolean", col.Name)
		}
		seen[col.Name] = true
	}
	for i, row := range tb.Rows {
		if len(row.When) != len(tb.Inputs) || len(row.Then) != len(tb.Outputs) {
			return fmt.Errorf("row %d does not fill every column", i+1)
		}
		for j, cell := range row.When {
			if _, err := buildapi.ValueParseCondition(cell, tb.Inputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Inputs[j].Name, err)
			}
		}
		for j, cell := range row.Then {
			if _, err := buildapi.ValueCastCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("row %d, %s: %w", i+1, tb.Outputs[j].Name, err)
			}
		}
	}
	if len(tb.Default) > 0 {
		if len(tb.Default) != len(tb.Outputs) {
			return fmt.Errorf("the default result fills every result column")
		}
		for j, cell := range tb.Default {
			if _, err := buildapi.ValueCastCell(cell, tb.Outputs[j].Type); err != nil {
				return fmt.Errorf("default, %s: %w", tb.Outputs[j].Name, err)
			}
		}
	}
	return nil
}

// Compute evaluates a published table: the builder's only native operations.
func (b *Build) Compute(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	tb, ok := b.tables[name]
	if !ok {
		return nil, fmt.Errorf("the builder has no native operation %s", name)
	}
	var values map[string]any
	if err := json.Unmarshal(input, &values); err != nil {
		return nil, fmt.Errorf("the input is not an object")
	}
	result, err := tb.Decide(values)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
