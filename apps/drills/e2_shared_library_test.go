// Drill E2 (docs/Platform.md §8): a personal music library becomes a shared
// family library. Music-shaped decisions run on the kernel alone to show which
// layer the change touches; the Music app's own work is listed in its queue.
package drills

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

const (
	library    = "tenant-7c1e" // a personal space with a globally unique tenant ID from the start
	correction = "music.metadata-correction"
	recording  = "music.recording"
)

var t0 = time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)

func decision(principal, authority, key string, evidence ...string) *pb.Submission {
	return &pb.Submission{TenantId: library, PrincipalId: principal, Authority: authority,
		Target: &pb.EntityRef{Type: recording, Id: "rec-42"}, Schema: &pb.SchemaRef{Name: correction, Version: 1},
		IdempotencyKey: key, Payload: []byte(`{"field":"title","value":"晴天"}`), EvidenceFactIds: evidence}
}

func declaration(authority string, kind pb.AuthorityKind, epoch uint32) *pb.AuthorityDeclaration {
	return &pb.AuthorityDeclaration{TenantId: library, DataClass: recording, Kind: kind, AuthorityId: authority, Epoch: epoch}
}

func TestDrillE2PersonalLibraryBecomesShared(t *testing.T) {
	schemas := kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: correction, Version: 1}}, nil)

	// 1. Personal: Ada's Mac is the authority; her corrections apply at once (K5 A4).
	device := kernel.NewAuthorities("mac-ada")
	device.Declare(declaration("mac-ada", pb.AuthorityKind_AUTHORITY_KIND_DEVICE, 1))
	deviceLog := kernel.NewChangeLog(schemas)
	for i, key := range []string{"c-1", "c-2"} {
		s := decision("ada", "mac-ada", key)
		if state, _ := device.Enqueue(s); state != pb.SubmissionState_SUBMISSION_STATE_CONFIRMED {
			t.Fatalf("device authority should confirm locally, got %v", state)
		}
		deviceLog.Submit(s, t0.Add(time.Duration(i)*time.Minute))
	}

	// 2. Sharing migrates the authority to the family server (K5 A2) and the server
	//    adopts the device's history unchanged (A10). The tenant ID does not change.
	server := kernel.NewAuthorities("family-server")
	server.Declare(declaration("mac-ada", pb.AuthorityKind_AUTHORITY_KIND_DEVICE, 1))
	if err := server.Declare(declaration("family-server", pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, 2)); err != nil {
		t.Fatal(err)
	}
	facts := kernel.NewFactLog(kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: "music.provider-title", Version: 1}}, nil))
	serverLog := kernel.NewChangeLog(schemas)
	serverLog.Facts = func(tenant, id string) bool {
		for _, r := range facts.Records(tenant) {
			if r.GetFactId() == id {
				return true
			}
		}
		return false
	}
	if err := serverLog.Adopt(deviceLog.Records(library)); err != nil {
		t.Fatal(err)
	}

	// 3. Family members are principals of the same tenant; the policy (domain data)
	//    lets adults correct and children only listen (K6 T3).
	roles := map[string]string{"ada": "adult", "bob": "adult", "kid": "child"}
	receiver := kernel.Receiver{Changes: serverLog, Authorities: server,
		Policy: func(c kernel.Caller, _ *pb.Submission) bool { return roles[c.Principal] == "adult" }}
	receive := func(principal string, s *pb.Submission) string {
		if _, err := receiver.Receive(kernel.Caller{Tenant: library, Principal: principal}, s, t0.Add(time.Hour), nil); err != nil {
			return err.Error()
		}
		return "ok"
	}

	// A correction Ada made offline on the Mac after the migration names the old
	// authority: refused, the Mac refreshes its declarations (A9) and resubmits.
	if got := receive("ada", decision("ada", "mac-ada", "c-3")); got != "ERROR_CODE_NOT_AUTHORITY" {
		t.Fatalf("stale authority accepted: %s", got)
	}
	if got := receive("ada", decision("ada", "family-server", "c-3")); got != "ok" {
		t.Fatal(got)
	}
	// Replaying an adopted decision returns the Mac's original record (A10, C4).
	if record, _ := serverLog.Submit(decision("ada", "mac-ada", "c-1"), t0.Add(2*time.Hour)); record.GetChangeId() != deviceLog.Records(library)[0].GetChangeId() {
		t.Fatal("adopted history was not kept")
	}
	// Bob corrects citing a provider's claim as evidence (K4 C11); the kid may not.
	claim, _ := facts.Record(&pb.Fact{TenantId: library, Kind: pb.FactKind_FACT_KIND_CLAIM, Subject: &pb.EntityRef{Type: recording, Id: "rec-42"},
		Attribute: "title", Schema: &pb.SchemaRef{Name: "music.provider-title", Version: 1}, IdempotencyKey: "mb-1", Payload: []byte(`"晴天"`),
		Provenance: &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: "musicbrainz"}, SourceTime: timestamppb.New(t0), Confidence: 0.9}}, t0)
	if got := receive("bob", decision("bob", "family-server", "c-4", claim.GetFactId())); got != "ok" {
		t.Fatal(got)
	}
	if got := receive("kid", decision("kid", "family-server", "c-5")); got != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("child corrected metadata: %s", got)
	}
	if n := len(serverLog.Records(library)); n != 4 {
		t.Fatalf("shared log has %d records, want 4 (2 adopted, Ada's resubmission, Bob's)", n)
	}
}
