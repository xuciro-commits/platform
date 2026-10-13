package platformserver

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// The fixture exercises the production pgx source reader, including connection
// failures and latest/read queries. It introduces no alternate host read path.
func streamRetryDatabase(t *testing.T, fail map[int]bool, onConnect func(int)) (string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var connections, latestQueries atomic.Int32
	var workers sync.WaitGroup
	t.Cleanup(func() { listener.Close(); workers.Wait() })
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				backend := pgproto3.NewBackend(conn, conn)
				if _, err := backend.ReceiveStartupMessage(); err != nil {
					return
				}
				attempt := int(connections.Add(1))
				if onConnect != nil {
					onConnect(attempt)
				}
				if fail[attempt] {
					backend.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "40001", Message: "transient source failure"})
					backend.Flush()
					return
				}
				backend.Send(&pgproto3.AuthenticationOk{})
				backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
				backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
				backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
				if err := backend.Flush(); err != nil {
					return
				}
				for {
					message, err := backend.Receive()
					if err != nil {
						return
					}
					query, ok := message.(*pgproto3.Query)
					if !ok {
						return
					}
					names := []string{"eventAt", "eventId", "plantId", "reading", "seq"}
					values := [][]byte{[]byte("2026-10-11T12:00:00Z"), []byte("event-8"), []byte("plant"), []byte("12"), []byte("8")}
					if strings.Contains(query.String, " desc limit 1") {
						latestQueries.Add(1)
						names, values = []string{"seq"}, [][]byte{[]byte("7")}
					}
					fields := make([]pgproto3.FieldDescription, len(names))
					for index, name := range names {
						fields[index] = pgproto3.FieldDescription{Name: []byte(name), DataTypeOID: 25, DataTypeSize: -1}
					}
					backend.Send(&pgproto3.RowDescription{Fields: fields})
					backend.Send(&pgproto3.DataRow{Values: values})
					backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
					backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
					if err := backend.Flush(); err != nil {
						return
					}
				}
			}()
		}
	}()
	address := "postgres://source@" + listener.Addr().String() + "/source?sslmode=disable&default_query_exec_mode=simple_protocol"
	return address, &connections, &latestQueries
}

type retryIntakeSource struct {
	*frameSource
	latest bool
}

func (s *retryIntakeSource) Manifest() platform.Manifest {
	m := s.frameSource.Manifest()
	intake := &platform.StreamIntake{SourceRecord: "stream", Key: "eventId", Partition: []string{"plantId"}, EventTime: "eventAt", Value: "reading", Retry: &platform.StreamPreparationRetry{Attempts: 3, BackoffMS: 1}}
	if s.latest {
		intake.Offset = "latest"
	}
	m.Flows[0].Continuous.Intake = intake
	return m
}

func TestContinuousSourcePreparationRetriesWithoutCursorLoss(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		latest   bool
		fail     map[int]bool
		mutation string
		calls    int32
		queries  int32
		accepted bool
	}{
		{"transient-read", false, map[int]bool{1: true}, "", 2, 0, true},
		{"exhausted-read", false, map[int]bool{1: true, 2: true, 3: true}, "", 3, 0, false},
		{"exhausted-after-checkpoint", false, map[int]bool{2: true, 3: true, 4: true}, "", 4, 0, false},
		{"transient-latest", true, map[int]bool{1: true}, "", 3, 1, true},
		{"read-after-latest", true, map[int]bool{2: true}, "", 3, 1, true},
		{"exhausted-latest", true, map[int]bool{1: true, 2: true, 3: true}, "", 3, 0, false},
		{"revoked-between-attempts", false, map[int]bool{1: true}, "revoke", 1, 0, false},
		{"revoked-after-capture", false, nil, "revoke", 1, 0, false},
		{"source-changed-between-attempts", false, map[int]bool{1: true}, "source", 1, 0, false},
		{"source-changed-after-capture", false, nil, "source", 1, 0, false},
		{"connection-changed-between-attempts", false, map[int]bool{1: true}, "connection", 1, 0, false},
		{"connection-changed-after-capture", false, nil, "connection", 1, 0, false},
		{"instance-stopped-between-attempts", false, map[int]bool{1: true}, "stop", 1, 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			const tenant, id = "source-retry", "frames.window:source"
			at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
			var tn *Tenant
			address, connections, queries := streamRetryDatabase(t, scenario.fail, func(attempt int) {
				if attempt != 1 {
					return
				}
				switch scenario.mutation {
				case "revoke":
					decide(t, tn, "admin", PlatformApp, "platform.member.revoke", "platform.member", "member", map[string]any{"app": build.ID}, at)
				case "source":
					x, _ := platform.Get[build.Source](tn.automation(build.ID, false), "stream")
					x.Entity = "different_source"
					if err := tn.automation(build.ID, true).PutAt(at, "test.change-source", x); err != nil {
						t.Error(err)
					}
				case "connection":
					x, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "db")
					x.Address += "&connect_timeout=2"
					if err := tn.automation(build.ID, true).PutAt(at, "test.change-connection", x); err != nil {
						t.Error(err)
					}
				case "stop":
					decide(t, tn, "member", flow.ID, flow.SchemaFlowStop, flow.InstanceType, id, map[string]any{}, at)
				}
			})
			source := &retryIntakeSource{frameSource: newFrameSource(tenant, 2<<20), latest: scenario.latest}
			tn = composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin", "build:builder"), seatOf("admin", "platform:admin")}, build.New(tenant), work.New(tenant), flow.New(tenant), source)
			for _, record := range []any{
				build.Connection{Record: platform.Record{ID: "db"}, Name: "db", Kind: "postgres", Address: address, State: "ready"},
				build.Source{Record: platform.Record{ID: "stream"}, Name: "frames.source", Connection: "db", Profile: "table", Entity: "telemetry", Since: "seq", Stream: true, State: "published"},
			} {
				if err := tn.automation(build.ID, true).PutAt(at, "test.source", record); err != nil {
					t.Fatal(err)
				}
			}
			decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
			member := memberOf(t, tn, "member")
			if scenario.name == "exhausted-after-checkpoint" {
				if first, err := tn.ConsumeFlowSource(member, id, at); err != nil || first.Consumed != 1 {
					t.Fatal("fixture did not accept its initial source checkpoint", err)
				}
			}
			before, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), id)
			outcome, refusal := tn.ConsumeFlowSource(member, id, at)
			after, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), id)
			if scenario.accepted {
				if refusal != nil {
					t.Fatal(refusal.Message)
				}
				if refusal != nil || outcome.Consumed != 1 || after.Batch == nil || after.Batch.Source.Cursor != "8" {
					t.Fatalf("retry did not accept the original source row: %+v, %+v", outcome, refusal)
				}
				if _, err := tn.ReadFlowFrame(member, id, at); err != nil {
					t.Fatal(err)
				}
			} else {
				if refusal == nil || string(platform.Raw(before.Batch)) != string(platform.Raw(after.Batch)) || scenario.mutation != "stop" && string(platform.Raw(before)) != string(platform.Raw(after)) {
					t.Fatal("a refused source read changed its original frame or cursor", refusal)
				}
			}
			if connections.Load() != scenario.calls || queries.Load() != scenario.queries {
				t.Fatalf("fixed retry/latest bounds differ: connections=%d latest=%d", connections.Load(), queries.Load())
			}
			if stream, _ := platform.Get[build.Source](tn.automation(build.ID, false), "stream"); stream.Cursor != "" {
				t.Fatal("preparation created a second source cursor")
			}
		})
	}
}

func TestContinuousSourceWithoutControlledIntakeRefuses(t *testing.T) {
	const tenant, id = "no-controlled-intake", "frames.window:source"
	at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), newFrameSource(tenant, 2<<20))
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	if _, refusal := tn.ConsumeFlowSource(memberOf(t, tn, "member"), id, at); refusal == nil {
		t.Fatal("a native batch-only instance accepted an uncontrolled source read")
	}
}
