package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func deviceDispatchTenant(t *testing.T) *Tenant {
	t.Helper()
	return composeTenant(t, "dispatch", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user"), seatOf("reviewer", "build:reviewer")}, build.New("dispatch"), work.New("dispatch"))
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

func TestDeviceDispatchUnsafeUnknownReadsRealCompletionAndRecovers(t *testing.T) {
	at := time.Date(2026, 10, 15, 16, 0, 0, 0, time.UTC)
	tn := deviceDispatchTenant(t)
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	prepareDeviceDispatch(t, tn, at, "source", false)
	posts, status := 0, ""
	tn.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
		if request.Method == http.MethodPost {
			posts++
			if request.Header.Get("Idempotency-Key") != "M1" {
				t.Fatal("command lost the stable subject key")
			}
			return confirmationResponse(503, `{}`)
		}
		if status == "" {
			return confirmationResponse(200, `[]`)
		}
		return confirmationResponse(200, `[{"id":"M1","state":"`+status+`"}]`)
	}
	run := startDeviceDispatch(t, tn, at)
	tn.Dispatch(at)
	run = currentDeviceDispatch(t, tn)
	if run.State != "unknown" || posts != 1 {
		t.Fatalf("unsafe delivery state=%s sends=%d fault=%v", run.State, posts, tn.fault.Load())
	}
	tn.Dispatch(at.Add(time.Hour))
	if posts != 1 {
		t.Fatal("an uncertain unsafe command was automatically resent")
	}
	if result := refuse(t, tn, "builder", PlatformApp, SchemaEffectRetry, EffectType, run.Intent, map[string]any{}, at); result == "ok" {
		t.Fatal("console retry bypassed the command reviewer")
	}
	tn.ReadDispatches(at.Add(time.Second))
	if run = currentDeviceDispatch(t, tn); run.State != "unknown" || len(run.Readback) != 1 {
		t.Fatalf("missing status should remain unknown: %+v", run)
	}
	status = "active"
	tn.ReadDispatches(at.Add(2 * time.Second))
	if run = currentDeviceDispatch(t, tn); run.State != "running" {
		t.Fatalf("status did not reach running: %+v", run)
	}
	status = "done"
	tn.ReadDispatches(at.Add(3 * time.Second))
	if run = currentDeviceDispatch(t, tn); run.State != "completed" || posts != 1 {
		t.Fatalf("real completion was not retained: state=%s sends=%d", run.State, posts)
	}
	CheckReplay(t, tn, entries, func() *Tenant { return deviceDispatchTenant(t) })
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

func TestDeviceDispatchHumanRetryRetargetAndComplete(t *testing.T) {
	at := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	tn := deviceDispatchTenant(t)
	prepareDeviceDispatch(t, tn, at, "source", false)
	posts := 0
	var hosts []string
	tn.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
		if request.Method != http.MethodPost {
			return confirmationResponse(200, `{}`)
		}
		posts++
		hosts = append(hosts, request.URL.Host)
		if request.Header.Get("Idempotency-Key") != "M1" || request.URL.Path != "/api/commands/M1" {
			t.Fatalf("retry changed frozen request identity: %s %s", request.URL, request.Header.Get("Idempotency-Key"))
		}
		return nil, io.ErrUnexpectedEOF
	}
	run := startDeviceDispatch(t, tn, at)
	tn.Dispatch(at)
	if result := refuse(t, tn, "operator", build.ID, platform.DispatchReviewSchema, platform.DispatchType, run.ID, map[string]any{"outcome": "retry", "reason": "No task exists at the controller"}, at); result == "ok" {
		t.Fatal("applicant impersonated the declared reviewer")
	}
	decide(t, tn, "reviewer", build.ID, platform.DispatchReviewSchema, platform.DispatchType, run.ID, map[string]any{"outcome": "retry", "reason": "Controller confirms the task was not created"}, at.Add(time.Second))
	tn.Dispatch(at.Add(time.Second))
	decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, "SECOND", map[string]any{"name": "secondcontroller", "title": "Second controller", "kind": "http", "address": "https://second.example/api"}, at)
	decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, "SECOND", map[string]any{}, at)
	tn.CheckConnections(at)
	targets, refusal := tn.DispatchTargets(memberOf(t, tn, "reviewer"), run.ID, at)
	if refusal != nil || !slices.ContainsFunc(targets, func(target DispatchTarget) bool { return target.ID == "SECOND" && target.Title == "Second controller" }) {
		t.Fatalf("reviewer cannot choose the replacement target: %+v %v", targets, refusal)
	}
	if _, refusal := tn.DispatchTargets(memberOf(t, tn, "operator"), run.ID, at); refusal == nil {
		t.Fatal("applicant read targets reserved for the current reviewer")
	}
	decide(t, tn, "reviewer", build.ID, platform.DispatchReviewSchema, platform.DispatchType, run.ID, map[string]any{"outcome": "retarget", "target": "SECOND", "reason": "First controller confirms no work exists; use standby"}, at.Add(2*time.Second))
	tn.Dispatch(at.Add(2 * time.Second))
	if posts != 3 || hosts[2] != "second.example" {
		t.Fatalf("authorized attempts=%d hosts=%v", posts, hosts)
	}
	decide(t, tn, "reviewer", build.ID, platform.DispatchReviewSchema, platform.DispatchType, run.ID, map[string]any{"outcome": "complete", "reason": "Operator confirmed the physical move at the destination"}, at.Add(3*time.Second))
	run = currentDeviceDispatch(t, tn)
	if run.State != "completed" || run.Attempt != 3 || len(run.Attempts) != 3 {
		t.Fatalf("manual result lost original attempt history: %+v", run)
	}
	tn.Dispatch(at.Add(time.Hour))
	if posts != 3 {
		t.Fatal("a completed command was sent again")
	}
}

func TestDeviceDispatchRecordAndObservationReadbacks(t *testing.T) {
	for _, kind := range []string{"record", "observation"} {
		t.Run(kind, func(t *testing.T) {
			at := time.Date(2026, 10, 15, 19, 0, 0, 0, time.UTC)
			tn := deviceDispatchTenant(t)
			prepareDeviceDispatch(t, tn, at, kind, true)
			tn.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
				if !strings.HasSuffix(request.URL.Path, "/observations") {
					return confirmationResponse(200, `{}`)
				}
				var input struct {
					Device       string
					Since, Until time.Time
				}
				if json.NewDecoder(request.Body).Decode(&input) != nil || input.Device != "controller-1" {
					t.Fatal("observation lost its frozen device")
				}
				sampleAt := input.Since.Add(input.Until.Sub(input.Since) / 2)
				window := platform.ObservationWindow{Since: input.Since, Until: input.Until, Count: 1, FirstAt: sampleAt, LastAt: sampleAt, First: "2", Last: "2", Min: "2", Max: "2", Mean: "2", Sum: "2", Unit: "code"}
				return confirmationResponse(200, string(platform.Raw(map[string]any{"window": window})))
			}
			startDeviceDispatch(t, tn, at)
			tn.Dispatch(at)
			if currentDeviceDispatch(t, tn).State != "accepted" {
				t.Fatal("transport acceptance was treated as execution completion")
			}
			if kind == "record" {
				decide(t, tn, "operator", build.ID, "build.movement.edit", "build.movement", "M1", map[string]any{"status": "done"}, at)
			}
			tn.ReadDispatches(at.Add(time.Second))
			if run := currentDeviceDispatch(t, tn); run.State != "completed" || len(run.Readback) != 1 {
				t.Fatalf("%s status not connected: state=%s reason=%s fault=%v", kind, run.State, run.Reason, tn.fault.Load())
			}
		})
	}
}

func TestDeviceDispatchCompensationRequiresItsOwnEvidence(t *testing.T) {
	for _, kind := range []string{"source", "record", "observation"} {
		t.Run(kind, func(t *testing.T) {
			at := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
			tn := deviceDispatchTenant(t)
			prepareDeviceDispatch(t, tn, at, kind, true)
			run := startDeviceDispatch(t, tn, at)
			run.Compensating, run.Intent = true, "reverse-attempt"
			run.Attempts = append(run.Attempts, platform.DispatchAttempt{Intent: run.Intent, At: at.Add(time.Second), Compensation: true})
			ownEvidence := false
			tn.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
				if kind == "observation" {
					var input struct{ Since, Until time.Time }
					if json.NewDecoder(request.Body).Decode(&input) != nil {
						t.Fatal("invalid observation request")
					}
					sampleAt := at
					if ownEvidence {
						sampleAt = at.Add(2 * time.Second)
					}
					window := platform.ObservationWindow{Since: input.Since, Until: input.Until, Count: 1, FirstAt: sampleAt, LastAt: sampleAt, First: "2", Last: "2", Min: "2", Max: "2", Mean: "2", Sum: "2", Unit: "code"}
					return confirmationResponse(200, string(platform.Raw(map[string]any{"window": window})))
				}
				rows := []map[string]any{{"id": "M1", "state": "done"}}
				if ownEvidence {
					rows = append(rows, map[string]any{"id": "M1:compensate", "state": "done"})
				}
				return confirmationResponse(200, string(platform.Raw(rows)))
			}
			if kind == "record" {
				decide(t, tn, "operator", build.ID, "build.movement.edit", "build.movement", "M1", map[string]any{"status": "done"}, at)
			}
			if reading := tn.readDispatchStatus(run, at.Add(3*time.Second)); reading.State != "unknown" {
				t.Fatalf("the original command completed its compensation: %+v", reading)
			}
			ownEvidence = true
			if kind == "record" {
				decide(t, tn, "operator", build.ID, "build.movement.create", "build.movement", "M1:compensate", map[string]any{"status": "done"}, at)
			}
			if reading := tn.readDispatchStatus(run, at.Add(3*time.Second)); reading.State != "completed" {
				t.Fatalf("the compensation's own completion was not read: %+v", reading)
			}
		})
	}
}
