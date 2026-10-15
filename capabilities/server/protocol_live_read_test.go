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
			descriptor, found := index.find(protocol.id, "0.1.2")
			if !found || descriptor.Runtime == nil {
				t.Fatal("make packages must provide", protocol.id+"@0.1.2")
			}
			artifact, err := descriptor.Runtime.Target()
			if err != nil {
				t.Fatal(err)
			}
			configuration, wants := protocol.configure(t)

			tn := composeTenant(t, "protocol-read", []Seat{seatOf("builder", "build:builder", "platform:admin")}, build.New("protocol-read"))
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
