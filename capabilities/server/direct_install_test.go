package platformserver

import (
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// ADR-0048 D5b: a tenant that declares the production profile delivers through
// a saved joint candidate, and the direct install its authoring surfaces offered
// is refused by the owner — a hidden button is not a retirement. The draft, its
// versions and the recovery path are untouched, and the development profile
// keeps the same install implementation.
func TestProductionProfileRefusesDirectInstallAndKeepsDelivery(t *testing.T) {
	at := time.Date(2026, 10, 5, 11, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		tn, err := NewTenant("release-profile", NewConsole("release-profile",
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, PlatformApp: Admin}}},
			Seat{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User, flow.ID: "member"}}}),
			work.New("release-profile"), flow.New("release-profile"), build.New("release-profile"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	member, _ := tn.Member("builder")
	keys := 0
	submit := func(authority, schema, typ, id string, payload any) *kernel.Error {
		t.Helper()
		keys++
		_, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: authority,
			IdempotencyKey: fmt.Sprint(keys), Schema: &pb.SchemaRef{Name: schema, Version: 1},
			Target: &pb.EntityRef{Type: typ, Id: id}, Payload: platform.Raw(payload)}, at)
		return err
	}
	must := func(authority, schema, typ, id string, payload any) {
		t.Helper()
		if err := submit(authority, schema, typ, id, payload); err != nil {
			t.Fatalf("%s: %v: %s", schema, err, err.Message)
		}
	}
	profile := func(value string) {
		t.Helper()
		must(PlatformApp, SchemaSettingSet, SettingType, build.ID+"/"+build.SettingReleaseProfile, map[string]string{"value": value})
	}

	// The default profile is the development/import one the authoring surfaces
	// have always used: the direct install still installs.
	must(build.ID, build.ObjectType+".create", build.ObjectType, "DEV", map[string]any{
		"name": "devsigned", "title": "Signed off", "plural": "Signed off",
		"fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	must(build.ID, build.SchemaPublish, build.ObjectType, "DEV", struct{}{})
	if installed, _ := tn.RecordOf(member, build.ObjectType, "DEV", at); installed.Record.(build.Object).Published == "" {
		t.Fatal("the development profile lost its direct install")
	}

	// The authoring surfaces read the profile from the builder itself, so a
	// builder without a platform role knows which entry it may offer.
	if out, err := tn.Read(member, build.ReadReleaseProfile); err != nil || !out.(build.ReleaseProfile).DirectInstall {
		t.Fatalf("development profile read: %+v %v", out, err)
	}

	// Declared production: the same entry is refused, and the draft is still a
	// draft with nothing installed behind it.
	profile(build.ProfileProduction)
	must(build.ID, build.ObjectType+".create", build.ObjectType, "O", map[string]any{
		"name": "prodintake", "title": "Prod intake", "plural": "Prod intakes",
		"fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	if out, err := tn.Read(member, build.ReadReleaseProfile); err != nil || out.(build.ReleaseProfile).DirectInstall {
		t.Fatalf("production profile read: %+v %v", out, err)
	}
	refused := submit(build.ID, build.SchemaPublish, build.ObjectType, "O", struct{}{})
	if refused == nil || refused.Message == "" {
		t.Fatalf("production accepted a direct install: %v", refused)
	}
	draft, _ := tn.RecordOf(member, build.ObjectType, "O", at)
	if object := draft.Record.(build.Object); object.Published != "" || object.Installed != "" {
		t.Fatalf("the refused install changed the draft: %+v", object)
	}
	// The refusal is the profile's, not the draft's: it names what to do instead.
	for _, want := range []string{"build.object.publish", "/v1/releases/candidates"} {
		if !strings.Contains(refused.Message, want) {
			t.Fatalf("refusal does not name %q: %s", want, refused.Message)
		}
	}

	// Delivery itself is unchanged: the joint candidate installs the same
	// definition, and replaying the accepted log keeps it.
	must(build.ID, build.PageType+".create", build.PageType, "PAGE", map[string]any{
		"name": "prodintakes", "title": "Prod intakes", "object": "build.prodintake",
		"sections": []build.Section{{Widget: "table", Fields: []string{"note"}}}})
	preview, err := tn.PreviewReleaseDrafts(member, []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetPage, ID: "PAGE"}})
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("production joint preview: %+v %v", preview, err)
	}
	keys++
	if _, err := tn.SaveReleaseCandidates(member, preview.Drafts, preview.CandidateID, fmt.Sprint(keys), at); err != nil {
		t.Fatalf("production save: %v", err)
	}
	keys++
	if _, err := tn.ActivateRelease(member, preview.CandidateID, fmt.Sprint(keys), at); err != nil {
		t.Fatalf("production activation: %v", err)
	}
	reader := platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User, flow.ID: "member"}}
	delivered := false
	for _, definition := range tn.Definitions(reader) {
		if definition.Ref.Kind == platform.AssetObject && definition.Ref.Name == "build.prodintake" {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("the joint candidate did not deliver in production")
	}
	CheckReplay(t, tn, entries, compose)
}
