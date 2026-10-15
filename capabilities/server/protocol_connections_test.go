package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/gateway"
	"platformserver/platform"
)

type protocolSampleSink func([]byte) (int, error)

func (f protocolSampleSink) Write(raw []byte) (int, error) { return f(raw) }

func TestProtocolConnectionsRealDriversObservationAndDispatch(t *testing.T) {
	directory := os.Getenv("PLATFORM_PROTOCOL_PACKAGES")
	if directory == "" {
		t.Skip("set PLATFORM_PROTOCOL_PACKAGES to the real output of make packages")
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, found := index.find("protocol.simulator", "0.1.3")
	if !found || descriptor.Runtime == nil {
		t.Fatal("make packages must provide protocol.simulator@0.1.3")
	}
	binary, err := descriptor.Runtime.Target()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the real executable, but isolate this test's additional package
	// version from the build directory used by the other integration cases.
	executable, err := os.ReadFile(filepath.Join(directory, "artifacts", strings.TrimPrefix(binary.Artifact, "sha256:")))
	if err != nil {
		t.Fatal(err)
	}
	directory = t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "artifacts", strings.TrimPrefix(binary.Artifact, "sha256:")), executable, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "simulator-0.1.3.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err = LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	tn := composeTenant(t, "protocol-connections", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user"), seatOf("reviewer", "build:reviewer")}, build.New("protocol-connections"), work.New("protocol-connections"))
	console := testConsoleOf(tn)
	console.index = index
	at := time.Now().UTC().Truncate(time.Second)
	decide(t, tn, "builder", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
	samples := make(chan platform.DriverSample, 32)
	sink := protocolSampleSink(func(raw []byte) (int, error) {
		var sample platform.DriverSample
		if json.Unmarshal(raw, &sample) == nil {
			select {
			case samples <- sample:
			default:
			}
		}
		return len(raw), nil
	})
	service, err := gateway.New(gateway.Config{Directory: directory, StateDir: t.TempDir(), Token: "protocol-integration-token", Samples: sink,
		Resolve: func(binding platform.DriverBinding) (platform.DriverRuntime, error) {
			return console.ResolveDriver(binding.Package, binding.Version, binding.Digest, binding.Profile)
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(service)
	defer server.Close()
	tn.Secrets = func(name string) ([]byte, bool) {
		return []byte("Bearer protocol-integration-token"), name == "gateway"
	}

	const typ = "build.protocoldevice"
	latest := platform.ExprReference("decimal", "input.latest")
	warning := platform.ExprLiteralValue("choice", "warning")
	waiting := platform.ExprLiteralValue("text", "waiting")
	warn := build.Action{Name: "warn", Title: "Record warning", From: []string{"normal"}, Roles: []string{"user"}, Inputs: []build.Input{{Name: "latest", Title: "Latest", Type: "decimal", Required: true}},
		Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{{Name: "warning", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "latest", Expr: &latest}, {Field: "state", Expr: &warning}}}}}}
	set := build.Action{Name: "set", Title: "Set device position", From: []string{"normal", "warning"}, Roles: []string{"user"},
		Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{{Name: "waiting", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", Expr: &waiting}}}}}}
	publishObject(t, tn, "builder", "OBJECT", map[string]any{
		"name": "protocoldevice", "title": "Protocol device", "fields": []build.Field{{Name: "device", Title: "Device", Type: "text"}, {Name: "limit", Title: "Limit", Type: "decimal"}, {Name: "latest", Title: "Latest", Type: "decimal"}, {Name: "status", Title: "Status", Type: "text"}},
		"states": []build.State{{Name: "normal", Title: "Normal"}, {Name: "warning", Title: "Warning"}}, "access": []build.Access{{Role: "user", Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all"}}, "actions": []build.Action{warn, set},
	}, at)
	decide(t, tn, "operator", build.ID, typ+".create", typ, "A", map[string]any{"device": "device-a", "limit": 35}, at)
	decide(t, tn, "operator", build.ID, typ+".create", typ, "B", map[string]any{"device": "device-b", "limit": 35}, at)
	binding := func(device, scenario, initial string) platform.DriverBinding {
		return platform.DriverBinding{Package: descriptor.ID, Version: descriptor.Version, Digest: binary.Artifact, Profile: "simulator", Configuration: platform.DriverConfiguration{Scenario: scenario, Seed: 42,
			Devices: []platform.DriverDevice{{ID: device, Title: device}}, Points: []platform.DriverPoint{{ID: "temperature", Device: device, Address: initial, Type: "decimal", Unit: "C", IntervalMS: 1000}, {ID: "position", Device: device, Address: "0", Type: "decimal", Unit: "code", IntervalMS: 1000}}, Commands: []platform.DriverCommand{{ID: "setposition", Point: "position", Min: "0", Max: "100"}}}}
	}
	for _, test := range []struct {
		id      string
		binding platform.DriverBinding
	}{{"FIRST", binding("device-a", "anomaly", "20")}, {"SECOND", binding("device-b", "normal", "24")}} {
		decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, test.id, map[string]any{"name": strings.ToLower(test.id), "title": test.id, "kind": "http", "address": server.URL, "secret": "gateway", "allowPrivate": true, "protocol": test.binding}, at)
		decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, test.id, map[string]any{}, at)
		tn.CheckConnections(at)
		connection, _ := platform.Get[build.Connection](tn.automation(build.ID, false), test.id)
		if connection.Last == nil || !connection.Last.OK || connection.Last.BindingRevision != test.binding.Revision() {
			t.Fatalf("real driver connection %s: %+v", test.id, connection.Last)
		}
	}

	waitSample := func(revision string) platform.DriverSample {
		t.Helper()
		timeout := time.NewTimer(3 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case sample := <-samples:
				if sample.Source.Connection == "SECOND" && sample.Source.Revision == revision && sample.Signal == "temperature" {
					return sample
				}
			case <-timeout.C:
				t.Fatalf("no external sample arrived from binding revision %s", revision)
			}
		}
	}
	// Upgrade an actively sampling connection through the same Package owner.
	// The second version uses the exact same executable, not a test handler.
	old, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "SECOND")
	if err := tn.protocolHTTP(context.Background(), old, http.MethodGet, "source", nil, nil); err != nil {
		t.Fatal(err)
	}
	oldSample := waitSample(old.Protocol.Revision())
	upgraded := descriptor
	upgraded.Version = "0.2.0"
	if err := os.WriteFile(filepath.Join(directory, "simulator-0.2.0.json"), platform.Raw(upgraded), 0600); err != nil {
		t.Fatal(err)
	}
	newIndex, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	console.mu.Lock()
	console.index = newIndex
	console.mu.Unlock()
	decide(t, tn, "builder", PlatformApp, SchemaPackageUpgrade, PackageType, upgraded.ID, map[string]any{"id": upgraded.ID, "version": upgraded.Version}, at)
	currentBinding := binding("device-b", "unknown", "31")
	currentBinding.Version = upgraded.Version
	decide(t, tn, "builder", build.ID, "build.connection.edit", build.ConnectionType, "SECOND", map[string]any{"protocol": currentBinding}, at)
	decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, "SECOND", map[string]any{}, at)
	tn.CheckConnections(at)
	current, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "SECOND")
	if err := tn.protocolHTTP(context.Background(), current, http.MethodGet, "source", nil, nil); err != nil {
		t.Fatal(err)
	}
	newSample := waitSample(currentBinding.Revision())
	if oldSample.Value != "24" || newSample.Value != "31" || newSample.Source.Generation == oldSample.Source.Generation {
		t.Fatalf("package upgrade did not replace the sampling session: old=%+v new=%+v", oldSample, newSample)
	}
	// A retained read opens the former package and revision without making it writable.
	if err := tn.configureProtocol(context.Background(), old, true); err != nil {
		t.Fatal(err)
	}
	var retained platform.DriverReadResult
	if err := tn.protocolHTTP(context.Background(), old, "GET", "check", nil, &retained); err != nil {
		t.Fatal(err)
	}
	if len(retained.Samples) != 2 || retained.Samples[0].Value != "24" || retained.Samples[0].Source.Revision != old.Protocol.Revision() {
		t.Fatalf("retained revision used the new mapping: %+v", retained)
	}
	current, _ = platform.Get[build.Connection](tn.automation(build.ID, false), "SECOND")
	if current.Protocol.Revision() != currentBinding.Revision() || current.Last == nil || !current.Last.OK {
		t.Fatal("retained read altered the current published binding")
	}
	oldCommand := platform.DriverExecute{Dispatch: platform.DriverDispatchIdentity{RunID: "retained-run", AttemptID: "new-attempt", BusinessKey: "B", Deadline: time.Now().Add(time.Minute), BindingRevision: old.Protocol.Revision(), PackageDigest: old.Protocol.Digest}, Command: platform.DriverCommandValue{Name: "setposition", Value: "99"}}
	if err := tn.protocolHTTP(context.Background(), old, http.MethodPost, "execute", oldCommand, nil); err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("retained package revision accepted a new write: %v", err)
	}

	thermal := platform.AssetRef{App: build.ID, Kind: platform.AssetObservation, Name: "protocoltemperature"}
	decision := &platform.ObservationDecision{Action: platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: typ + ".warn"},
		When: platform.Expr{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("decimal", "observation.latest"), platform.ExprReference("decimal", "record.limit")}}, Inputs: map[string]platform.Expr{"latest": platform.ExprReference("decimal", "observation.latest")}}
	for _, observation := range []struct {
		id, name, connection, signal, unit string
		decision                           *platform.ObservationDecision
	}{{"THERMAL", thermal.Name, "FIRST", "temperature", "C", decision}, {"POSITION", "protocolposition", "SECOND", "position", "code", nil}} {
		decide(t, tn, "builder", build.ID, "build.observation.create", build.ObservationType, observation.id, map[string]any{"name": observation.name, "title": observation.name, "connection": observation.connection, "object": typ, "deviceField": "device", "signal": observation.signal, "unit": observation.unit, "path": "/observation", "windowSeconds": 60, "expectedRetentionSeconds": 3600, "expectedGranularitySeconds": 1, "roles": []string{"user", "builder"}, "requireComplete": true, "decision": observation.decision}, at)
		decide(t, tn, "builder", build.ID, build.SchemaObservation, build.ObservationType, observation.id, map[string]any{}, at)
	}
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	operator := memberOf(t, tn, "operator")
	read, refusal := tn.ReadObservation(context.Background(), operator, thermal, ObservationRead{Record: "A", Since: at.Add(-time.Minute), Until: at, Mode: "points", Limit: 100}, at)
	if refusal != nil || read.Window.Count != 60 || len(read.Points) != 60 || len(entries) != 0 {
		t.Fatalf("real protocol observation read: window=%+v points=%d refusal=%v writes=%d", read.Window, len(read.Points), refusal, len(entries))
	}
	answer, refusal := tn.EvaluateObservation(context.Background(), operator, thermal, ObservationEvaluate{Record: "A", Until: at, Key: "protocol-warning"}, at)
	if refusal != nil || !answer.Matched || answer.Receipt == nil || len(entries) != 1 {
		t.Fatalf("protocol window did not produce the original business decision: %+v %v", answer, refusal)
	}
	if bytes.Contains(entries[0].Body, []byte(`"receivedAt"`)) || bytes.Contains(entries[0].Body, []byte(`"points":[{"at":`)) || len(entries[0].Body) > 100000 {
		t.Fatal("business acceptance copied raw protocol samples")
	}
	record, found := tn.Held(typ + "/A")
	if !found || toMap(record)["state"] != "warning" || toMap(record)["latest"] != float64(1000) {
		t.Fatalf("observation bypassed or missed the business transaction: %+v", record)
	}

	// The current revision must still accept this new attempt after the old
	// read. Its lost reply is transport uncertainty; a fresh value proves completion.
	decide(t, tn, "builder", build.ID, "build.writeback.create", build.WritebackType, "COMMAND", map[string]any{"name": "protocolset", "title": "Set position", "connection": "SECOND", "object": typ, "on": "dispatch", "path": "execute"}, at)
	decide(t, tn, "builder", build.ID, "build.writeback.publish", build.WritebackType, "COMMAND", map[string]any{}, at)
	value := platform.ExprLiteralValue("decimal", "2")
	set.Transaction.Dispatch = &platform.Dispatch{Command: "setposition", Target: "SECOND", Key: "$subject", Payload: []platform.TxnSet{{Field: "value", Expr: &value}}, DeadlineSeconds: 120, OnStuck: "manual", OnFail: "manual", ReviewerRole: "reviewer",
		Readback: platform.DispatchRead{Name: "position", Observation: "POSITION", States: map[string]string{"0": "running", "2": "completed"}, EverySeconds: 1}}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "OBJECT", map[string]any{"actions": []build.Action{warn, set}, "dispatchCommands": []platform.DispatchCommand{{Name: "setposition", Title: "Set position", EndpointKind: "controller", Writeback: "COMMAND"}}}, at)
	decide(t, tn, "builder", build.ID, build.SchemaPublish, build.ObjectType, "OBJECT", map[string]any{}, at)
	sentAt := time.Now().UTC().Add(-100 * time.Millisecond)
	decide(t, tn, "operator", build.ID, typ+".set", typ, "B", map[string]any{}, sentAt)
	tn.Dispatch(sentAt)
	run := currentDeviceDispatch(t, tn)
	if run.State != "unknown" || run.Attempt != 1 {
		t.Fatalf("lost protocol reply became success or was refused by an incorrectly active old revision: %+v", run)
	}
	tn.ReadDispatches(sentAt.Add(5 * time.Second))
	run = currentDeviceDispatch(t, tn)
	if run.State != "completed" || run.Attempt != 1 || len(run.Readback) != 1 || run.Readback[0].Value != "2" {
		t.Fatalf("fresh physical readback did not finish the original dispatch: %+v", run)
	}
	var window platform.ObservationWindow
	if json.Unmarshal(run.Readback[0].Observed, &window) != nil || !window.LastAt.After(run.StartedAt) {
		t.Fatal("dispatch completion used a value older than the command")
	}
}
