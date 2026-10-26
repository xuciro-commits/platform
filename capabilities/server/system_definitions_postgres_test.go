package platformserver

import (
	"os"
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
	deployment := &Deployment{SnapshotEvery: 1}
	owner, business := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "pg-business")
	tenants := []*Tenant{business, owner} // Recovery must establish the system owner first.
	if err := deployment.restoreTenants(ctx, database, "self-hosting-pg", tenants, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if database.Position("system") != 0 || database.Position("pg-business") != 0 {
		t.Fatal("the PostgreSQL rehearsal database is not empty")
	}
	for _, tenant := range tenants {
		tenant.attachJournal(ctx, database)
	}
	definitionDraft(t, owner, "PostgreSQL shared note")
	proposeDefinition(t, owner, "postgres-definitions", nil)
	mustDefinitionDecision(t, owner, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{})
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
	if got := definitionOf(t, recoveredBusiness); got.Source != "system" || got.Entity.Title != "PostgreSQL shared note" {
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
	path := "web/projection-check.ts"
	for revision := 0; revision < 2; revision++ {
		mustDefinitionDecision(t, recoveredOwner, "builder", system.SchemaFileSave, system.FileType, "source-edit", map[string]any{"path": path, "content": "export const n = 1;", "revision": revision})
	}
	if err = projection.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var revision int
	if err = pool.QueryRow(ctx, `select revision from tenant_system.system_file where path=$1`, path).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 2 {
		t.Fatalf("projected revision = %d; declared file revision was shadowed by its embedded record", revision)
	}

}
