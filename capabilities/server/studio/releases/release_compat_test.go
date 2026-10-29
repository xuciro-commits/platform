package releases

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"platformserver/platform"
)

func TestHistoricalCodeAssetCompatibility(t *testing.T) {
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	action := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.count"}
	asset := func(ref platform.AssetRef, value any) platform.ReleaseAsset {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		current, err := platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: "1", Body: raw})
		if err != nil {
			t.Fatal(err)
		}
		return current
	}
	body := func(a platform.ReleaseAsset) map[string]json.RawMessage {
		t.Helper()
		var out map[string]json.RawMessage
		if err := json.Unmarshal(a.Body, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	withBody := func(a platform.ReleaseAsset, value any) platform.ReleaseAsset {
		t.Helper()
		var err error
		a.Body, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	legacy := func(a platform.ReleaseAsset) platform.ReleaseAsset {
		t.Helper()
		fields := body(a)
		for _, key := range []string{"objectBindings", "protocolBindings", "toolBindings"} {
			delete(fields, key)
		}
		a = withBody(a, fields)
		a.Provides = nil
		return a
	}
	check := func(current, retained platform.ReleaseAsset, want bool) {
		t.Helper()
		before := bytes.Clone(retained.Body)
		owner := runtimeAssetOwner{code: []platform.ReleaseAsset{current}}
		controlled, err := owner.controlled([]platform.ReleaseAsset{retained})
		if (err == nil) != want || len(controlled) != 0 {
			t.Fatalf("compatibility=%t, want %t: %v", err == nil, want, err)
		}
		if !bytes.Equal(retained.Body, before) {
			t.Fatal("historical bytes were rewritten")
		}
	}
	currentAction := asset(action, map[string]any{
		"schema": action.Name, "target": object.Name, "roles": []string{"lead"},
		"objectBindings": map[string]platform.AssetRef{object.Name: object},
		"action":         platform.Action{Schema: action.Name, Target: object.Name, Roles: []string{"lead"}, Payload: []platform.Field{{Name: "qty", Type: "integer", Description: "Counted"}}},
	})
	t.Run("same frozen owner accepts newly derived metadata", func(t *testing.T) {
		retained := legacy(currentAction)
		retained.Requires = nil // Original action validation did not infer its record target.
		check(currentAction, retained, true)
	})
	t.Run("roles payload version and bindings remain immutable", func(t *testing.T) {
		for _, mutation := range []func(platform.ReleaseAsset) platform.ReleaseAsset{
			func(a platform.ReleaseAsset) platform.ReleaseAsset { a.SourceVersion = "2"; return a },
			func(a platform.ReleaseAsset) platform.ReleaseAsset { a.ContractVersion = 2; return a },
			func(a platform.ReleaseAsset) platform.ReleaseAsset {
				fields := body(a)
				fields["roles"] = json.RawMessage(`["clerk"]`)
				return withBody(a, fields)
			},
			func(a platform.ReleaseAsset) platform.ReleaseAsset {
				fields := body(a)
				fields["action"] = json.RawMessage(`{"schema":"stock.item.count","target":"stock.item","payload":[]}`)
				return withBody(a, fields)
			},
			func(a platform.ReleaseAsset) platform.ReleaseAsset {
				fields := body(a)
				fields["objectBindings"] = json.RawMessage(`{"stock.item":{"app":"other","kind":"object","name":"stock.item"}}`)
				return withBody(a, fields)
			},
			func(a platform.ReleaseAsset) platform.ReleaseAsset {
				a.Requires = append(slices.Clone(a.Requires), platform.AssetRef{App: "other", Kind: platform.AssetObject, Name: "other.secret"})
				return a
			},
		} {
			check(currentAction, mutation(legacy(currentAction)), false)
		}
	})
	t.Run("new descriptors require exact dependencies", func(t *testing.T) {
		missing := currentAction
		missing.Requires = nil
		check(currentAction, missing, false)
		check(currentAction, currentAction, true)
	})
	t.Run("security and lifecycle body still match exactly", func(t *testing.T) {
		current := asset(object, map[string]any{"type": object.Name, "fields": []platform.FieldInfo{}, "scope": map[string]any{"default": "own"}, "lifecycle": map[string]any{"Initial": "draft"}})
		retained := legacy(current)
		check(current, retained, true)
		fields := body(retained)
		fields["scope"] = json.RawMessage(`{"default":"tenant"}`)
		check(current, withBody(retained, fields), false)
		fields = body(retained)
		fields["lifecycle"] = json.RawMessage(`{"Initial":"approved"}`)
		check(current, withBody(retained, fields), false)
	})
	t.Run("application adds only new protocol and interface descriptors", func(t *testing.T) {
		protocol := platform.AssetRef{App: "stock", Kind: platform.AssetProtocol, Name: "inventory"}
		shape := platform.AssetRef{App: "stock", Kind: platform.AssetInterface, Name: "stock.item-shape"}
		application := platform.Application{Name: "stock", Title: "Stock", Resources: []platform.AssetRef{object, protocol, shape}}
		current := asset(platform.AssetRef{App: "stock", Kind: platform.AssetApp, Name: "stock"}, application)
		application.Resources = []platform.AssetRef{object}
		retained := withBody(legacy(current), application)
		retained.Requires = []platform.AssetRef{object}
		check(current, retained, true)
		application.Title = "Changed title"
		check(current, withBody(retained, application), false)
	})
	t.Run("native flow compiled bindings cannot change", func(t *testing.T) {
		flow := platform.FlowReleaseDescriptor{Native: true, Name: "stock.count", Subject: object, Actions: []platform.AssetRef{action}, Definition: json.RawMessage(`{"name":"count","policies":[{"name":"count","do":true}]}`)}
		current := asset(platform.AssetRef{App: "stock", Kind: platform.AssetFlow, Name: "stock.count"}, flow)
		retained := legacy(current)
		check(current, retained, true)
		flow.Definition = json.RawMessage(`{"name":"count","policies":[{"name":"count","do":false}]}`)
		check(current, withBody(retained, flow), false)
		flow.Definition = json.RawMessage(`{"name":"count","policies":[{"name":"count","do":true}]}`)
		flow.Actions = nil
		check(current, withBody(retained, flow), false)
	})
	t.Run("native agent retains original declared tools", func(t *testing.T) {
		agent := platform.AgentReleaseDescriptor{Name: "counter", Title: "Counter", Instructions: "Count items", Native: true, Tools: []string{"count"}, ToolBindings: map[string]platform.AssetRef{"count": {App: "stock", Kind: platform.AssetAction, Name: "count"}}}
		current := asset(platform.AssetRef{App: "stock", Kind: platform.AssetAgent, Name: "counter"}, agent)
		retained := legacy(current)
		retained.Requires = nil
		check(current, retained, true)
		agent.ToolBindings = nil
		agent.Instructions = "Change quantities"
		check(current, withBody(retained, agent), false)
	})
}
