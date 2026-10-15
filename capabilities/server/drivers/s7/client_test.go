package s7

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"platformserver/platform"
)

func TestClientReadsStandardDBOverTCP(t *testing.T) {
	data := make([]byte, 16)
	data[0], data[12] = 8, 127
	negative16, negative32 := int16(-123), int32(-123456)
	binary.BigEndian.PutUint16(data[2:], uint16(negative16))
	binary.BigEndian.PutUint32(data[4:], uint32(negative32))
	binary.BigEndian.PutUint32(data[8:], math.Float32bits(58.4))
	server, err := ListenTest("127.0.0.1:0", map[uint16][]byte{1: data}, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := Dial(context.Background(), Config{Address: server.Address(), Slot: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	points := []Point{
		{Name: "enabled", Address: "DB1.DBX0.3", Type: "BOOL"},
		{Name: "integer", Address: "DB1.DBW2", Type: "INT"},
		{Name: "long", Address: "DB1.DBD4", Type: "DINT"},
		{Name: "real", Address: "DB1.DBD8", Type: "REAL"},
		{Name: "byte", Address: "DB1.DBB12", Type: "BYTE"},
		{Name: "missing", Address: "DB2.DBW0", Type: "INT"},
		{Name: "outside", Address: "DB1.DBD100", Type: "REAL"},
		{Name: "invalid", Address: "DB1.DBW0", Type: "REAL"},
	}
	values, err := client.Read(context.Background(), points)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []any{true, "-123", "-123456", "58.4", "127"} {
		if values[i].Value != want || values[i].Error != "" {
			t.Fatalf("point %s: want %v, got %+v", points[i].Name, want, values[i])
		}
	}
	for _, value := range values[5:] {
		if value.Error == "" || value.Value != nil {
			t.Fatalf("invalid point became a value: %+v", value)
		}
	}
	if client.pdu != 64 {
		t.Fatal("client ignored the peer's smaller PDU")
	}
}

func TestS7HandlerKeepsReadOnlyProfile(t *testing.T) {
	server, err := ListenTest("127.0.0.1:0", map[uint16][]byte{1: {0, 0, 0, 42}}, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	h := New()
	in := platform.DriverOpen{Session: "one", Connection: "plc", Revision: "r1", Generation: "g1", Profile: Profile,
		Configuration: platform.DriverConfiguration{Connection: map[string]any{"address": server.Address()}, Devices: []platform.DriverDevice{{ID: "cpu", Title: "Test CPU"}},
			Points: []platform.DriverPoint{{ID: "value", Device: "cpu", Address: "DB1.DBW2", Type: "integer", DataType: "INT", IntervalMS: 1000}}}}
	if _, err := h.Handle(context.Background(), "open", platform.Raw(in)); err != nil {
		t.Fatal(err)
	}
	defer h.Handle(context.Background(), "close", platform.Raw(platform.DriverSession{Session: in.Session}))
	answer, err := h.Handle(context.Background(), "read", platform.Raw(platform.DriverRead{Session: in.Session}))
	if err != nil {
		t.Fatal(err)
	}
	read := answer.(platform.DriverReadResult)
	if len(read.Samples) != 1 || read.Samples[0].Value != "42" || read.Samples[0].Source.Generation != "g1" {
		t.Fatalf("read lost value or configuration provenance: %+v", read)
	}
	if _, err := h.Handle(context.Background(), "execute", platform.Raw(platform.DriverExecute{Session: in.Session})); err == nil {
		t.Fatal("read-only profile accepted a write")
	}
	if _, err := h.Handle(context.Background(), "read", platform.Raw(platform.DriverRead{Session: in.Session, Mode: "summary"})); err == nil {
		t.Fatal("live S7 read claimed to supply an external history window")
	}
}
