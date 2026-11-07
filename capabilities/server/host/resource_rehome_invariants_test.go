package host

import (
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func TestInvariantResourceOwnershipRehome(t *testing.T) {
	adminSeat := platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{
		ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: build.Builder},
	}}
	live := newInvariantTenant(t, "invariant-resource-rehome", adminSeat)
	live.accept(t, build.AppType+".create", build.AppType, "REHOMEPROJECT", map[string]any{"name": "rehomeproject", "title": "Rehome project"})
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("rehomeobject", "Rehome object"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, "build.rehomeobject.create", "build.rehomeobject", "ROW1", map[string]any{"note": "Keep my record"})

	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.rehomeobject"}
	before := resourceRecord(t, live, ref)
	payload := map[string]any{"entries": []map[string]any{{
		"id": before.ID, "project": "REHOMEPROJECT", "managedBy": "next.package", "version": before.Version,
	}}}
	submission := live.submission(build.SchemaResourceRehome, build.ResourceType, before.ID, payload)
	if _, refusal := live.Submit(live.member(t), submission, invariantClock); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("non-admin rehome: refusal=%v; want POLICY_DENIED", refusal)
	}

	admin, ok := live.Member("admin")
	if !ok {
		t.Fatal("the administrator is not seated")
	}
	submission = live.submission(build.SchemaResourceRehome, build.ResourceType, before.ID, payload)
	submission.PrincipalId = admin.ID
	if _, refusal := live.Submit(admin, submission, invariantClock); refusal != nil {
		t.Fatalf("rehome: %s: %s", refusal.Code, refusal.Message)
	}
	after := resourceRecord(t, live, ref)
	if after.ID != before.ID || after.Ref != before.Ref || after.Type != before.Type || after.Draft != before.Draft ||
		after.Title != before.Title || after.Variant != before.Variant || after.State != before.State {
		t.Fatalf("rehome changed resource identity: before=%+v after=%+v", before, after)
	}
	if after.Project != "REHOMEPROJECT" || after.ManagedBy != "next.package" || after.Version != before.Version+1 {
		t.Fatalf("rehome did not transfer ownership: %+v", after)
	}
	view, refusal := live.RecordOf(t.Context(), live.member(t), "build.rehomeobject", "ROW1", invariantClock)
	if refusal != nil || view.Record == nil {
		t.Fatalf("rehome changed record access: %v", refusal)
	}

	replayed := live.restore(t, live.entries, adminSeat)
	replayedResource := resourceRecord(t, replayed, ref)
	if replayedResource.Project != after.Project || replayedResource.ManagedBy != after.ManagedBy {
		t.Fatalf("replay did not preserve rehome: %+v", replayedResource)
	}
	if _, refusal := replayed.RecordOf(t.Context(), replayed.member(t), "build.rehomeobject", "ROW1", invariantClock); refusal != nil {
		t.Fatalf("replay changed the customer record: %v", refusal)
	}
}
