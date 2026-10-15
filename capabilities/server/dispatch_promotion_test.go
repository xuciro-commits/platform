package platformserver

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestDeviceDispatchPromotesWithTargetLocalIntegrations(t *testing.T) {
	at := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	compose := func(id string) *Tenant {
		return composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user"), seatOf("reviewer", "build:reviewer")}, build.New(id), work.New(id))
	}
	prepare := func(tenant *Tenant, prefix, address, token string) {
		start := build.Action{Name: "send", Title: "Send movement", From: []string{"open"}, Roles: []string{build.User}, Transaction: &platform.BusinessTxn{Subject: "build.movement", Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.movement", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", From: "=waiting"}}}}}}
		publishObject(t, tenant, "builder", prefix+"-object", map[string]any{"name": "movement", "title": "Movement", "fields": []build.Field{{Name: "status", Title: "Status", Type: "text"}, {Name: "device", Title: "Device", Type: "text"}}, "states": []build.State{{Name: "open", Title: "Open"}}, "access": []build.Access{{Role: build.User, Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all"}}, "actions": []build.Action{start}}, at)
		tenant.Secrets = func(name string) ([]byte, bool) { return []byte("Bearer " + token), name == prefix+"-device-secret" }
		tenant.Outbound = func(*http.Request, bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
		decide(t, tenant, "builder", build.ID, "build.connection.create", build.ConnectionType, prefix+"-controller", map[string]any{"name": "controller", "title": "Controller", "kind": "http", "address": address, "secret": prefix + "-device-secret"}, at)
		decide(t, tenant, "builder", build.ID, "build.connection.check", build.ConnectionType, prefix+"-controller", map[string]any{}, at)
		tenant.CheckConnections(at)
		decide(t, tenant, "builder", build.ID, "build.writeback.create", build.WritebackType, prefix+"-command", map[string]any{"name": "sendmovement", "title": "Send movement", "connection": prefix + "-controller", "object": "build.movement", "on": "dispatch", "path": "/commands/{id}"}, at)
		decide(t, tenant, "builder", build.ID, "build.writeback.publish", build.WritebackType, prefix+"-command", map[string]any{}, at)
		decide(t, tenant, "builder", build.ID, "build.source.create", build.SourceType, prefix+"-status", map[string]any{"name": "movementstatus", "title": "Movement status", "connection": prefix + "-controller", "profile": "json", "url": "/status", "object": "build.movement", "key": "id", "mapping": []build.SourceField{{From: "state", To: "status"}}}, at)
		decide(t, tenant, "builder", build.ID, "build.source.publish", build.SourceType, prefix+"-status", map[string]any{}, at)
	}
	source, target := compose("dispatch-dev"), compose("dispatch-target")
	prepare(source, "source", "https://source-device.example/api", "source-only-device-token")
	object, _ := platform.Get[build.Object](source.automation(build.ID, false), "source-object")
	object.DispatchCommands = []platform.DispatchCommand{{Name: "move", Title: "Move", EndpointKind: "controller", Writeback: "source-command"}}
	object.Actions[0].Transaction.Dispatch = &platform.Dispatch{Command: "move", Target: "source-controller", Key: "$subject", DeadlineSeconds: 10, OnStuck: "manual", OnFail: "manual", ReviewerRole: "reviewer", Readback: platform.DispatchRead{Name: "movementstatus", Source: "source-status", KeyField: "id", ResultField: "state", EverySeconds: 1, States: map[string]string{"active": "running", "done": "completed"}}}
	decide(t, source, "builder", build.ID, "build.object.edit", build.ObjectType, "source-object", map[string]any{"dispatchCommands": object.DispatchCommands, "actions": object.Actions}, at)
	decide(t, source, "builder", build.ID, build.SchemaPublish, build.ObjectType, "source-object", map[string]any{}, at)
	builder := memberOf(t, source, "builder")
	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "source-object"}}
	preview, err := source.PreviewReleaseDrafts(builder, drafts)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("dispatch candidate: %+v %v", preview, err)
	}
	if _, err := source.SaveReleaseCandidates(builder, drafts, preview.CandidateID, "dispatch-release", at); err != nil {
		t.Fatal(err)
	}
	if _, err := source.SealCandidate(preview.CandidateID, at); err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(preview.CandidateID, source.releases.raw(preview.CandidateID))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"https://source-device.example/api", "source-device-secret", "source-only-device-token", `"source-controller"`, `"source-command"`, `"source-status"`} {
		if bytes.Contains(candidate.Bytes, []byte(forbidden)) {
			t.Fatalf("portable dispatch retains source binding %q", forbidden)
		}
	}
	prepare(target, "target", "https://target-device.example/api", "target-device-token")
	if _, err := PromoteCandidate(source, target, candidate.ID, "promote-dispatch", true, memberOf(t, target, "builder"), at); err != nil {
		t.Fatal(err)
	}
	posts, reads := 0, 0
	target.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
		if request.URL.Host != "target-device.example" || request.Header.Get("Authorization") != "Bearer target-device-token" {
			t.Fatalf("dispatch escaped the target binding: %s", request.URL)
		}
		if request.Method == http.MethodPost {
			posts++
			if request.Header.Get("Idempotency-Key") != "M1" {
				t.Fatal("promotion changed the stable business key")
			}
			return confirmationResponse(503, `{}`)
		}
		reads++
		return confirmationResponse(200, `[{"id":"M1","state":"done"}]`)
	}
	run := startDeviceDispatch(t, target, at)
	if run.Target != "target-controller" || run.Command.Writeback != "target-command" || run.Definition.Readback.Source != "target-status" {
		t.Fatalf("dispatch retained source record IDs: target=%s command=%s source=%s", run.Target, run.Command.Writeback, run.Definition.Readback.Source)
	}
	target.Dispatch(at)
	target.ReadDispatches(at.Add(time.Second))
	if run = currentDeviceDispatch(t, target); posts != 1 || reads != 1 || run.State != "completed" {
		t.Fatalf("promoted dispatch did not complete: sends=%d reads=%d state=%s", posts, reads, run.State)
	}
	if !bytes.Equal(candidate.Bytes, source.releases.raw(candidate.ID)) {
		t.Fatal("promotion changed the sealed source candidate")
	}
}
