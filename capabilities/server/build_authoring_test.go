package platformserver

import "platformserver/studio/build"

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/work"
	automation "platformserver/engine/automation"
	"platformserver/platform"
)

func TestDraftActionCapabilitiesDescribeSavedInputsWithoutInstallingThem(t *testing.T) {
	const tenant = "draft-action-capabilities"
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("builder", "build:builder"), seatOf("user", "build:user"), seatOf("publisher", "build:publisher")}, build.New(tenant))
	builder := memberOf(t, tn, "builder")
	const object = "build.authoring"
	peerObject := publishObject(t, tn, "builder", "PEER", map[string]any{"name": "authoringpeer", "title": "Authoring peer", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: object + ".review"}
	action := build.Action{Name: "review", Title: "Review", From: []string{"ready"}, To: "done", Inputs: []build.Input{
		{Name: "peer", Title: "Peer", Type: "reference", Ref: peerObject, Required: true},
		{Name: "note", Title: "Note", Type: "text"},
	}}
	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "OBJECT", map[string]any{
		"name": "authoring", "title": "Authoring", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}},
		"states": []build.State{{Name: "ready", Title: "Ready"}, {Name: "done", Title: "Done"}}, "actions": []build.Action{action},
	}, at)
	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "ARCHIVED", map[string]any{"name": "archived", "title": "Archived", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, tn, "builder", build.ID, "build.object.archive", build.ObjectType, "ARCHIVED", map[string]any{}, at)
	for _, who := range []string{"user", "publisher"} {
		if capabilities, err := tn.DraftActionCapabilities(memberOf(t, tn, who)); err == nil || len(capabilities) != 0 {
			t.Fatalf("%s read builder draft action declarations: %+v %v", who, capabilities, err)
		}
	}
	capabilities, err := tn.DraftActionCapabilities(builder)
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(capabilities, func(capability platform.CapabilityDescriptor) bool { return capability.Ref == ref })
	if index < 0 {
		t.Fatal("saved object action was missing from authoring metadata")
	}
	draft := capabilities[index]
	peer := slices.IndexFunc(draft.Parameters, func(field platform.Field) bool { return field.Name == "peer" })
	if draft.Source != "draft" || draft.Version != "" || draft.Target != object || peer < 0 || draft.Parameters[peer].Ref != peerObject || !draft.Parameters[peer].Required || draft.Input == nil || !slices.Contains(draft.Input.Required, "peer") {
		t.Fatalf("draft action lost its original target or required reference input: %+v", draft)
	}
	for _, capability := range capabilities {
		if capability.Target == "build.archived" {
			t.Fatal("archived object remained in draft action metadata")
		}
	}
	for _, capability := range tn.Capabilities(builder) {
		if capability.Target == object || strings.HasPrefix(capability.Ref.Name, object+".") {
			t.Fatal("draft-only action entered current capabilities")
		}
	}
	for _, definition := range tn.Definitions(builder) {
		if definition.Ref.Name == object || strings.HasPrefix(definition.Ref.Name, object+".") {
			t.Fatal("draft-only object or action entered installed definitions")
		}
	}
	if got := refuse(t, tn, "builder", build.ID, ref.Name, object, "R", map[string]any{"peer": "R"}, at); got == "ok" {
		t.Fatal("reading draft metadata made the unpublished action executable")
	}
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{}, at)
	before, _ := platform.Get[build.Object](tn.automation(build.ID, false), "OBJECT")
	definitions := tn.Definitions(builder)
	published := slices.IndexFunc(definitions, func(definition platform.Definition) bool { return definition.Ref == ref })
	if published < 0 {
		t.Fatal("publication did not install the original action")
	}
	original := platform.Raw(definitions[published])
	action.Inputs = append(action.Inputs, build.Input{Name: "reason", Title: "Reason", Type: "text", Required: true})
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "OBJECT", map[string]any{"actions": []build.Action{action}}, at)
	capabilities, err = tn.DraftActionCapabilities(builder)
	if err != nil {
		t.Fatal(err)
	}
	index = slices.IndexFunc(capabilities, func(capability platform.CapabilityDescriptor) bool { return capability.Ref == ref })
	if index < 0 || capabilities[index].Input == nil || !slices.Contains(capabilities[index].Input.Required, "reason") {
		t.Fatal("saved action input change was not reflected in authoring metadata")
	}
	definitions = tn.Definitions(builder)
	published = slices.IndexFunc(definitions, func(definition platform.Definition) bool { return definition.Ref == ref })
	after, _ := platform.Get[build.Object](tn.automation(build.ID, false), "OBJECT")
	if after.Published != before.Published || published < 0 || !bytes.Equal(original, platform.Raw(definitions[published])) {
		t.Fatal("draft input edit or discovery rewrote the original publication")
	}
	for _, capability := range tn.Capabilities(builder) {
		if capability.Ref == ref && capability.Input != nil && slices.Contains(capability.Input.Required, "reason") {
			t.Fatal("draft input change entered current executable capabilities")
		}
	}
	decide(t, tn, "builder", build.ID, peerObject+".create", peerObject, "R", map[string]any{"note": "Peer"}, at)
	decide(t, tn, "builder", build.ID, object+".create", object, "R", map[string]any{"note": "Original runtime"}, at)
	decide(t, tn, "builder", build.ID, ref.Name, object, "R", map[string]any{"peer": "R"}, at)
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "OBJECT", map[string]any{"actions": []build.Action{}}, at)
	process := build.Process{Name: "removedaction", Title: "Removed action", Manual: true, Steps: []build.ProcessStep{{Name: "review", Kind: "action", Act: ref.Name, Target: &platform.Binding{Source: "literal", Value: platform.Raw("R")}, Inputs: map[string]platform.Binding{"peer": {Source: "literal", Value: platform.Raw("R")}}}}}
	if problem := tn.app(build.ID).(*build.Build).CheckProcess(platform.NewCaller(runtime{tn}, builder, build.ID, false, false), process); problem == nil {
		t.Fatal("a removed draft action fell back to its original published declaration")
	}
}

func TestManualAndScheduledProcessesUseSelectedDraftActions(t *testing.T) {
	for _, manual := range []bool{true, false} {
		t.Run(map[bool]string{true: "manual", false: "schedule"}[manual], func(t *testing.T) {
			const tenant = "explicit-draft-process"
			at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
			tn := builderTenant(t, tenant, work.New(tenant), automation.New(tenant))
			member := memberOf(t, tn, "builder")
			const object = "build.drafttarget"
			decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "OBJECT", map[string]any{"name": "drafttarget", "title": "Draft target", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
			process := build.Process{Record: platform.Record{ID: "FLOW"}, Name: "explicitdraft", Title: "Explicit draft", Manual: manual, Steps: []build.ProcessStep{{Name: "create", Kind: "action", Act: object + ".create", Target: &platform.Binding{Source: "literal", Value: platform.Raw("R")}, Inputs: map[string]platform.Binding{"note": {Source: "literal", Value: platform.Raw("Synthetic")}}}}}
			if !manual {
				process.Kind, process.Every = "automation", "1h"
			}
			caller := platform.NewCaller(runtime{tn}, member, build.ID, false, false)
			if problem := tn.app(build.ID).(*build.Build).CheckProcess(caller, process); problem != nil {
				t.Fatalf("draft action with an explicit target could not be checked: %+v", problem)
			}
			decide(t, tn, "builder", build.ID, "build.process.create", build.ProcessType, "FLOW", map[string]any{"name": process.Name, "title": process.Title, "manual": process.Manual, "kind": process.Kind, "every": process.Every, "steps": process.Steps}, at)
			if got := refuse(t, tn, "builder", build.ID, "build.process.publish", build.ProcessType, "FLOW", map[string]any{}, at); got == "ok" {
				t.Fatal("direct publication resolved an unselected object draft")
			}
			if alone, err := tn.PreviewRelease(member, platform.AssetFlow, "FLOW"); err == nil && alone.Diagnostic == "" {
				t.Fatal("flow-only candidate resolved an unselected object draft")
			}
			selection := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "OBJECT"}, {Kind: platform.AssetFlow, ID: "FLOW"}}
			joint, err := tn.PreviewReleaseDrafts(member, selection)
			if err != nil || joint.Diagnostic != "" || joint.CandidateID == "" {
				t.Fatalf("explicit object and workflow selection: %+v %v", joint, err)
			}
		})
	}
}
