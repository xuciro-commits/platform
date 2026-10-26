package platformserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
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
