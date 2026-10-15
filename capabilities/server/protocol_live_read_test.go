package platformserver

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/drivers/modbus"
	"platformserver/drivers/s7"
	"platformserver/gateway"
	"platformserver/platform"
)

func TestProtocolConnectionLoadsReadOnlyPackages(t *testing.T) {
	directory := os.Getenv("PLATFORM_PROTOCOL_PACKAGES")
	if directory == "" {
		t.Skip("set PLATFORM_PROTOCOL_PACKAGES to the real output of make packages")
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []struct {
		id, profile string
		configure   func(*testing.T) (platform.DriverConfiguration, map[string]any)
	}{
		{"protocol.s7-1200", s7.Profile, s7ReadConfiguration},
		{"protocol.modbus-tcp", modbus.Profile, modbusReadConfiguration},
	} {
		t.Run(protocol.id, func(t *testing.T) {
			descriptor, found := index.find(protocol.id, "0.1.3")
			if !found || descriptor.Runtime == nil {
				t.Fatal("make packages must provide", protocol.id+"@0.1.3")
			}
			artifact, err := descriptor.Runtime.Target()
			if err != nil {
				t.Fatal(err)
			}
			configuration, wants := protocol.configure(t)

			tn := composeTenant(t, "protocol-read", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user"), seatOf("outsider", "build:observer")}, build.New("protocol-read"))
			console := testConsoleOf(tn)
			console.index = index
			at := time.Now().UTC()
			decide(t, tn, "builder", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
			service, err := gateway.New(gateway.Config{Directory: directory, StateDir: t.TempDir(), Token: "read-integration-token", Samples: io.Discard,
				Resolve: func(binding platform.DriverBinding) (platform.DriverRuntime, error) {
					return console.ResolveDriver(binding.Package, binding.Version, binding.Digest, binding.Profile)
				}})
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			server := httptest.NewServer(service)
			defer server.Close()
			tn.Secrets = func(name string) ([]byte, bool) { return []byte("Bearer read-integration-token"), name == "gateway" }
			binding := platform.DriverBinding{Package: descriptor.ID, Version: descriptor.Version, Digest: artifact.Artifact, Profile: protocol.profile, Configuration: configuration}
			decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, "PLC", map[string]any{"name": "plc", "title": "Local protocol test service", "kind": "http", "address": server.URL, "secret": "gateway", "allowPrivate": true, "protocol": binding}, at)
			decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, "PLC", map[string]any{}, at)
			tn.CheckConnections(at)
			connection, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "PLC")
			if connection.Last == nil || !connection.Last.OK || connection.Last.BindingRevision != binding.Revision() || len(connection.Last.Samples) != 0 {
				t.Fatalf("package did not load through the original connection check: %+v", connection.Last)
			}
			connection.ID = "preview-" + binding.Revision()[:24]
			preview := tn.checkProtocolConnection(context.Background(), connection, at)
			if !preview.OK || len(preview.Samples) != len(wants) {
				t.Fatalf("read-only protocol preview: %+v", preview)
			}
			for _, sample := range preview.Samples {
				want, declared := wants[sample.Signal]
				if !declared || sample.Value != want || sample.Quality != "good" || sample.Source.Revision != binding.Revision() || sample.Source.Generation == "" {
					t.Fatalf("native package lost typed value or original binding identity: %+v", sample)
				}
				delete(wants, sample.Signal)
			}
			if len(wants) != 0 {
				t.Fatal("connection check omitted declared points")
			}

			// The same read belongs to an authorized business record, without
			// granting the member access to connection settings or making history.
			publishObject(t, tn, "builder", "EQUIPMENT", map[string]any{
				"name": "equipment", "title": "Equipment",
				"fields": []build.Field{{Name: "device", Title: "Device", Type: "text"}},
				"access": []build.Access{{Role: "user", Read: "all", Create: true}},
			}, at)
			var point platform.DriverPoint
			for _, candidate := range configuration.Points {
				if candidate.Type == "decimal" && candidate.Unit != "" {
					point = candidate
					break
				}
			}
			decide(t, tn, "operator", build.ID, "build.equipment.create", "build.equipment", "DEVICE", map[string]any{"device": point.Device}, at)
			decide(t, tn, "builder", build.ID, "build.observation.create", build.ObservationType, "CURRENT", map[string]any{
				"name": "current", "title": "Current equipment reading", "connection": "PLC", "object": "build.equipment", "deviceField": "device",
				"signal": point.ID, "unit": point.Unit, "path": "/observation", "windowSeconds": 60,
				"expectedRetentionSeconds": 3600, "expectedGranularitySeconds": 1, "roles": []string{"user", "builder"},
			}, at)
			decide(t, tn, "builder", build.ID, build.SchemaObservation, build.ObservationType, "CURRENT", map[string]any{}, at)
			member := memberOf(t, tn, "operator")
			observations, refusal := tn.Observations(member)
			if refusal != nil || len(observations) != 1 || !observations[0].SupportsLatest {
				t.Fatalf("current reading was not offered to its business member: %+v %v", observations, refusal)
			}
			ref := observations[0].Ref
			before := snapshot(tn)
			result, refusal := tn.ReadObservation(context.Background(), member, ref, ObservationRead{Record: "DEVICE", Mode: "latest"}, at)
			if refusal != nil || result.Latest == nil {
				t.Fatalf("business member current reading: %+v %v", result, refusal)
			}
			if result.Latest.Signal != point.ID || result.Latest.Device != point.Device || result.Latest.Unit != point.Unit || result.Latest.Quality != "good" || result.Latest.Source.Revision != binding.Revision() {
				t.Fatalf("current reading lost original point or binding: %+v", result.Latest)
			}
			if result.Window != (platform.ObservationWindow{}) || len(result.Points) != 0 || snapshot(tn) != before {
				t.Fatal("current reading synthesized history or wrote tenant state")
			}
			if _, refusal := tn.ReadObservation(context.Background(), memberOf(t, tn, "outsider"), ref, ObservationRead{Record: "DEVICE", Mode: "latest"}, at); refusal == nil {
				t.Fatal("current reading bypassed the original member permissions")
			}
			if _, refusal := tn.ReadObservation(context.Background(), member, ref, ObservationRead{Record: "DEVICE", Mode: "summary", Since: at.Add(-time.Minute), Until: at}, at); refusal == nil {
				t.Fatal("read-only device data replaced durable historical coverage")
			}
		})
	}
}

func s7ReadConfiguration(t *testing.T) (platform.DriverConfiguration, map[string]any) {
	t.Helper()
	data := make([]byte, 12)
	data[0] = 8
	negative := int16(-7)
	binary.BigEndian.PutUint16(data[2:], uint16(negative))
	binary.BigEndian.PutUint32(data[4:], 2048)
	binary.BigEndian.PutUint32(data[8:], math.Float32bits(23.5))
	plc, err := s7.ListenTest("127.0.0.1:0", map[uint16][]byte{1: data}, 240)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plc.Close() })
	configuration := platform.DriverConfiguration{Connection: map[string]any{"address": plc.Address()}, Devices: []platform.DriverDevice{{ID: "cpu", Title: "Local S7 test service"}}, Points: []platform.DriverPoint{
		{ID: "enabled", Device: "cpu", Address: "DB1.DBX0.3", Type: "boolean", DataType: "BOOL", IntervalMS: 1000},
		{ID: "quantity", Device: "cpu", Address: "DB1.DBW2", Type: "integer", DataType: "INT", IntervalMS: 1000},
		{ID: "total", Device: "cpu", Address: "DB1.DBD4", Type: "integer", DataType: "DINT", IntervalMS: 1000},
		{ID: "reading", Device: "cpu", Address: "DB1.DBD8", Type: "decimal", DataType: "REAL", Unit: "unit", IntervalMS: 1000},
	}}
	return configuration, map[string]any{"enabled": true, "quantity": "-7", "total": "2048", "reading": "23.5"}
}

func modbusReadConfiguration(t *testing.T) (platform.DriverConfiguration, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile("../../deploy/examples/hvac/modbus.json")
	if err != nil {
		t.Fatal(err)
	}
	var configuration platform.DriverConfiguration
	if err = json.Unmarshal(raw, &configuration); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../deploy/examples/hvac/modbus-memory.json")
	if err != nil {
		t.Fatal(err)
	}
	var units map[byte]modbus.Memory
	if err = json.Unmarshal(raw, &units); err != nil {
		t.Fatal(err)
	}
	device, err := modbus.ListenTest("127.0.0.1:0", units)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = device.Close() })
	configuration.Connection["address"] = device.Address()
	return configuration, map[string]any{"mau-running": true, "mau-temperature-setpoint": "22", "mau-supply-temperature": "23.5", "mau-supply-humidity": "56", "mau-supply-pressure": "123.4", "ddc-temperature": "21.5", "ddc-valve-position": "35", "ddc-sensor-alarm": "1", "plant-cooling-supply-temperature": "7.5", "plant-cooling-return-temperature": "12.5"}
}
