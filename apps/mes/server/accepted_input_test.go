package mes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/enterprise"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestAcceptedEquipmentBatchAndDowntimeAreAtomic(t *testing.T) {
	plant, tn := plantTenant(t)
	now := time.Date(2026, 9, 28, 19, 0, 0, 0, time.UTC)
	batch, _ := json.Marshal(StateBatch{BatchID: "gateway-batch-1", Resource: "CNC-11",
		Samples: []Sample{{At: now, State: "down"}}})
	var entries []platformserver.Entry
	fail := true
	tn.AcceptResult = func(entry platformserver.Entry, _, _ string) ([]byte, error) {
		if len(plant.facts.Records(tn.ID)) != 0 || len(plant.Downtime()) != 0 ||
			tn.Connectors(now)[0].LastSeen != nil {
			t.Fatal("equipment fact, derived state or cursor escaped before commit")
		}
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if _, refusal := tn.Input(gw.Member, "states", batch, now); refusal == nil {
		t.Fatal("append failure accepted a gateway batch")
	}
	if len(plant.facts.Records(tn.ID)) != 0 || len(plant.Downtime()) != 0 {
		t.Fatal("rejected gateway batch changed plant state")
	}
	fail = false
	answer, refusal := tn.Input(gw.Member, "states", batch, now)
	fact, _ := answer.(*pb.FactRecord)
	if refusal != nil || fact == nil || len(entries) != 1 || len(plant.Downtime()) != 1 {
		t.Fatalf("gateway result was not committed: %v %+v", refusal, fact)
	}
	again, refusal := tn.Input(gw.Member, "states", batch, now.Add(time.Minute))
	if refusal != nil || again.(*pb.FactRecord).GetFactId() != fact.GetFactId() || len(entries) != 1 {
		t.Fatalf("gateway retry did not return the saved fact: %v", refusal)
	}
	freshPlant, fresh := plantTenant(t)
	if err := fresh.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if len(freshPlant.Downtime()) != 1 || len(freshPlant.facts.Records(fresh.ID)) != 1 {
		t.Fatal("recovery did not restore the observation and derived downtime")
	}
	platformserver.CheckReplay(t, tn, entries, func() *platformserver.Tenant {
		_, rebuilt := plantTenant(t)
		return rebuilt
	})
}

func TestJournalAcceptedEquipmentCrashAndRestart(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("plant-gateway-%d", time.Now().UnixNano())
	cleanup, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Close(ctx)
	defer cleanup.Exec(ctx, `delete from journal where tenant=$1`, id)
	if entries, err := journal.Entries(ctx, id, 0); err != nil || len(entries) != 0 {
		t.Fatalf("new tenant journal is not empty: %v %v", entries, err)
	}
	member := platform.Member{ID: "gateway-l1", Tenant: id, Roles: map[string]string{ID: string(Gateway)}}
	compose := func() (*Plant, *platformserver.Tenant) {
		p := New(id, DemoMaster())
		seat := platformserver.Seat{Subjects: []string{member.ID}, Member: member}
		tn, err := platformserver.NewTenant(id, platformserver.NewConsole(id, seat),
			enterprise.New(id, DemoOrganization(nil)), ai.New(id), work.New(id), flow.New(id),
			platformserver.NewAgents(id), p)
		if err == nil {
			err = tn.Connect(DemoConnectors(id)...)
		}
		if err != nil {
			t.Fatal(err)
		}
		return p, tn
	}
	plant, live := compose()
	now := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(StateBatch{BatchID: "gateway-crash", Resource: "CNC-11",
		Samples: []Sample{{At: now, State: "down"}}})
	live.AcceptResult = func(entry platformserver.Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, entry, key, hash); err != nil {
			return nil, err
		}
		return nil, errors.New("simulated crash after append")
	}
	if _, refusal := live.Input(member, "states", body, now); refusal == nil {
		t.Fatal("interrupted answer returned success")
	} else if journal.Position(id) == 0 {
		t.Fatalf("gateway input was refused before append: %v", refusal)
	}
	if len(plant.facts.Records(id)) != 0 || len(plant.Downtime()) != 0 || live.Connectors(now)[0].LastSeen != nil {
		t.Fatal("unapplied equipment result changed the plant")
	}
	reopened, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("committed gateway input was not recovered: %v %v", entries, err)
	}
	recoveredPlant, recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if len(recoveredPlant.facts.Records(id)) != 1 || len(recoveredPlant.Downtime()) != 1 ||
		recovered.Connectors(now)[0].LastSeen == nil {
		t.Fatal("recovery omitted the fact, downtime or connector mark")
	}
	recovered.AcceptResult = func(entry platformserver.Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, entry, key, hash)
	}
	if answer, refusal := recovered.Input(member, "states", body, now.Add(time.Minute)); refusal != nil ||
		answer.(*pb.FactRecord).GetFactId() == "" || reopened.Position(id) != 1 {
		t.Fatalf("retry did not return the saved equipment fact: %v", refusal)
	}
	platformserver.CheckReplay(t, recovered, entries, func() *platformserver.Tenant {
		_, fresh := compose()
		return fresh
	})
}
