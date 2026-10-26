package platformserver

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
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
func TestContinuousSourceWithoutControlledIntakeRefuses(t *testing.T) {
	const tenant, id = "no-controlled-intake", "frames.window:source"
	at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), newFrameSource(tenant, 2<<20))
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	if _, refusal := tn.ConsumeFlowSource(memberOf(t, tn, "member"), id, at); refusal == nil {
		t.Fatal("a native batch-only instance accepted an uncontrolled source read")
	}
}
