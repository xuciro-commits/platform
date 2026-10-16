package platformserver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestApplicationRulesPromoteUpgradeRollbackAndRecover(t *testing.T) {
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	source := builderTenant(t, "rule-source")
	target := builderTenant(t, "rule-target")
	var entries []Entry
	target.Record = func(e Entry) { entries = append(entries, e) }
	fail := false
	target.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.device"}
	table := platform.AssetRef{App: build.ID, Kind: platform.AssetCompute, Name: "gate"}
	alert := platform.AssetRef{App: build.ID, Kind: platform.AssetAlert, Name: "warning"}
	decide(t, source, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "OBJECT", map[string]any{"name": "device", "title": "Device", "fields": []build.Field{{Name: "condition", Title: "Condition", Type: "text"}}}, at)
	decide(t, source, "builder", build.ID, build.TableType+".create", build.TableType, "TABLE", map[string]any{"name": "gate", "title": "Device gate", "inputs": []build.TableColumn{{Name: "condition", Type: "text"}}, "outputs": []build.TableColumn{{Name: "allowed", Type: "boolean"}}, "rows": []build.TableRow{{When: []string{"bad"}, Then: []string{"true"}}}, "default": []string{"false"}}, at)
	decide(t, source, "builder", build.ID, build.AlertType+".create", build.AlertType, "ALERT", map[string]any{"name": "warning", "title": "Device warning", "object": object.Name, "field": "condition", "operator": "=", "value": "bad", "message": "Original warning", "role": build.User, "active": true, "every": true}, at)
	decide(t, source, "builder", build.ID, build.AppType+".create", build.AppType, "APP", map[string]any{"name": "operations", "title": "Device operations", "resources": []platform.AssetRef{object, table, alert}}, at)
	selection := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "OBJECT"}, {Kind: platform.AssetCompute, ID: "TABLE"}, {Kind: platform.AssetAlert, ID: "ALERT"}, {Kind: platform.AssetApp, ID: "APP"}}
	save := func(key string) string {
		t.Helper()
		preview, err := source.PreviewReleaseDrafts(memberOf(t, source, "builder"), selection)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("application rule preview: %s %v", preview.Diagnostic, err)
		}
		if _, err := source.SaveReleaseCandidates(memberOf(t, source, "builder"), selection, preview.CandidateID, "save-"+key, at); err != nil {
			t.Fatal(err)
		}
		if _, err := source.ActivateRelease(memberOf(t, source, "builder"), preview.CandidateID, "activate-"+key, at); err != nil {
			t.Fatal(err)
		}
		return preview.CandidateID
	}
	check := func(tn *Tenant, expected string, message string) {
		t.Helper()
		owner := tn.app(build.ID).(*build.Build)
		result, err := owner.Compute(context.Background(), table.Name, platform.Raw(map[string]any{"condition": "bad"}))
		if err != nil || string(result) != expected {
			t.Fatalf("running table = %s, %v; expected %s", result, err, expected)
		}
		rules, err := owner.AlertRules(object.Name)
		if err != nil || len(rules) != 1 || rules[0].Message != message {
			t.Fatalf("running alert = %+v, %v; expected %s", rules, err, message)
		}
	}
	v1 := save("v1")
	if _, err := PromoteCandidate(source, target, v1, "promote-v1", true, memberOf(t, target, "builder"), at); err != nil {
		t.Fatal(err)
	}
	check(target, `{"allowed":true}`, "Original warning")
	decide(t, target, "builder", build.ID, object.Name+".create", object.Name, "D1", map[string]any{"condition": "bad"}, at)
	if notices := target.notificationsFor("user"); len(notices) != 1 || notices[0].Body != "Original warning" {
		t.Fatalf("published alert did not notify its original role: %+v", notices)
	}
	decide(t, source, "builder", build.ID, build.TableType+".edit", build.TableType, "TABLE", map[string]any{"rows": []build.TableRow{{When: []string{"bad"}, Then: []string{"false"}}}}, at)
	decide(t, source, "builder", build.ID, build.AlertType+".edit", build.AlertType, "ALERT", map[string]any{"message": "Updated warning"}, at)
	check(source, `{"allowed":true}`, "Original warning")
	v2 := save("v2")
	if _, err := PromoteCandidate(source, target, v2, "promote-v2", false, memberOf(t, target, "builder"), at); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err := target.ActivateRelease(memberOf(t, target, "builder"), v2, "activate-v2", at); err == nil {
		t.Fatal("failed append installed the new rules")
	}
	check(target, `{"allowed":true}`, "Original warning")
	fail = false
	if _, err := target.ActivateRelease(memberOf(t, target, "builder"), v2, "activate-v2", at); err != nil {
		t.Fatal(err)
	}
	check(target, `{"allowed":false}`, "Updated warning")
	if _, err := target.ActivateRelease(memberOf(t, target, "builder"), v1, "rollback-v1", at); err != nil {
		t.Fatal(err)
	}
	check(target, `{"allowed":true}`, "Original warning")
	decide(t, target, "builder", build.ID, object.Name+".edit", object.Name, "D1", map[string]any{"condition": "bad"}, at)
	if notices := target.notificationsFor("user"); len(notices) != 2 || notices[0].Body != "Original warning" {
		t.Fatalf("rolled-back rule did not execute: %+v", notices)
	}
	for _, definition := range target.Definitions(memberOf(t, target, "builder")) {
		if definition.Ref == alert && !strings.HasSuffix(definition.Version, ".alert-1") {
			t.Fatal("alert discovery points at the editable draft")
		}
	}
	CheckReplay(t, target, entries, func() *Tenant { return builderTenant(t, "rule-target") })
}

func TestSharedApplicationRulesKeepDependentsAndHistory(t *testing.T) {
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	tn := builderTenant(t, "shared-rules")
	publishObject(t, tn, "builder", "DEVICE", map[string]any{"name": "device", "title": "Device", "fields": []build.Field{{Name: "condition", Title: "Condition", Type: "text"}}}, at)
	object := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.device"}
	table := platform.AssetRef{App: build.ID, Kind: platform.AssetCompute, Name: "gate"}
	decide(t, tn, "builder", build.ID, build.TableType+".create", build.TableType, "TABLE", map[string]any{"name": "gate", "title": "Gate", "inputs": []build.TableColumn{{Name: "condition", Type: "text"}}, "outputs": []build.TableColumn{{Name: "allowed", Type: "boolean"}}, "rows": []build.TableRow{{When: []string{"bad"}, Then: []string{"true"}}}}, at)
	decide(t, tn, "builder", build.ID, build.SchemaTablePublish, build.TableType, "TABLE", map[string]any{}, at)
	apps := []platform.AssetRef{}
	for _, id := range []string{"first", "second"} {
		decide(t, tn, "builder", build.ID, build.AppType+".create", build.AppType, id, map[string]any{"name": id, "title": id, "resources": []platform.AssetRef{object, table}}, at)
		decide(t, tn, "builder", build.ID, build.SchemaHandOver, build.AppType, id, map[string]any{}, at)
		apps = append(apps, platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: id})
	}
	decide(t, tn, "builder", build.ID, build.TableType+".edit", build.TableType, "TABLE", map[string]any{"rows": []build.TableRow{{When: []string{"bad"}, Then: []string{"false"}}}}, at)
	preview, err := tn.PreviewRelease(memberOf(t, tn, "builder"), platform.AssetCompute, "TABLE")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview.Diagnostic, err)
	}
	for _, app := range apps {
		if !slices.Contains(preview.Included, app) {
			t.Fatalf("shared update omitted dependent %s", app)
		}
	}
	if _, err := tn.SaveReleaseCandidate(memberOf(t, tn, "builder"), platform.AssetCompute, "TABLE", preview.CandidateID, "save-shared", at); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), preview.CandidateID, "activate-shared", at); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", build.ID, build.AppType+".edit", build.AppType, "first", map[string]any{"resources": []platform.AssetRef{object}}, at)
	decide(t, tn, "builder", build.ID, build.SchemaHandOver, build.AppType, "first", map[string]any{}, at)
	second, _ := platform.Get[build.Application](tn.automation(build.ID, false), "second")
	if !slices.Contains(second.Resources, table) {
		t.Fatal("removing one application member changed another application")
	}
	before, _ := platform.Get[build.Table](tn.automation(build.ID, false), "TABLE")
	refuse(t, tn, "builder", build.ID, build.TableType+".archive", build.TableType, "TABLE", map[string]any{}, at)
	after, _ := platform.Get[build.Table](tn.automation(build.ID, false), "TABLE")
	if string(platform.Raw(after)) != string(platform.Raw(before)) {
		t.Fatal("rejected retirement changed the shared definition")
	}
	owner := tn.app(build.ID).(*build.Build)
	value, err := owner.Compute(context.Background(), "gate", platform.Raw(map[string]any{"condition": "bad"}))
	if err != nil || string(value) != `{"allowed":false}` {
		t.Fatal(string(value), err)
	}
}
