package platformserver

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A legal result written by the old Process model retains its original row
// identity through journal replay and snapshot recovery. The next edit uses
// only canonical nodes while its Before/history chain still names that row.
func TestLegacyProcessResultRecoversThenEditsWithOriginalPredecessor(t *testing.T) {
	at := time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		tenant, err := NewTenant("process-history", NewConsole("process-history", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New("process-history"))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	created := compose()
	member, _ := created.Member("builder")
	var saved Entry
	created.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { saved = entry; return entry.Body, nil }
	payload := json.RawMessage(`{"name":"review","title":"Old review","object":"build.item","when":"open","steps":[{"name":"review","ask":"user","answers":["yes"],"branches":{"yes":"finish"}},{"name":"finish","act":"close"}]}`)
	submission := &pb.Submission{TenantId: created.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "create", Target: &pb.EntityRef{Type: build.ProcessType, Id: "P"}, Schema: &pb.SchemaRef{Name: build.ProcessType + ".create", Version: 1}, Payload: payload}
	if _, problem := created.Submit(member, submission, at); problem != nil {
		t.Fatal(problem.Message)
	}
	var result acceptedResult
	if err := json.Unmarshal(saved.Body, &result); err != nil {
		t.Fatal(err)
	}
	var oldFields map[string]json.RawMessage
	json.Unmarshal(payload, &oldFields)
	var image map[string]json.RawMessage
	json.Unmarshal(result.Row.Value, &image)
	image["steps"] = oldFields["steps"]
	result.Row.Value, _ = json.Marshal(image)
	for history := range result.Row.History {
		for field := range result.Row.History[history].Fields {
			change := &result.Row.History[history].Fields[field]
			if change.Field == "steps" {
				change.After = oldFields["steps"]
			}
		}
	}
	result.Digest, _ = digestAcceptedResult(result)
	saved.Body, _ = json.Marshal(result)
	expectedBefore, _ := canonicalDigest(result.Row)
	recovered := compose()
	if err := recovered.Replay([]Entry{saved}); err != nil {
		t.Fatal(err)
	}
	check := func(tenant *Tenant) {
		t.Helper()
		row := tenant.records.types[build.ProcessType].rows["P"]
		stored, err := acceptedRowOf(build.ProcessType, "P", row)
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := canonicalDigest(stored)
		if actual != expectedBefore {
			t.Fatal("typed decoding changed the legal historical row identity")
		}
		process := row.value.Interface().(build.Process)
		if process.Steps[0].Kind != "ask" || process.Steps[0].Cases["yes"] != "finish" {
			t.Fatal("API state did not expose canonical nodes")
		}
	}
	check(recovered)
	snapshotBytes, _, err := recovered.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	fromSnapshot := compose()
	if err := fromSnapshot.Restore(snapshotBytes); err != nil {
		t.Fatal(err)
	}
	check(fromSnapshot)
	edit := &pb.Submission{TenantId: recovered.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "edit", Target: &pb.EntityRef{Type: build.ProcessType, Id: "P"}, Schema: &pb.SchemaRef{Name: build.ProcessType + ".edit", Version: 1}, Payload: json.RawMessage(`{"title":"Canonical review","steps":[{"name":"review","kind":"ask","ask":"user","answers":["yes"],"cases":{"yes":"finish"}},{"name":"finish","kind":"action","act":"close"}]}`)}
	draft := fromSnapshot.newStagedDecision()
	app := fromSnapshot.app(build.ID).(platform.ResultApp)
	receipt, problem := decideAccepted(app, draft, member, edit, at.Add(time.Minute))
	if problem != nil {
		t.Fatal(problem.Message)
	}
	batchBytes, err := draft.batchResult(build.ID, edit, receipt, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	batch, _, err := decodeAcceptedBatch(batchBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Rows) != 1 || batch.Rows[0].Before != expectedBefore {
		t.Fatal("canonical edit lost its original historical predecessor")
	}
	principal, _ := json.Marshal(member)
	next := Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: batchBytes, At: at.Add(time.Minute)}
	if err := fromSnapshot.Replay([]Entry{next}); err != nil {
		t.Fatal(err)
	}
	if len(fromSnapshot.records.types[build.ProcessType].rows["P"].original) != 0 {
		t.Fatal("new mutation retained the legacy editing format")
	}
	complete := compose()
	if err := complete.Replay([]Entry{saved, next}); err != nil {
		t.Fatal(err)
	}
	if snapshot(complete) != snapshot(fromSnapshot) {
		t.Fatal("old result plus new edit did not equal snapshot plus new edit")
	}
}
