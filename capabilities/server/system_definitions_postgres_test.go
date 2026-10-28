package platformserver

import (
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/system"
	"testing"
	"time"
)

// The caller provides a fresh, isolated database. Never point this rehearsal at
// a deployment database: system is deliberately the real singleton tenant ID.
func TestSystemDefinitionsPostgresCheckpointRestoresSharedRuntime(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE needs an isolated PostgreSQL database")
	}
	ctx := t.Context()
	database, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	if entries, err := database.Entries(ctx, "system", 0); err != nil || len(entries) != 0 {
		t.Fatalf("rehearsal database must be empty: %v", err)
	}
	// Seed exact pre-freeze accepted results through the original journal owner.
	// The current runtime must restore them without reopening system writes.
	for _, entry := range retainedSystemHistory(t, "definitions-active") {
		var saved struct {
			RequestHash string
			Receipt     json.RawMessage
		}
		if err := json.Unmarshal(entry.Body, &saved); err != nil {
			t.Fatal(err)
		}
		receipt := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(saved.Receipt, receipt); err != nil {
			t.Fatal(err)
		}
		if _, err := database.AppendAccepted(ctx, "system", entry, receipt.GetSubmission().GetIdempotencyKey(), saved.RequestHash); err != nil {
			t.Fatal(err)
		}
	}
	deployment := &Deployment{SnapshotEvery: 1}
	owner, business := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "pg-business")
	tenants := []*Tenant{business, owner} // Recovery must establish the system owner first.
	if err := deployment.restoreTenants(ctx, database, "self-hosting-pg", tenants, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if database.Position("system") != int64(len(retainedSystemHistory(t, "definitions-active"))) || database.Position("pg-business") != 0 {
		t.Fatal("the PostgreSQL rehearsal database is not empty")
	}
	for _, tenant := range tenants {
		tenant.attachJournal(ctx, database)
	}
	mustDefinitionDecision(t, business, "builder", "build.globalnote.create", "build.globalnote", "durable-note", map[string]any{"title": "Only business data belongs here"})
	for _, tenant := range tenants {
		if _, err := tenant.checkpointRuntime(ctx, database, "self-hosting-pg"); err != nil {
			t.Fatal(err)
		}
	}
	database.Close()
	database, err = OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	recoveredOwner, recoveredBusiness := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "pg-business")
	if err := deployment.restoreTenants(ctx, database, "self-hosting-pg", []*Tenant{recoveredBusiness, recoveredOwner}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []*Tenant{recoveredOwner, recoveredBusiness} {
		if tenant.quarantined() {
			t.Fatalf("%s quarantined: %+v", tenant.ID, tenant.fault.Load())
		}
		tenant.attachJournal(ctx, database)
	}
	if got := definitionOf(t, recoveredBusiness); got.Source != "system" || got.Entity.Title != "Retained system title" {
		t.Fatalf("recovered shared definition: %+v", got)
	}
	member, _ := recoveredBusiness.Member("builder")
	row, refusal := recoveredBusiness.RecordOf(member, "build.globalnote", "durable-note", time.Now())
	if refusal != nil {
		t.Fatal(refusal)
	}
	if row.Values["title"] != "Only business data belongs here" {
		t.Fatalf("SQL record lost: %+v", row.Values)
	}
	if database.Position("pg-business") != 1 {
		t.Fatal("projection wrote into the business journal")
	}
	pool, ok := journalPool(database)
	if !ok {
		t.Fatal("PostgreSQL pool unavailable")
	}
	projection, err := Project(ctx, pool, recoveredOwner)
	if err != nil {
		t.Fatal(err)
	}
	member, _ = recoveredOwner.Member("builder")
	input := &pb.Submission{TenantId: "system", PrincipalId: member.ID, Authority: system.ID, IdempotencyKey: "frozen-file", Target: &pb.EntityRef{Type: system.FileType, Id: "source-edit"}, Schema: &pb.SchemaRef{Name: system.SchemaFileSave, Version: 1}, Payload: []byte(`{"path":"web/projection-check.ts","content":"new source"}`)}
	if _, refusal := recoveredOwner.Submit(member, input, time.Now()); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("SQL runtime reopened system source writes: %v", refusal)
	}
	if err := projection.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from tenant_system.system_file`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("frozen source request inserted a SQL record")
	}
}
