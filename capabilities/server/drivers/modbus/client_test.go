package modbus

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"platformserver/platform"
)

func TestModbusReadsAreasTypesAndBoundedBatches(t *testing.T) {
	memory := Memory{
		Coils: map[uint16]bool{7: true, 8: false}, Discrete: map[uint16]bool{0: true},
		Holding: map[uint16]uint16{0: 0xff9c, 1: 0xffff, 2: 0xffff, 3: 0xfb2e, 4: 0xffff, 5: 0xffff, 6: 0xc148, 7: 0, 8: 8},
		Input:   map[uint16]uint16{0: 0x7fc0, 1: 0, 2: 42},
	}
	for i := range 126 {
		memory.Holding[uint16(2000+i)] = uint16(i)
	}
	server, err := ListenTest("127.0.0.1:0", map[byte]Memory{7: memory})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := Dial(context.Background(), Config{Address: server.Address(), UnitID: 7, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	point := func(address, kind, valueType string) platform.DriverPoint {
		return platform.DriverPoint{Address: address, DataType: kind, Type: valueType}
	}
	points := []platform.DriverPoint{
		point("holding:0", "INT16", "integer"), point("holding:1", "UINT16", "integer"),
		point("holding:2", "INT32", "integer"), point("holding:4", "UINT32", "integer"),
		point("holding:6", "FLOAT32", "decimal"), point("holding:8.3", "BOOL", "boolean"),
		point("holding:8.15", "BOOL", "boolean"), point("coil:7", "BOOL", "boolean"),
		point("coil:8", "BOOL", "boolean"), point("discrete:0", "BOOL", "boolean"),
		point("input:2", "UINT16", "integer"),
		point("holding:1000", "INT16", "integer"), point("input:0", "FLOAT32", "decimal"),
		point("holding:65535", "UINT32", "integer"),
	}
	for i := range 126 {
		points = append(points, point("holding:"+strconv.Itoa(2000+i), "UINT16", "integer"))
	}
	values, err := client.Read(context.Background(), points)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []any{"-100", "65535", "-1234", "4294967295", "-12.5", true, false, true, false, true, "42"} {
		if values[i].Value != want || values[i].Error != "" {
			t.Fatalf("point %s: want %v, got %+v", points[i].Address, want, values[i])
		}
	}
	for _, value := range values[11:14] {
		if value.Error == "" || value.Value != nil {
			t.Fatalf("missing, non-finite or invalid point became a value: %+v", value)
		}
	}
	for i, value := range values[14:] {
		if value.Error != "" || value.Value != strconv.Itoa(i) {
			t.Fatalf("125-register limit or neighbouring exception lost valid point %d: %+v", i, value)
		}
	}
}

func TestModbusValidatesWireReply(t *testing.T) {
	for name, reply := range map[string]string{
		"fragmented":  "000100000005070302002a",
		"transaction": "000200000005070302002a",
		"protocol":    "000100010005070302002a",
		"unit":        "000100000005080302002a",
		"length":      "00010000ffff070302002a",
		"function":    "000100000005070402002a",
		"byte-count":  "000100000005070301002a",
	} {
		t.Run(name, func(t *testing.T) {
			peer, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := peer.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				request := make([]byte, 12)
				if _, err = io.ReadFull(conn, request); err == nil && hex.EncodeToString(request) != "000100000006070300000001" {
					err = fmt.Errorf("unexpected read request: %x", request)
				}
				if err == nil {
					raw, _ := hex.DecodeString(reply)
					for _, b := range raw {
						if _, err = conn.Write([]byte{b}); err != nil {
							break
						}
					}
				}
				done <- err
			}()
			client, err := Dial(context.Background(), Config{Address: peer.Addr().String(), UnitID: 7, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			values, readErr := client.Read(context.Background(), []platform.DriverPoint{{Address: "holding:0", DataType: "UINT16", Type: "integer"}})
			if name == "fragmented" {
				if readErr != nil || values[0].Value != "42" {
					t.Fatalf("fragmented reply: %+v %v", values, readErr)
				}
			} else if readErr == nil {
				t.Fatalf("accepted malformed reply: %+v", values)
			}
			// Invalid headers may be rejected before the peer finishes writing.
			if peerErr := <-done; peerErr != nil && name == "fragmented" {
				t.Fatal(peerErr)
			}
		})
	}
}

func TestModbusConfiguredPointsKeepConversionQualityAndReadOnlyScope(t *testing.T) {
	configuration, err := os.ReadFile("../../../../deploy/examples/hvac/modbus.json")
	if err != nil {
		t.Fatal(err)
	}
	var in platform.DriverOpen
	if err = json.Unmarshal(configuration, &in.Configuration); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../../../deploy/examples/hvac/modbus-memory.json")
	if err != nil {
		t.Fatal(err)
	}
	var memory map[byte]Memory
	if err = json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}
	server, err := ListenTest("127.0.0.1:0", memory)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	in.Configuration.Connection["address"] = server.Address()
	in.Session, in.Connection, in.Revision, in.Generation, in.Profile = "one", "hvac", "r1", "g1", Profile
	in.Configuration.Points = append(in.Configuration.Points, platform.DriverPoint{ID: "missing", Device: "ddc-01", Address: "holding:6000", Type: "integer", DataType: "INT16", IntervalMS: 1000})
	h := New()
	if _, err = h.Handle(context.Background(), "open", platform.Raw(in)); err != nil {
		t.Fatal(err)
	}
	defer h.Handle(context.Background(), "close", platform.Raw(platform.DriverSession{Session: in.Session}))
	answer, err := h.Handle(context.Background(), "read", platform.Raw(platform.DriverRead{Session: in.Session}))
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]any{"mau-running": true, "mau-temperature-setpoint": "22", "mau-supply-temperature": "23.5", "mau-supply-humidity": "56", "mau-supply-pressure": "123.4", "ddc-temperature": "21.5", "ddc-valve-position": "35", "ddc-sensor-alarm": "1", "plant-cooling-supply-temperature": "7.5", "plant-cooling-return-temperature": "12.5"}
	for _, sample := range answer.(platform.DriverReadResult).Samples {
		if sample.Signal == "missing" {
			if sample.Quality != "bad" || sample.Value != nil || sample.Reason == "" {
				t.Fatalf("missing register became healthy: %+v", sample)
			}
			continue
		}
		want, ok := wants[sample.Signal]
		if !ok || sample.Value != want || sample.Quality != "good" || sample.Source.Revision != "r1" || sample.Source.Generation != "g1" || sample.Source.Sequence == "" {
			t.Fatalf("configured point lost its conversion or origin: %+v", sample)
		}
		delete(wants, sample.Signal)
	}
	if len(wants) != 0 {
		t.Fatal("declared points were omitted", wants)
	}
	in.Configuration.Commands = []platform.DriverCommand{{ID: "set", Point: "mau-temperature-setpoint"}}
	if _, err = h.Handle(context.Background(), "validate", platform.Raw(in)); err == nil {
		t.Fatal("read-only profile allowed a command declaration")
	}
	if _, err = h.Handle(context.Background(), "execute", platform.Raw(platform.DriverExecute{Session: in.Session})); err == nil {
		t.Fatal("read-only profile executed a command")
	}
	if _, err = h.Handle(context.Background(), "read", platform.Raw(platform.DriverRead{Session: in.Session, Mode: "summary"})); err == nil {
		t.Fatal("live values were presented as a complete historical window")
	}
	_ = server.Close()
	answer, err = h.Handle(context.Background(), "read", platform.Raw(platform.DriverRead{Session: in.Session}))
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range answer.(platform.DriverReadResult).Samples {
		if sample.Quality != "bad" || sample.Value != nil {
			t.Fatalf("disconnected device became a healthy zero: %+v", sample)
		}
	}
}
