package store

import (
	"testing"
	"time"

	"platformserver/platform"
)

func TestFileAuditAndRunIndexSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	open := func() *File {
		store, err := OpenFile(dir, func([]byte) (Identity, error) { return Identity{}, nil })
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	first := open()
	if err := first.SaveAuditEntries("tenant-a", []platform.AuditEntry{
		{ID: "audit-a", At: at, Member: "admin", App: "platform", Action: "build.object.create", Target: "build.object/OBJ1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.SavePersonalReads("tenant-a", []platform.PersonalRead{
		{ID: "read-a", At: at, Member: "admin", Type: "build.object", IDs: []string{"OBJ1"}, Fields: []string{"email"}},
	}); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"id":"source:SRC1","type":"sync","status":"failed"}`)
	if err := first.ReplaceRunIndex("tenant-a", "admin", []platform.RunIndexEntry{
		{ID: "source:SRC1", Type: "sync", Source: "source", App: "build", Status: "failed", At: at, Payload: payload},
	}); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := open()
	defer second.Close()
	audit, err := second.QueryAuditEntries(platform.AuditQuery{Tenant: "tenant-a", To: at.Add(time.Hour), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].ID != "audit-a" {
		t.Fatalf("audit after reopen: %+v", audit.Entries)
	}
	reads, err := second.QueryPersonalReads(platform.PersonalReadQuery{Tenant: "tenant-a", To: at.Add(time.Hour), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(reads.Entries) != 1 || reads.Entries[0].ID != "read-a" {
		t.Fatalf("personal reads after reopen: %+v", reads.Entries)
	}
	runs, err := second.QueryRunIndex(platform.RunIndexQuery{Tenant: "tenant-a", Scope: "admin", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs.Entries) != 1 || runs.Entries[0].ID != "source:SRC1" {
		t.Fatalf("run index after reopen: %+v", runs.Entries)
	}
}
