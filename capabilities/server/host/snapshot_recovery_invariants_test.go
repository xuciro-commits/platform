package host

import (
	"bytes"
	"os"
	"testing"

	"google.golang.org/protobuf/proto"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"platformserver/services/security/idp"
)

func TestInvariantK1PostgresSnapshotRestoresReleasedObjectPage(t *testing.T) {
	url := os.Getenv("PLATFORM_BENCH_DATABASE")
	if url == "" {
		t.Skip("requires an isolated PostgreSQL database")
	}
	ctx := t.Context()
	journal, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	const tenant = "k1-project-page"
	const object = "invariantproject.loaditem"
	deployment := Deployment{Flags: idp.Flags{SnapshotEvery: 10000}}
	start := func() (*invariantTenant, int64) {
		t.Helper()
		live := newInvariantTenantSetup(t, tenant, nil)
		live.Store = journal.Journals
		restored := map[string]int64{}
		err := deployment.tenantRestore(ctx, live.Tenant, journal.Journals, CodeOf(live.Tenant), restored, map[string]bool{}).Run(ctx)
		if err != nil || live.quarantined() {
			t.Fatalf("restore tenant: %v, fault %v", err, live.Fault.Load())
		}
		return live, restored[tenant]
	}
	live, _ := start()
	seedInvariantProject(t, live)
	live.accept(t, "build.object.create", build.ObjectType, "LOADOBJECT", invariantObject("loaditem", "Load item"))
	invariantPublishData(t, live, live.member(t), []build.JointDraftRef{{Kind: platform.AssetObject, ID: "LOADOBJECT"}}, "load-release")
	live.accept(t, object+".create", object, "L1", map[string]any{"note": "first"})
	edited := live.accept(t, object+".edit", object, "L1", map[string]any{"note": "revised"})
	images := live.recordImage(t)
	position := journal.Position(tenant)
	runtimekernel.NewSnapshotSaver(journal.Journals, deployment.SnapshotEvery).Save(ctx, live.snapshotSource(ctx, journal.Journals, CodeOf(live.Tenant)), true)

	restarted, checkpoint := start()
	if checkpoint != position || position == 0 {
		t.Fatalf("restored checkpoint %d, want %d", checkpoint, position)
	}
	restored := restarted.recordImage(t)
	for _, typ := range []string{object, "build.page"} {
		if len(images[typ]) == 0 || bytes.Equal(images[typ], []byte("[]")) || !bytes.Equal(restored[typ], images[typ]) {
			t.Fatalf("saved records or history changed for %s", typ)
		}
	}
	again, refusal := restarted.Submit(restarted.member(t), edited.Submission, invariantClock)
	if refusal != nil || !proto.Equal(again, edited) || journal.Position(tenant) != position {
		t.Fatalf("retry changed the saved receipt or journal position: %v, %v", again, refusal)
	}
	next := restarted.accept(t, object+".edit", object, "L1", map[string]any{"note": "after restart"})
	if next.Revision != edited.Revision+1 || journal.Position(tenant) != position+1 {
		t.Fatalf("next revision %d, position %d", next.Revision, journal.Position(tenant))
	}
}
