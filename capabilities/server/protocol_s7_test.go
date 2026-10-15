package platformserver

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/drivers/s7"
	"platformserver/gateway"
	"platformserver/platform"
)

func TestProtocolConnectionLoadsS7Package(t *testing.T) {
	directory := os.Getenv("PLATFORM_PROTOCOL_PACKAGES")
	if directory == "" {
		t.Skip("set PLATFORM_PROTOCOL_PACKAGES to the real output of make packages")
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, found := index.find("protocol.s7-1200", "0.1.2")
	if !found || descriptor.Runtime == nil {
		t.Fatal("make packages must provide protocol.s7-1200@0.1.2")
	}
	artifact, err := descriptor.Runtime.Target()
	if err != nil {
		t.Fatal(err)
	}
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
	defer plc.Close()

	tn := composeTenant(t, "protocol-s7", []Seat{seatOf("builder", "build:builder", "platform:admin")}, build.New("protocol-s7"))
	console := testConsoleOf(tn)
	console.index = index
	at := time.Now().UTC()
	decide(t, tn, "builder", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
	service, err := gateway.New(gateway.Config{Directory: directory, StateDir: t.TempDir(), Token: "s7-integration-token", Samples: io.Discard,
		Resolve: func(binding platform.DriverBinding) (platform.DriverRuntime, error) {
			return console.ResolveDriver(binding.Package, binding.Version, binding.Digest, binding.Profile)
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(service)
	defer server.Close()
	tn.Secrets = func(name string) ([]byte, bool) { return []byte("Bearer s7-integration-token"), name == "gateway" }
	binding := platform.DriverBinding{Package: descriptor.ID, Version: descriptor.Version, Digest: artifact.Artifact, Profile: s7.Profile,
		Configuration: platform.DriverConfiguration{Connection: map[string]any{"address": plc.Address()}, Devices: []platform.DriverDevice{{ID: "cpu", Title: "Local S7 test service"}}, Points: []platform.DriverPoint{
			{ID: "enabled", Device: "cpu", Address: "DB1.DBX0.3", Type: "boolean", DataType: "BOOL", IntervalMS: 1000},
			{ID: "quantity", Device: "cpu", Address: "DB1.DBW2", Type: "integer", DataType: "INT", IntervalMS: 1000},
			{ID: "total", Device: "cpu", Address: "DB1.DBD4", Type: "integer", DataType: "DINT", IntervalMS: 1000},
			{ID: "reading", Device: "cpu", Address: "DB1.DBD8", Type: "decimal", DataType: "REAL", Unit: "unit", IntervalMS: 1000},
		}}}
	decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, "PLC", map[string]any{"name": "plc", "title": "Local S7 test service", "kind": "http", "address": server.URL, "secret": "gateway", "allowPrivate": true, "protocol": binding}, at)
	decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, "PLC", map[string]any{}, at)
	tn.CheckConnections(at)
	connection, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "PLC")
	if connection.Last == nil || !connection.Last.OK || connection.Last.BindingRevision != binding.Revision() || len(connection.Last.Samples) != 0 {
		t.Fatalf("second package did not load through the original connection check: %+v", connection.Last)
	}
	connection.ID = "preview-" + binding.Revision()[:24]
	preview := tn.checkProtocolConnection(context.Background(), connection, at)
	if !preview.OK || len(preview.Samples) != 4 {
		t.Fatalf("read-only S7 preview: %+v", preview)
	}
	wants := map[string]any{"enabled": true, "quantity": "-7", "total": "2048", "reading": "23.5"}
	for _, sample := range preview.Samples {
		want, declared := wants[sample.Signal]
		if !declared || sample.Value != want || sample.Quality != "good" || sample.Source.Revision != binding.Revision() || sample.Source.Generation == "" {
			t.Fatalf("native S7 package lost typed value or original binding identity: %+v", sample)
		}
		delete(wants, sample.Signal)
	}
	if len(wants) != 0 {
		t.Fatal("connection check omitted declared S7 points")
	}
}
