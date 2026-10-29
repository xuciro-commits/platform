package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	pgx "github.com/jackc/pgx/v5"

	"google.golang.org/protobuf/proto"
	"os"
	automation "platformserver/engine/automation"
	"platformserver/journal"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/studio/build"
	"testing"
	"time"
)

func TestPostgresSQLCheckpointRestoresTailWithoutResidentRecords(t *testing.T) {
	if os.Getenv("PLATFORM_TEST_DATABASE") == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("sql-checkpoint-tail-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, os.Getenv("PLATFORM_TEST_DATABASE"))
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant {
		return composeTenant(t, id, []platform.Seat{seatOf("ana", "stock:clerk")}, &formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
	}
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	first := decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, at)
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I2", map[string]any{"name": "two", "qty": 2}, at.Add(time.Second))
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	savedPosition, err := live.checkpointRuntime(ctx, j, "checkpoint-build")
	if err != nil {
		t.Fatal(err)
	}
	key, err := live.runtimeSnapshotKey("checkpoint-build")
	if err != nil {
		t.Fatal(err)
	}
	portableKey, err := live.snapshotKey("checkpoint-build")
	if err != nil || portableKey == key {
		t.Fatal("SQL owner checkpoint reused the portable snapshot namespace")
	}
	seq, raw, found, err := j.Snapshot(ctx, id, key)
	if err != nil || !found || seq != savedPosition {
		t.Fatalf("checkpoint not saved: %d %v", seq, err)
	}
	var saved tenantState
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Runtime == nil || len(saved.Records) != 0 || saved.Runtime.Records.Records != 2 {
		t.Fatal("SQL owner checkpoint copied current records or omitted its archive")
	}
	if err = compose().Restore(raw); err == nil {
		t.Fatal("SQL owner checkpoint was accepted as a portable snapshot")
	}

	decide(t, live, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 3}, at.Add(2*time.Second))
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I3", map[string]any{"name": "three", "qty": 7}, at.Add(3*time.Second))
	// A stale capture cannot publish an archive of the newer rows under its
	// original owner position. The older valid checkpoint remains available.
	if err = saveRuntimeSnapshot(ctx, j, id, savedPosition, key, raw); err == nil {
		t.Fatal("stale owner snapshot captured newer SQL records")
	}
	restored := compose()
	deployed := Deployment{SnapshotEvery: 1}
	positions := map[string]int64{}
	if err = deployed.restoreTenants(ctx, j, "another-build", []*Tenant{restored}, positions, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("tail recovery failed: %v, fault=%+v", err, restored.fault.Load())
	}
	if positions[id] != j.Position(id) {
		t.Fatal("startup did not save the recovered owner state at the current position")
	}
	for typ, et := range restored.records.types {
		if !et.rows.Sourced() || et.rows.Stored() != 0 || uniqueIndexCount(et) != 0 || referenceIndexCount(et) != 0 {
			t.Fatalf("current SQL state remained resident for %s", typ)
		}
	}
	member := memberOf(t, restored, "ana")
	view, refusal := restored.RecordOf(member, "stock.item", "I1", at.Add(time.Minute))
	if refusal != nil || view.Record.(Item).Qty != 3 || len(view.History) != 2 {
		t.Fatalf("tail changed the original record or history: %+v %v", view, refusal)
	}
	page, refusal := restored.Records(member, "stock.item", platform.Query{}, at.Add(time.Minute))
	if refusal != nil || page.Total != 3 {
		t.Fatalf("tail create or unchanged predecessor was lost: %+v %v", page, refusal)
	}
	repeated, refusal := restored.Submit(member, first.GetSubmission(), at.Add(time.Hour))
	if refusal != nil || repeated.GetChangeId() != first.GetChangeId() {
		t.Fatalf("checkpoint lost an original accepted answer: %v", refusal)
	}
	// Reopen the just-created same-position owner checkpoint as well: the
	// successful tail path alone must not hide a shortcut-only startup bug.
	again := compose()
	if err = deployed.restoreTenants(ctx, j, "next-build", []*Tenant{again}, map[string]int64{}, map[string]bool{}); err != nil || again.quarantined() {
		t.Fatalf("same-position recovery failed: %v, fault=%+v", err, again.fault.Load())
	}
}

func TestPostgresSQLCheckpointUpgradesAnUnchangedPortableSnapshot(t *testing.T) {
	if os.Getenv("PLATFORM_TEST_DATABASE") == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("sql-checkpoint-upgrade-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, os.Getenv("PLATFORM_TEST_DATABASE"))
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant {
		return composeTenant(t, id, []platform.Seat{seatOf("ana", "stock:clerk")}, &formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
	}
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1}, time.Now().UTC())
	state, seq, err := live.Snapshot(func() int64 { return j.Position(id) })
	if err != nil {
		t.Fatal(err)
	}
	key, err := live.snapshotKey("upgrade-build")
	if err != nil {
		t.Fatal(err)
	}
	if err = j.SaveSnapshot(ctx, id, seq, key, state); err != nil {
		t.Fatal(err)
	}
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	restored := compose()
	deployed := Deployment{SnapshotEvery: 1}
	if err = deployed.restoreTenants(ctx, j, "upgrade-build", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("portable migration failed: %v, fault=%+v", err, restored.fault.Load())
	}
	runtimeKey, err := live.runtimeSnapshotKey("upgrade-build")
	if err != nil {
		t.Fatal(err)
	}
	_, state, _, err = j.Snapshot(ctx, id, runtimeKey)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := runtimeCheckpoint(state)
	if err != nil || saved == nil {
		t.Fatalf("unchanged portable snapshot was not upgraded at its original position: %v", err)
	}
}

func TestPostgresSQLCheckpointRestoresPublishedTypesAndTailPublication(t *testing.T) {
	if os.Getenv("PLATFORM_TEST_DATABASE") == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("sql-checkpoint-build-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, os.Getenv("PLATFORM_TEST_DATABASE"))
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant { return builderTenant(t, id) }
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	firstType := publishObject(t, live, "builder", "FIRST", map[string]any{"name": "first", "title": "First", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	decide(t, live, "builder", build.ID, firstType+".create", firstType, "A1", map[string]any{"note": "before"}, at.Add(time.Second))
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	if _, err = live.checkpointRuntime(ctx, j, "build-checkpoint"); err != nil {
		t.Fatal(err)
	}
	decide(t, live, "builder", build.ID, firstType+".edit", firstType, "A1", map[string]any{"note": "after"}, at.Add(2*time.Second))
	secondType := publishObject(t, live, "builder", "SECOND", map[string]any{"name": "second", "title": "Second", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, at.Add(3*time.Second))
	decide(t, live, "builder", build.ID, secondType+".create", secondType, "B1", map[string]any{"note": "new type"}, at.Add(4*time.Second))
	restored := compose()
	deployed := Deployment{SnapshotEvery: 1}
	if err = deployed.restoreTenants(ctx, j, "next-build", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("published type recovery failed: %v fault=%+v", err, restored.fault.Load())
	}
	member := memberOf(t, restored, "builder")
	for _, want := range []struct {
		typ, id, note string
		history       int
	}{{firstType, "A1", "after", 2}, {secondType, "B1", "new type", 1}} {
		view, refusal := restored.RecordOf(member, want.typ, want.id, at.Add(time.Minute))
		if refusal != nil {
			t.Fatal(refusal)
		}
		var value map[string]any
		raw, _ := json.Marshal(view.Record)
		if err = json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if value["note"] != want.note || len(view.History) != want.history {
			t.Fatalf("published record lost its accepted state: %s %+v", want.typ, value)
		}
		if restored.records.types[want.typ].rows.Stored() != 0 {
			t.Fatalf("published current records remained resident: %s", want.typ)
		}
	}
}

// SQL has always retained ordinary batch bytes in its historical answer bucket.
// They must remain readable without becoming host snapshot reply metadata.
func TestPostgresSQLCheckpointKeepsOrdinaryBatchAnswersOnDemand(t *testing.T) {
	if os.Getenv("PLATFORM_TEST_DATABASE") == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("sql-checkpoint-batch-answer-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, os.Getenv("PLATFORM_TEST_DATABASE"))
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant {
		seat := seatOf("ana", "stock:clerk")
		seat.Member.Grants = []platform.Grant{{App: "stock", Role: "clerk"}, {App: "stock", Role: "line"}}
		return composeTenant(t, id, []platform.Seat{seat}, &formattedStock{stock: newStock(id), format: "stock.ledger.v1"})
	}
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "one", "qty": 1, "line": "L1"}, at)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	receipt := decide(t, live, "ana", "stock", "stock.item.count", "stock.item", "I1", map[string]any{"qty": 4}, at.Add(time.Second))
	answerKey := "stock/" + receipt.GetSubmission().GetIdempotencyKey()
	original, err := j.StateValue(ctx, id, "answer", answerKey, -1)
	if err != nil || len(original) == 0 {
		t.Fatalf("SQL lost its original batch answer bytes: %v", err)
	}
	batch, answer, err := acceptedformat.DecodeBatch(original)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := acceptedformat.BatchSubmission(batch, answer)
	if err != nil || batch.HasSavedAnswer(sub) {
		t.Fatalf("test did not produce an ordinary batch: %v", err)
	}
	portable, _, err := live.Snapshot(func() int64 { return j.Position(id) })
	if err != nil {
		t.Fatal(err)
	}
	var portableState tenantState
	if err = json.Unmarshal(portable, &portableState); err != nil {
		t.Fatal(err)
	}
	if len(portableState.AcceptedAnswers[answerKey]) != 0 {
		t.Fatal("ordinary SQL batch became incompatible portable reply metadata")
	}
	if err = compose().Restore(portable); err != nil {
		t.Fatalf("portable SQL snapshot rejected ordinary batch: %v", err)
	}
	if _, err = live.checkpointRuntime(ctx, j, "batch-answer"); err != nil {
		t.Fatal(err)
	}
	key, err := live.runtimeSnapshotKey("batch-answer")
	if err != nil {
		t.Fatal(err)
	}
	_, raw, _, err := j.Snapshot(ctx, id, key)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint tenantState
	if err = json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(checkpoint.AcceptedAnswers)+len(checkpoint.AcceptedInputs)+len(checkpoint.Refusals)+len(checkpoint.DecisionDefinitions)+len(checkpoint.SubmissionSchemas)+len(checkpoint.CompositeAnswers)+len(checkpoint.Composites) != 0 {
		t.Fatal("SQL owner checkpoint copied persisted reply history")
	}
	decide(t, live, "ana", "stock", "stock.item.count", "stock.item", "I1", map[string]any{"qty": 7}, at.Add(2*time.Second))
	restored := compose()
	deployment := Deployment{SnapshotEvery: 1}
	if err = deployment.restoreTenants(ctx, j, "batch-answer", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("ordinary batch checkpoint plus tail failed: %v fault=%+v", err, restored.fault.Load())
	}
	repeated, refusal := restored.Submit(memberOf(t, restored, "ana"), receipt.GetSubmission(), at.Add(time.Hour))
	if refusal != nil || !proto.Equal(repeated, receipt) {
		t.Fatalf("on-demand original batch answer changed: %v", refusal)
	}
	current, refusal := restored.RecordOf(memberOf(t, restored, "ana"), "stock.item", "I1", at)
	if refusal != nil || current.Record.(Item).Qty != 7 {
		t.Fatal("original answer retry replaced a later accepted record", current, refusal)
	}
	after, err := j.StateValue(ctx, id, "answer", answerKey, -1)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("snapshot changed original SQL answer bytes: %v", err)
	}
}

func TestPostgresSQLCheckpointReplaysReleaseWithEarlierFlow(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy-%v", legacy), func(t *testing.T) { testPostgresSQLCheckpointReleaseTail(t, legacy) })
	}
}

func testPostgresSQLCheckpointReleaseTail(t *testing.T, legacy bool) {
	if os.Getenv("PLATFORM_TEST_DATABASE") == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	id := fmt.Sprintf("checkpoint-flow-release-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, os.Getenv("PLATFORM_TEST_DATABASE"))
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant { return builderTenant(t, id, automation.New(id)) }
	live := compose()
	if _, err = j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	if err = live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	if _, err = live.checkpointRuntime(ctx, j, "flow-release"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	publishObject(t, live, "builder", "OBJECT", map[string]any{"name": "visit", "title": "Visit", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}, "states": []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}}}, at)
	decide(t, live, "builder", build.ID, "build.process.create", build.ProcessType, "PROCESS", map[string]any{"name": "review", "title": "Review", "object": "build.visit", "when": "open", "steps": []build.ProcessStep{{Name: "close", Kind: "action", Act: "close"}}}, at)
	decide(t, live, "builder", build.ID, build.SchemaProcess, build.ProcessType, "PROCESS", map[string]any{}, at)
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetFlow, Name: "build.review"}
	decide(t, live, "builder", build.ID, "build.app.create", build.AppType, "APP", map[string]any{"name": "desk", "title": "Desk", "resources": []platform.AssetRef{ref}}, at)
	member := memberOf(t, live, "builder")
	preview, err := live.PreviewRelease(member, platform.AssetApp, "APP")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	candidate, err := live.SaveReleaseCandidate(member, platform.AssetApp, "APP", preview.CandidateID, "save-desk", at)
	if err != nil {
		t.Fatal(err)
	}
	if legacy {
		persist := live.AcceptResult
		live.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
			saved, err := acceptedformat.DecodeRelease(entry.Body)
			if err != nil {
				return nil, err
			}
			saved.Version = 2
			for i := range saved.Installations {
				saved.Installations[i].Record = ""
				saved.Installations[i].Ref = nil
			}
			entry.Body, err = acceptedformat.EncodeRelease(saved)
			if err != nil {
				return nil, err
			}
			return persist(entry, key, hash)
		}
		original := j.Delta
		j.Delta = func(raw []byte) (journal.StateDelta, error) {
			delta, err := original(raw)
			delta.Replacements = nil
			return delta, err
		}
		defer func() { j.Delta = original }()
	}
	if _, err = live.ActivateRelease(member, candidate, "activate-desk", at); err != nil {
		t.Fatal(err)
	}
	if legacy {
		db, err := pgx.Connect(ctx, os.Getenv("PLATFORM_TEST_DATABASE"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close(ctx)
		if _, err = db.Exec(ctx, `update runtime_tenants set format=1 where tenant=$1`, id); err != nil {
			t.Fatal(err)
		}
		original, err := j.StateRecord(ctx, id, build.ObjectType, "OBJECT")
		if err != nil {
			t.Fatal(err)
		}
		var damaged map[string]json.RawMessage
		_ = json.Unmarshal(original, &damaged)
		var value map[string]any
		_ = json.Unmarshal(damaged["value"], &value)
		value["title"] = "Damaged outside accepted results"
		damaged["value"] = platform.Raw(value)
		bad := platform.Raw(damaged)
		if _, err = db.Exec(ctx, `update runtime_records set image=$2,query=$3::jsonb where tenant=$1 and type='build.object' and id='OBJECT'`, id, []byte(bad), string(bad)); err != nil {
			t.Fatal(err)
		}
		broken := compose()
		deployment := Deployment{SnapshotEvery: 1}
		if err = deployment.restoreTenants(ctx, j, "flow-release", []*Tenant{broken}, map[string]int64{}, map[string]bool{}); err != nil || !broken.quarantined() {
			t.Fatal("damaged old SQL state was admitted", err)
		}
		retained, err := j.StateRecord(ctx, id, build.ObjectType, "OBJECT")
		needed, upgradeErr := j.StateNeedsUpgrade(ctx, id)
		if err != nil || upgradeErr != nil || !needed || !bytes.Equal(retained, bad) {
			t.Fatal("rejected upgrade changed old SQL data", err, upgradeErr)
		}
		if _, err = db.Exec(ctx, `update runtime_records set image=$2,query=$3::jsonb where tenant=$1 and type='build.object' and id='OBJECT'`, id, []byte(original), string(original)); err != nil {
			t.Fatal(err)
		}
	}
	restored := compose()
	deployment := Deployment{SnapshotEvery: 1}
	if err = deployment.restoreTenants(ctx, j, "flow-release", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatalf("release tail recovery: %v %+v", err, restored.fault.Load())
	}
	got, err := restored.ReleaseCandidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "desk"}})
	if err != nil || got.ID != candidate || restored.ActiveRelease() != candidate {
		t.Fatalf("restored flow closure differs: %+v %v", got, err)
	}
	if restored.records.types[build.ProcessType].rows.Stored() != 0 {
		t.Fatal("restored process records remained resident")
	}
}
