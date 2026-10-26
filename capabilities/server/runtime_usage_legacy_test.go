package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/journal"
	"platformserver/platform"
)

func TestPostgresLegacyUsageOwnerMigration(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	for _, format := range []int{1, 2} {
		for _, configured := range []bool{false, true} {
			for _, damaged := range []bool{false, true} {
				t.Run(fmt.Sprintf("format-%d/configured-%t/damaged-%t", format, configured, damaged), func(t *testing.T) {
					testPostgresLegacyUsageOwnerMigration(t, dsn, format, configured, damaged)
				})
			}
		}
	}
}

func testPostgresLegacyUsageOwnerMigration(t *testing.T, dsn string, format int, configured, damaged bool) {
	t.Helper()
	ctx := context.Background()
	id := fmt.Sprintf("legacy-usage-%d", time.Now().UnixNano())
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	compose := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("ana", "ai:admin")}, ai.New(id))
	}
	live := compose()
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.attachJournal(ctx, j)
	if err := live.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	// Reproduce the original SQL writer: complete owner images are retained,
	// but a later committed meter reading is absent from app-state/ai.
	originalDelta := j.Delta
	j.Delta = func(raw []byte) (journal.StateDelta, error) {
		delta, err := originalDelta(raw)
		delta.ValueUpdates = nil
		return delta, err
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	if configured {
		decide(t, live, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": "http://127.0.0.1:1/v1"}, at)
		decide(t, live, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/model", map[string]string{"access": "users"}, at)
	}
	usages := []platform.AIUsage{
		{Member: "ana", Model: "local/model", Input: 7, Output: 3, At: at.Add(time.Second)},
		{Member: "ana", Model: "local/model", Input: 11, Output: 2, At: at.Add(3 * time.Second)},
	}
	if refusal := live.meter(memberOf(t, live, "ana"), usages[0]); refusal != nil {
		t.Fatal(refusal)
	}
	if configured {
		// This sealed owner image already includes the first reading. Recovery
		// must use this last image, rather than the initial state or replaying
		// both readings onto the stale SQL value.
		decide(t, live, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/model", map[string]any{"access": "users", "dailyTokens": 100}, at.Add(2*time.Second))
	}
	preimage, err := j.StateValue(ctx, id, "app-state", ai.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		owner, err := live.app(ai.ID).(platform.AcceptedStateApp).AcceptedState()
		if err != nil || !sameStoredJSON(preimage, owner) {
			t.Fatalf("last sealed AI owner image was not retained: %v", err)
		}
	} else {
		initial, err := ai.New(id).AcceptedState()
		if err != nil || !sameStoredJSON(preimage, initial) {
			t.Fatalf("unconfigured SQL owner differs from official initial state: %v", err)
		}
	}
	if refusal := live.meter(memberOf(t, live, "ana"), usages[1]); refusal != nil {
		t.Fatal(refusal)
	}
	j.Delta = originalDelta
	want, err := live.app(ai.ID).(platform.AcceptedStateApp).AcceptedState()
	if err != nil || sameStoredJSON(want, preimage) {
		t.Fatalf("old writer did not omit committed usage: %v", err)
	}
	if _, err := j.Pool().Exec(ctx, `update runtime_tenants set format=$2 where tenant=$1`, id, format); err != nil {
		t.Fatal(err)
	}
	if damaged {
		var state map[string]json.RawMessage
		if err := json.Unmarshal(preimage, &state); err != nil {
			t.Fatal(err)
		}
		state["daily"] = platform.Raw(map[string]int{"2026-10-20|ana": 999})
		bad := platform.Raw(state)
		if _, err := j.Pool().Exec(ctx, `update runtime_values set image=$2,query=$3::jsonb where tenant=$1 and owner='app-state' and key='ai'`, id, []byte(bad), string(bad)); err != nil {
			t.Fatal(err)
		}
	}
	// Compare original journal, business rows/history, receipts and other owner
	// values. A rejected conversion must also preserve the damaged AI bytes and
	// old format marker, rather than partially repairing any of them.
	retained := func() map[string]json.RawMessage {
		t.Helper()
		tables := []string{"journal", "runtime_records", "runtime_history", "runtime_changes", "runtime_results", "runtime_unique", "runtime_unique_formats", "runtime_values"}
		if damaged {
			tables = append(tables, "runtime_tenants", "snapshots")
		}
		out := map[string]json.RawMessage{}
		for _, table := range tables {
			where := "tenant=$1"
			if table == "runtime_values" && !damaged {
				where += " and not (owner='app-state' and key='ai')"
			}
			query := "select coalesce(jsonb_agg(to_jsonb(x) order by to_jsonb(x)::text),'[]'::jsonb) from " + table + " x where " + where
			var raw json.RawMessage
			if err := j.Pool().QueryRow(ctx, query, id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			out[table] = raw
		}
		return out
	}
	before := retained()
	position := j.Position(id)
	deployment := Deployment{SnapshotEvery: 1}
	for restart := 0; restart < 2; restart++ {
		restored := compose()
		if err := deployment.restoreTenants(ctx, j, "legacy-usage", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil {
			t.Fatal(err)
		}
		if damaged {
			if !restored.quarantined() {
				t.Fatal("damaged legacy AI owner was admitted")
			}
		} else {
			if restored.quarantined() {
				t.Fatalf("legacy AI usage restoration failed: %+v", restored.fault.Load())
			}
			actual, err := restored.app(ai.ID).(platform.AcceptedStateApp).AcceptedState()
			if err != nil || !sameStoredJSON(actual, want) || !reflect.DeepEqual(restored.ai.Usage(), usages) {
				t.Fatalf("restored AI usage was lost or counted twice: %v, usage=%+v", err, restored.ai.Usage())
			}
			stored, err := j.StateValue(ctx, id, "app-state", ai.ID, -1)
			if err != nil || !sameStoredJSON(stored, want) {
				t.Fatalf("SQL AI owner does not match recovered final state: %v", err)
			}
		}
		var gotFormat int
		var gotPosition int64
		if err := j.Pool().QueryRow(ctx, `select format,seq from runtime_tenants where tenant=$1`, id).Scan(&gotFormat, &gotPosition); err != nil {
			t.Fatal(err)
		}
		wantFormat := 3
		if damaged {
			wantFormat = format
		}
		if gotFormat != wantFormat || gotPosition != position || j.Position(id) != position || !reflect.DeepEqual(before, retained()) {
			t.Fatalf("conversion changed retained data or its position: format=%d want=%d, seq=%d want=%d", gotFormat, wantFormat, gotPosition, position)
		}
	}
}
