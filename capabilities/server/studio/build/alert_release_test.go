package build

import (
	"reflect"
	"testing"

	"platformserver/platform"
)

func TestLegacyAlertFreezesBeforeDraftEdit(t *testing.T) {
	b, rows := lifecycleBuild()
	legacy := Alert{Record: platform.Record{ID: "legacy"}, Name: "warning", Title: "Warning", Object: "build.receipt", Field: "item", Operator: "=", Value: "late", Message: "Original message", Active: true}
	rows.rows[reflect.TypeFor[Alert]()] = []any{legacy}
	caller := b.host.Automation(platform.Caller{}, ID)
	edited := legacy
	edited.Message = "Draft message"
	if err := b.alertEntity().Validate(caller, &edited); err != nil {
		t.Fatal(err)
	}
	rows.rows[reflect.TypeFor[Alert]()] = []any{edited}
	rules, err := b.AlertRules(legacy.Object)
	if err != nil || len(rules) != 1 || rules[0].Message != legacy.Message || edited.Version != 1 {
		t.Fatalf("legacy edit changed the accepted alert: %+v, %v", rules, err)
	}
	rows.rows[reflect.TypeFor[Alert]()] = []any{Alert{Record: platform.Record{ID: "new"}, Name: "newrule", State: "draft", Active: true}}
	rules, err = b.AlertRules(legacy.Object)
	if err != nil || len(rules) != 0 {
		t.Fatalf("an unpublished alert became active: %+v, %v", rules, err)
	}
}

func TestPublishedRulesAndAgentsCannotBeArchivedAsDrafts(t *testing.T) {
	b, rows := lifecycleBuild()
	rows.rows[reflect.TypeFor[Table]()] = []any{Table{Record: platform.Record{ID: "table"}, Published: "retained"}}
	rows.rows[reflect.TypeFor[Alert]()] = []any{Alert{Record: platform.Record{ID: "alert"}, Published: "retained"}}
	rows.rows[reflect.TypeFor[Agent]()] = []any{Agent{Record: platform.Record{ID: "agent"}, Published: "retained"}}
	c := platform.NewCaller(rows, platform.Member{ID: "builder", Tenant: "target", Roles: map[string]string{ID: Builder}}, ID, false, false)
	for _, tc := range []struct{ typ, id string }{{TableType, "table"}, {AlertType, "alert"}, {AgentType, "agent"}} {
		if err := b.checkDefinitionArchive(c, tc.typ, tc.id); err == nil {
			t.Fatalf("published %s was archived as a draft", tc.typ)
		}
	}
}
