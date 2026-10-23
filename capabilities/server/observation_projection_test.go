package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func intervalProjectionTenant(t *testing.T, kind string) (*Tenant, func() *Tenant, platform.AssetRef, time.Time) {
	t.Helper()
	const id = "interval-projection"
	compose := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:user", "work:user")}, build.New(id), work.New(id), flow.New(id))
	}
	tn := compose()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	access := []construction.Access{{Role: "user", Read: "all", Create: true, Edit: true}}
	publishObject(t, tn, "builder", "DEVICE", map[string]any{"name": "device", "title": "Device", "fields": []construction.Field{{Name: "device", Title: "Device", Type: "text"}}, "states": []construction.State{{Name: "ready", Title: "Ready"}}, "access": access}, at)
	mapping := platform.TxnIntervalFields{Subject: "device", Source: "source", SourceVersion: "sourceversion", SourceRevision: "sourcerevision", ProcessingVersion: "processingversion", Since: "since", Until: "until", OpenStart: "openstart", OpenEnd: "openend", Retired: "retired", Review: "review"}
	fields := []construction.Field{{Name: "device", Title: "Device", Type: "reference", Ref: "build.device", Inverse: "events"}, {Name: "source", Title: "Source", Type: "text"}, {Name: "sourceversion", Title: "Source version", Type: "text"}, {Name: "sourcerevision", Title: "Source revision", Type: "text"}, {Name: "processingversion", Title: "Processing version", Type: "text"}, {Name: "since", Title: "Since", Type: "datetime"}, {Name: "until", Title: "Until", Type: "datetime"}, {Name: "openstart", Title: "Open start", Type: "boolean"}, {Name: "openend", Title: "Open end", Type: "boolean"}, {Name: "retired", Title: "Retired", Type: "boolean"}, {Name: "review", Title: "Review", Type: "boolean"}, {Name: "reason", Title: "Human reason", Type: "text"}, {Name: "seensince", Title: "Flow start", Type: "datetime"}}
	guard := platform.ExprReference("boolean", "observation.ready")
	immutable := platform.Expr{Op: "not", Kind: "boolean", Args: []platform.Expr{guard}}
	publishObject(t, tn, "builder", "EVENT", map[string]any{"name": "event", "title": "Event", "fields": fields, "access": access, "states": []construction.State{{Name: "recorded", Title: "Recorded"}, {Name: "review", Title: "Review"}, {Name: "reasoned", Title: "Reasoned"}},
		"constraints":       []platform.Constraint{{Name: "provenance", Kind: "immutable", Fields: []string{"device", "source", "sourceversion", "sourcerevision", "processingversion", "since", "until", "openstart", "openend", "retired"}, Expr: &immutable, Message: "Observation fields require their original source."}},
		"createTransaction": &platform.BusinessTxn{Subject: "build.event", Guard: &guard, Message: "An observation is required.", Steps: []platform.TxnStep{{Name: "created", Kind: "update", Target: platform.TxnRef{Type: "build.event", ID: "$subject"}}}},
		"actions": []construction.Action{
			{Name: "remind", Title: "Remind", From: []string{"recorded"}, Roles: []string{"user"}, Inputs: []construction.Input{{Name: "start", Title: "Start", Type: "datetime", Required: true}}, Transaction: &platform.BusinessTxn{Subject: "build.event", Steps: []platform.TxnStep{{Name: "state", Kind: "update", Target: platform.TxnRef{Type: "build.event", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", From: "=review"}, {Field: "seensince", From: "start"}}}, {Name: "reason", Kind: "work", Work: &platform.TxnWork{Title: "Review observed interval", Role: "user"}}}}},
			{Name: "reason", Title: "Record reason", From: []string{"review", "reasoned"}, To: "reasoned", Roles: []string{"user"}, Inputs: []construction.Input{{Name: "reason", Title: "Reason", Type: "text", Required: true}}, Sets: []construction.Set{{Field: "reason", From: "reason"}}},
		}}, at)
	decide(t, tn, "builder", "build", "build.linktype.create", construction.LinkTypeType, "PREDECESSORS", map[string]any{"name": "eventhistory", "title": "Event history", "description": "Original interval predecessors", "storage": "edge", "parent": "build.event", "child": "build.event", "forward": "successors", "reverse": "predecessors", "cardinality": "many-to-many", "roles": []string{"user", "builder"}}, at)
	decide(t, tn, "builder", "build", "build.linktype.publish", construction.LinkTypeType, "PREDECESSORS", nil, at)
	ref := platform.AssetRef{App: "build", Kind: platform.AssetObservation, Name: "statewindow"}
	projection := &platform.BusinessTxn{Subject: "build.device", Steps: []platform.TxnStep{{Name: "events", Kind: "intervals", Intervals: &platform.TxnIntervals{Source: ref.Name, Set: "events", Predecessors: platform.AssetBinding{Ref: platform.AssetRef{App: "build", Kind: platform.AssetLinkType, Name: "eventhistory"}, SourceVersion: "1.link-1"}, Fields: mapping}}}}
	decide(t, tn, "builder", "build", "build.object.edit", construction.ObjectType, "DEVICE", map[string]any{"sets": []platform.RecordSet{{Name: "events", From: "events", Direction: "inverse", Object: "build.event", Where: []platform.Expr{{Op: "not", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("boolean", "retired")}}}, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("datetime", "since")}}, Limit: platform.RecordSetMaxRows}}, "actions": []construction.Action{{Name: "observe", Title: "Record intervals", From: []string{"ready"}, Roles: []string{"user"}, Transaction: projection}}}, at)
	decide(t, tn, "builder", "build", "build.object.publish", construction.ObjectType, "DEVICE", nil, at)
	decide(t, tn, "operator", "build", "build.device.create", "build.device", "D1", map[string]any{"device": "source-1"}, at)
	decide(t, tn, "builder", "build", "build.process.create", construction.ProcessType, "FLOW", map[string]any{"name": "intervalreview", "title": "Interval review", "object": "build.event", "when": "recorded", "steps": []construction.ProcessStep{{Name: "remind", Kind: "action", Act: "remind", Inputs: map[string]platform.Binding{"start": {Source: "subject", Path: []string{"since"}}}, Next: "done"}, {Name: "done", Kind: "end"}}}, at)
	decide(t, tn, "builder", "build", "build.process.publish", construction.ProcessType, "FLOW", nil, at)
	decide(t, tn, "builder", "build", "build.connection.create", construction.ConnectionType, "CONNECTION", map[string]any{"name": "statestore", "title": "State store", "kind": "http", "address": "http://observation.test", "allowPrivate": true}, at)
	decide(t, tn, "builder", "build", "build.connection.check", construction.ConnectionType, "CONNECTION", nil, at)
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	}
	tn.CheckConnections(at)
	condition := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("choice", "sample.value"), platform.ExprLiteralValue("choice", "down")}}
	if kind == "decimal" {
		condition = platform.Expr{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("decimal", "sample.value"), platform.ExprLiteralValue("decimal", "35")}}
	}
	decide(t, tn, "builder", "build", "build.observation.create", construction.ObservationType, "OBSERVATION", map[string]any{"name": ref.Name, "title": "State intervals", "connection": "CONNECTION", "object": "build.device", "deviceField": "device", "signal": "state", "unit": "state", "path": "/observations", "windowSeconds": 60, "expectedRetentionSeconds": 86400, "expectedGranularitySeconds": 10, "roles": []string{"user", "builder"}, "intervals": platform.ObservationIntervals{ValueKind: kind, When: condition, MaxGapSeconds: 10, LatePolicy: "correct", LatenessSeconds: 30, MaxIntervals: 16}, "decision": &platform.ObservationDecision{Mode: "intervals", When: platform.ExprLiteralValue("boolean", true), Action: platform.AssetRef{App: "build", Kind: platform.AssetAction, Name: "build.device.observe"}, Inputs: map[string]platform.Expr{}}}, at)
	decide(t, tn, "builder", "build", "build.observation.publish", construction.ObservationType, "OBSERVATION", nil, at)
	return tn, compose, ref, at
}

func TestObservationIntervalProjectionFlowIdentityAndPureRecovery(t *testing.T) {
	for _, kind := range []string{"choice", "decimal"} {
		t.Run(kind, func(t *testing.T) {
			tn, compose, ref, at := intervalProjectionTenant(t, kind)
			checkpoint, _, err := tn.Snapshot(func() int64 { return 0 })
			if err != nil {
				t.Fatal(err)
			}
			values := []bool{false, true, true, true, true, false}
			calls := 0
			incomplete := false
			tn.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
				calls++
				var q struct {
					Since, Until time.Time
					Mode         string
				}
				if json.NewDecoder(request.Body).Decode(&q) != nil || q.Mode != "intervals" {
					t.Fatal("interval decision requested another read")
				}
				input := platform.ObservationIntervalInput{Since: q.Since, Until: q.Until, Revision: fmt.Sprint("revision-", calls), Coverage: &platform.DriverCoverage{AvailableSince: q.Since, CompleteUntil: q.Until, Good: 6, Truncated: incomplete}}
				for i, matching := range values {
					value := "run"
					if matching {
						value = "down"
					}
					if kind == "decimal" {
						value = "20"
						if matching {
							value = "40"
						}
					}
					sampleAt := q.Since.Add(time.Duration(i*10) * time.Second)
					input.Points = append(input.Points, platform.ObservationStatePoint{At: sampleAt, ReceivedAt: sampleAt, Quality: "good", Value: value})
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(platform.Raw(map[string]any{"intervals": input}))), Header: make(http.Header)}, nil
			}
			var entries []Entry
			fail := false
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
				if fail {
					return nil, errors.New("injected interval append failure")
				}
				entries = append(entries, e)
				return e.Body, nil
			}
			member := memberOf(t, tn, "operator")
			evaluate := func(key string) ObservationEvaluation {
				t.Helper()
				answer, problem := tn.EvaluateObservation(context.Background(), member, ref, ObservationEvaluate{Record: "D1", Until: at, Key: key}, at)
				if problem != nil || answer.Receipt == nil {
					t.Fatalf("interval decision %s: %+v %v", key, answer, problem)
				}
				tn.Work(at.Add(time.Second))
				return answer
			}
			rows := func() []map[string]any {
				t.Helper()
				found, problem := tn.Records(member, "build.event", platform.Query{Limit: 100}, at)
				if problem != nil {
					t.Fatal(problem)
				}
				var out []map[string]any
				for _, raw := range found.Records {
					var row map[string]any
					if json.Unmarshal(platform.Raw(raw), &row) != nil {
						t.Fatal("invalid output")
					}
					out = append(out, row)
				}
				return out
			}
			active := func() []map[string]any {
				var out []map[string]any
				for _, row := range rows() {
					if row["retired"] != true {
						out = append(out, row)
					}
				}
				return out
			}
			first := evaluate("first")
			initial := active()
			if len(initial) != 1 || initial[0]["state"] != "review" || initial[0]["seensince"] != initial[0]["since"] {
				t.Fatalf("Flow did not consume typed intervals: %+v", initial)
			}
			original := initial[0]["id"].(string)
			decide(t, tn, "operator", "build", "build.event.reason", "build.event", original, map[string]any{"reason": "Power isolated"}, at)
			before := snapshot(tn)
			previousCalls := calls
			reused, problem := tn.EvaluateObservation(context.Background(), member, ref, ObservationEvaluate{Record: "D1", Until: at, Key: "first"}, at)
			if problem != nil || !reused.Reused || calls != previousCalls || snapshot(tn) != before || reused.Receipt.Record.GetChangeId() != first.Receipt.Record.GetChangeId() {
				t.Fatal("accepted interval retry reread or duplicated results")
			}
			noticesBefore := len(tn.notices.all)
			tasksBefore, _, taskErr := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{Limit: 1000})
			if taskErr != nil || len(tasksBefore) != 1 || noticesBefore == 0 {
				t.Fatalf("missing original work notification: %d %d %v", len(tasksBefore), noticesBefore, taskErr)
			}
			evaluate("same-source")
			tasksAfter, _, taskErr := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{Limit: 1000})
			if taskErr != nil || len(tasksAfter) != 1 || len(tn.notices.all) != noticesBefore {
				t.Fatal("unchanged source duplicated work or notices")
			}
			if len(rows()) != 1 || active()[0]["reason"] != "Power isolated" {
				t.Fatal("unchanged reread duplicated event or erased reason")
			}
			values = []bool{false, false, true, true, true, false}
			evaluate("adjust")
			continued := active()
			if len(continued) != 1 || continued[0]["id"] != original || continued[0]["reason"] != "Power isolated" {
				t.Fatal("one-to-one correction replaced identity or reason")
			}
			values = []bool{false, true, true, false, true, false}
			before = snapshot(tn)
			fail = true
			if _, problem := tn.EvaluateObservation(context.Background(), member, ref, ObservationEvaluate{Record: "D1", Until: at, Key: "split"}, at); problem == nil || snapshot(tn) != before {
				t.Fatal("failed append exposed interval split")
			}
			fail = false
			evaluate("split")
			successors := active()
			if len(successors) != 2 {
				t.Fatalf("split did not create two successors: %+v", successors)
			}
			for _, row := range successors {
				if row["id"] == original || row["review"] != true || row["reason"] != nil && row["reason"] != "" {
					t.Fatalf("split copied an ambiguous human reason: %+v", row)
				}
			}
			links, problem := tn.TraverseLink(member, platform.AssetBinding{Ref: platform.AssetRef{App: "build", Kind: platform.AssetLinkType, Name: "eventhistory"}, SourceVersion: "1.link-1"}, "reverse", successors[0]["id"].(string), platform.Query{}, at)
			if problem != nil || links.Total != 1 || !bytes.Contains(platform.Raw(links), []byte(original)) || !bytes.Contains(platform.Raw(links), []byte("Power isolated")) {
				t.Fatalf("predecessor history unavailable: %s %v", platform.Raw(links), problem)
			}
			values = []bool{false, true, true, true, true, false}
			evaluate("merge")
			merged := active()
			if len(merged) != 1 || merged[0]["id"] == original || merged[0]["review"] != true {
				t.Fatalf("merge reused old identity: %+v", merged)
			}
			mergedID := merged[0]["id"]
			values = []bool{false, false, false, false, false, false}
			evaluate("gone")
			if len(active()) != 0 {
				t.Fatal("disappeared event remained current")
			}
			values = []bool{false, true, true, true, true, false}
			evaluate("reappeared")
			if len(active()) != 1 || active()[0]["id"] == original || active()[0]["id"] == mergedID {
				t.Fatal("retired identity revived")
			}
			before = snapshot(tn)
			incomplete = true
			if _, problem := tn.EvaluateObservation(context.Background(), member, ref, ObservationEvaluate{Record: "D1", Until: at, Key: "incomplete"}, at); problem == nil || snapshot(tn) != before {
				t.Fatal("incomplete source changed confirmed events")
			}
			incomplete = false
			if got := refuse(t, tn, "operator", "build", "build.device.observe", "build.device", "D1", nil, at); got == "ok" {
				t.Fatal("manual input forged an observation witness")
			}
			if got := refuse(t, tn, "operator", "build", "build.event.edit", "build.event", active()[0]["id"].(string), map[string]any{"since": at.Add(-time.Hour)}, at); got == "ok" {
				t.Fatal("manual edit rewrote observed provenance")
			}
			if got := refuse(t, tn, "operator", "build", "build.event.create", "build.event", "forged", map[string]any{"device": "D1", "since": at.Add(-time.Minute), "until": at, "source": "statewindow"}, at); got == "ok" {
				t.Fatal("manual create forged interval")
			}
			after := snapshot(tn)
			fresh := compose()
			if err := fresh.Restore(checkpoint); err != nil {
				t.Fatal(err)
			}
			fresh.Outbound = func(*http.Request, bool) (*http.Response, error) {
				t.Fatal("recovery reran source analysis")
				return nil, nil
			}
			if err := fresh.Replay(entries); err != nil || snapshot(fresh) != after {
				t.Fatalf("saved interval recovery differs: %v", err)
			}
			var batch resultformat.Batch
			for _, entry := range entries {
				var envelope struct{ Kind string }
				if json.Unmarshal(entry.Body, &envelope) != nil {
					t.Fatal("invalid saved envelope")
				}
				if envelope.Kind != "record-batch" {
					continue
				}
				decoded, _, err := resultformat.DecodeBatch(entry.Body)
				if err != nil {
					t.Fatal(err)
				}
				if decoded.Version == 10 {
					batch = decoded
					break
				}
			}
			if batch.Version != 10 || len(batch.Iterations) == 0 || batch.Decisions[0].Definition.Observation.Intervals == nil {
				t.Fatal("accepted interval proof or ordered witness missing")
			}
			originalBatch := platform.Raw(batch)
			for _, damage := range []func(*resultformat.Batch){
				func(b *resultformat.Batch) { b.Decisions[0].Definition.Observation.Record = "build.device/another" },
				func(b *resultformat.Batch) { b.Version = 9 },
				func(b *resultformat.Batch) { b.Decisions[0].Definition.Observation.Intervals = nil },
				func(b *resultformat.Batch) { b.Decisions[0].Definition.Observation.Intervals.Ready = false },
				func(b *resultformat.Batch) { b.Decisions[0].Definition.Observation.Intervals.Version = "unknown" },
				func(b *resultformat.Batch) {
					b.Decisions[0].Definition.Observation.Intervals.Unknown = []platform.ObservationUnknown{{Since: at.Add(-time.Minute), Until: at, Reason: "gap"}}
				},
			} {
				var invalid resultformat.Batch
				if json.Unmarshal(originalBatch, &invalid) != nil {
					t.Fatal("invalid batch fixture")
				}
				damage(&invalid)
				invalid.Digest, _ = resultformat.DigestBatch(invalid)
				if _, _, err := resultformat.DecodeBatch(platform.Raw(invalid)); err == nil {
					t.Fatal("incomplete or mismatched interval proof was recovered")
				}
			}
		})
	}
}
