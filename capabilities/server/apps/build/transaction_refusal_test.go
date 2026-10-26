package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/platform"
)

func refusalPublication(t *testing.T) (*Build, construction.Object) {
	t.Helper()
	b, _ := lifecycleBuild()
	reason := construction.Object{Name: "disposition", Title: "Disposition", Plural: "Dispositions", Fields: []construction.Field{{Name: "request", Title: "Request", Type: "reference", Ref: "build.request"}, {Name: "reason", Title: "Reason", Type: "text"}}}
	b.installed = map[string]platform.Entity{"build.disposition": Entity(reason)}
	b.jointEntities = map[string]platform.EntityInfo{}
	for name, entity := range b.installed {
		info, _ := platform.Describe(construction.ID, entity, func(reflect.Type) string { return "" })
		b.jointEntities[name] = info
	}
	object := construction.Object{Name: "request", Title: "Request", Plural: "Requests", Fields: []construction.Field{{Name: "done", Title: "Done", Type: "boolean"}}, States: []construction.State{{Name: "draft", Title: "Draft"}}, Actions: []construction.Action{
		{Name: "apply", Title: "Apply", From: []string{"draft"}, Transaction: &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{{Name: "done", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "done", From: "=true"}}}}}},
		{Name: "rework", Title: "Rework", From: []string{"draft"}, Transaction: &platform.BusinessTxn{Subject: "build.request", Steps: []platform.TxnStep{{Name: "done", Kind: "update", Target: platform.TxnRef{Type: "build.request", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "done", From: "=false"}}}}}},
	}}
	return b, object
}

func refusalDeclaration(level string) platform.Refusal {
	e := platform.ExprLiteralValue("boolean", true)
	declaration := platform.Refusal{When: &e, Level: level, Code: "RESOURCE_UNSUPPORTED", Message: "The selected resource cannot complete this operation", Translations: map[string]string{"zh-CN": "所选资源无法完成此操作"}}
	if level == "flag" {
		declaration.Record = &platform.TxnStep{Name: "disposition", Kind: "create", Target: platform.TxnRef{Type: "build.disposition"}, Changes: []platform.TxnSet{{Field: "request", From: "$subject"}, {Field: "reason", From: "=Allowed by reviewer"}}}
	}
	if level == "route" {
		declaration.Route = &platform.TxnRef{Type: "build.request", ID: "$subject", Action: "rework"}
	}
	return declaration
}

func TestBusinessRefusalPublication(t *testing.T) {
	for _, level := range []string{"block", "flag", "route"} {
		t.Run(level, func(t *testing.T) {
			b, object := refusalPublication(t)
			object.Actions[0].Transaction.Refusals = []platform.Refusal{refusalDeclaration(level)}
			if err := b.normalizeObjectRules(&object); err != nil {
				t.Fatal(err)
			}
			if err := b.checkTransactions(object); err != nil {
				t.Fatal(err)
			}
			if level == "flag" {
				assignment := object.Actions[0].Transaction.Refusals[0].Record.Changes[0]
				if assignment.Expr == nil || assignment.Expr.Ref != "input.$subject" || assignment.From != "" {
					t.Fatalf("refusal record did not retain its typed subject relationship: %+v", assignment)
				}
			}
		})
	}
	for _, scenario := range []string{"flag without record", "unrelated flag", "block writes", "missing route", "unknown route", "self route", "route cycle", "route approval", "missing translation", "nonboolean", "foreign record", "untyped record reference"} {
		t.Run(scenario, func(t *testing.T) {
			b, object := refusalPublication(t)
			level := "flag"
			if strings.Contains(scenario, "route") {
				level = "route"
			}
			declaration := refusalDeclaration(level)
			switch scenario {
			case "flag without record":
				declaration.Record = nil
			case "unrelated flag":
				declaration.Record.Changes = declaration.Record.Changes[1:]
			case "block writes":
				declaration.Level = "block"
			case "missing route":
				declaration.Route = nil
			case "unknown route":
				declaration.Route.Action = "missing"
			case "self route":
				declaration.Route.Action = "apply"
			case "route cycle":
				back := refusalDeclaration("route")
				back.Route.Action = "apply"
				object.Actions[1].Transaction.Refusals = []platform.Refusal{back}
			case "route approval":
				object.Actions[1].Approval = &construction.ActionApproval{}
			case "missing translation":
				declaration.Translations = nil
			case "nonboolean":
				e := platform.ExprLiteralValue("text", "wrong")
				declaration.When = &e
			case "foreign record":
				declaration.Record.Target.Type = "other.disposition"
			case "untyped record reference":
				declaration.Record.Changes[0].From = "=some-request"
			}
			object.Actions[0].Transaction.Refusals = []platform.Refusal{declaration}
			if err := b.normalizeObjectRules(&object); err == nil {
				if err := b.checkTransactions(object); err == nil {
					t.Fatal("incomplete business refusal declaration was published")
				}
			}
		})
	}
	for _, raw := range []string{`{"when":{"kind":"boolean","lit":{"value":true}},"level":"block","code":"code","message":"reason","script":"run"}`, `{"when":{"kind":"boolean","lit":{"value":true}},"level":"flag","code":"code","message":"reason","record":{"name":"entry","kind":"create","handler":"run"}}`} {
		var declaration platform.Refusal
		if json.Unmarshal([]byte(raw), &declaration) == nil {
			t.Fatal("refusal declaration ignored an undeclared execution language")
		}
	}
}
