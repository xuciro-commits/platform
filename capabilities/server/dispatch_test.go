package platformserver

import "platformserver/studio/build"

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"platformserver/apps/work"
	"platformserver/platform"
)

func deviceDispatchTenant(t *testing.T) *Tenant {
	t.Helper()
	return composeTenant(t, "dispatch", []platform.Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user"), seatOf("reviewer", "build:reviewer")}, build.New("dispatch"), work.New("dispatch"))
}

func prepareDeviceDispatch(t *testing.T, tn *Tenant, at time.Time, readKind string, safe bool) *platform.Dispatch {
	t.Helper()
	start := build.Action{Name: "send", Title: "Send movement", From: []string{"open"}, Roles: []string{build.User}, Transaction: &platform.BusinessTxn{Subject: "build.movement", Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.movement", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", From: "=waiting"}}}}}}
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "movement", "title": "Movement", "fields": []build.Field{{Name: "status", Title: "Status", Type: "text"}, {Name: "device", Title: "Device", Type: "text"}}, "states": []build.State{{Name: "open", Title: "Open"}}, "access": []build.Access{{Role: build.User, Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all"}}, "actions": []build.Action{start}}, at)
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
	decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, "TARGET", map[string]any{"name": "controller", "title": "Controller", "kind": "http", "address": "https://controller.example/api"}, at)
	decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, "TARGET", map[string]any{}, at)
	tn.CheckConnections(at)
	decide(t, tn, "builder", build.ID, "build.writeback.create", build.WritebackType, "COMMAND", map[string]any{"name": "sendmovement", "title": "Send movement", "connection": "TARGET", "object": "build.movement", "on": "dispatch", "path": "/commands/{id}"}, at)
	decide(t, tn, "builder", build.ID, "build.writeback.publish", build.WritebackType, "COMMAND", map[string]any{}, at)
	read := platform.DispatchRead{Name: "movementstatus", KeyField: "id", ResultField: "state", States: map[string]string{"active": "running", "done": "completed", "blocked": "stuck", "failed": "failed"}, EverySeconds: 1}
	switch readKind {
	case "source":
		decide(t, tn, "builder", build.ID, "build.source.create", build.SourceType, "SOURCE", map[string]any{"name": "movementstatus", "title": "Movement status", "connection": "TARGET", "profile": "json", "url": "/status", "object": "build.movement", "key": "id", "mapping": []build.SourceField{{From: "state", To: "status"}}}, at)
		decide(t, tn, "builder", build.ID, "build.source.publish", build.SourceType, "SOURCE", map[string]any{}, at)
		read.Source = "SOURCE"
	case "record":
		read.Record, read.ResultField = "build.movement", "status"
	case "observation":
		decide(t, tn, "builder", build.ID, "build.observation.create", build.ObservationType, "OBSERVATION", map[string]any{"name": "movementstatus", "title": "Movement status", "connection": "TARGET", "object": "build.movement", "deviceField": "device", "signal": "state", "unit": "code", "path": "/observations", "windowSeconds": 60, "expectedRetentionSeconds": 3600, "expectedGranularitySeconds": 1, "roles": []string{"user", "builder"}}, at)
		decide(t, tn, "builder", build.ID, build.SchemaObservation, build.ObservationType, "OBSERVATION", map[string]any{}, at)
		read.Observation, read.KeyField, read.ResultField, read.States = "OBSERVATION", "", "", map[string]string{"1": "running", "2": "completed"}
	}
	spec := &platform.Dispatch{Command: "move", Target: "TARGET", Key: "$subject", DeadlineSeconds: 10, Readback: read, OnStuck: "manual", OnFail: "manual", ReviewerRole: "reviewer"}
	start.Transaction.Dispatch = spec
	commands := []platform.DispatchCommand{{Name: "move", Title: "Move", EndpointKind: "controller", Writeback: "COMMAND", Idempotent: safe}}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "OBJECT", map[string]any{"actions": []build.Action{start}, "dispatchCommands": commands}, at)
	decide(t, tn, "builder", build.ID, build.SchemaPublish, build.ObjectType, "OBJECT", map[string]any{}, at)
	return spec
}

func startDeviceDispatch(t *testing.T, tn *Tenant, at time.Time) platform.DispatchRun {
	t.Helper()
	decide(t, tn, "operator", build.ID, "build.movement.create", "build.movement", "M1", map[string]any{"status": "new", "device": "controller-1"}, at)
	decide(t, tn, "operator", build.ID, "build.movement.send", "build.movement", "M1", map[string]any{}, at)
	return currentDeviceDispatch(t, tn)
}

func currentDeviceDispatch(t *testing.T, tn *Tenant) platform.DispatchRun {
	t.Helper()
	runs, _, refusal := platform.Find[platform.DispatchRun](tn.automation(build.ID, false), platform.Query{})
	if refusal != nil || len(runs) != 1 {
		t.Fatalf("dispatch runs=%d refusal=%v", len(runs), refusal)
	}
	return runs[0]
}
func TestDeviceDispatchUnsafeCrashDoesNotSendAgain(t *testing.T) {
	at := time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
	ctx, directory := context.Background(), t.TempDir()
	journal, err := OpenFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	tn := deviceDispatchTenant(t)
	if _, err := journal.Entries(ctx, tn.ID, 0); err != nil {
		t.Fatal(err)
	}
	tn.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, tn.ID, entry, key, hash)
	}
	prepareDeviceDispatch(t, tn, at, "source", false)
	startDeviceDispatch(t, tn, at)
	claimed, sends := false, 0
	tn.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		if claimed {
			return nil, errors.New("crash before transport result append")
		}
		raw, err := journal.AppendAccepted(ctx, tn.ID, entry, key, hash)
		claimed = err == nil && bytes.Contains(entry.Body, []byte("Dispatch attempt started"))
		return raw, err
	}
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) { sends++; return confirmationResponse(200, `{}`) }
	tn.Dispatch(at)
	if !claimed || sends != 1 || currentDeviceDispatch(t, tn).State != "unknown" {
		t.Fatalf("unsafe attempt was not durably claimed: claimed=%t sends=%d", claimed, sends)
	}
	journal.Close()
	reopened, err := OpenFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, tn.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	fresh := deviceDispatchTenant(t)
	if err := fresh.Replay(entries); err != nil {
		t.Fatal(err)
	}
	fresh.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("recovery resent an unsafe command")
		return nil, io.ErrUnexpectedEOF
	}
	fresh.Dispatch(at.Add(time.Hour))
	if currentDeviceDispatch(t, fresh).State != "unknown" {
		t.Fatal("recovery changed uncertain execution into failure")
	}
}
