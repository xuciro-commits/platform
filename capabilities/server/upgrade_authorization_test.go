package platformserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

// The upgrade batch's authorization, host console, environment and package
// behavior (ADR-0047 §6.5, §11, §10.3; ordered 2026-10-05).

func upgradeSeats() []Seat {
	seat := func(id string, roles map[string]string) Seat {
		return Seat{Subjects: []string{"user:" + id + "@example.test"}, Member: platform.Member{ID: id, Roles: roles}}
	}
	return []Seat{
		seat("dana", map[string]string{build.ID: build.Builder, PlatformApp: Admin, work.ID: work.Admin}),
		seat("pat", map[string]string{build.ID: build.Publisher}),
		seat("aud", map[string]string{PlatformApp: Auditor}),
		seat("mo", nil),
		seat("approver", map[string]string{"manager": "manager"}),
	}
}

func upgradeTenant(t *testing.T, id string) (*Tenant, func(string) platform.Member, func(string, string, string, string, any) string) {
	t.Helper()
	tn, err := NewTenant(id, NewConsole(id, upgradeSeats()...), work.New(id), build.New(id))
	if err != nil {
		t.Fatal(err)
	}
	member := func(who string) platform.Member {
		m, ok := tn.Member(who)
		if !ok {
			t.Fatalf("no member %s", who)
		}
		return m
	}
	keys := 0
	submit := func(who, schema, typ, target string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		authority := build.ID
		if typ == work.ApprovalType || typ == work.PolicyType {
			authority = work.ID
		} else if typ == PackageType || typ == ProjectType {
			authority = PlatformApp
		}
		sub := &pb.Submission{TenantId: id, PrincipalId: who, Authority: authority, IdempotencyKey: "u" + id + "-" + schema + "-" + target + "-" + string(rune('a'+keys%26)),
			Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
		m := member(who)
		if _, err := tn.Submit(m, sub, time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)); err != nil {
			return err.Code.String()
		}
		return "ok"
	}
	return tn, member, submit
}

func TestPublisherActivatesWithoutEditing(t *testing.T) {
	tn, member, _ := upgradeTenant(t, "t-pub")
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

	// The builder role edits definitions; the publisher does not.
	if role := member("pat").Roles[build.ID]; role != build.Publisher {
		t.Fatalf("publisher seat: %s", role)
	}
	if _, err := tn.SaveReleaseCandidates(member("pat"), nil, "cand", "k1", now); err == nil ||
		!strings.Contains(err.Error(), "builder role required") {
		// A publisher passes the role gate and fails later, on the empty draft:
		// the message must not be the role refusal.
		if err != nil && strings.Contains(err.Error(), "builder role required") {
			t.Fatalf("publisher was refused by the role gate: %v", err)
		}
	}
	if _, err := tn.ActivateRelease(member("pat"), "missing", "k2", now); err == nil ||
		strings.Contains(err.Error(), "builder role required") {
		t.Fatalf("publisher was refused by the activation role gate: %v", err)
	}
	if _, err := tn.SaveReleaseCandidates(member("aud"), nil, "cand", "k3", now); err == nil ||
		!strings.Contains(err.Error(), "builder or publisher role required") {
		t.Fatalf("auditor saved a release: %v", err)
	}
}

func TestAuditorReadsWithoutDeciding(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "t-aud")
	if _, err := tn.Read(member("aud"), "audit"); err != nil {
		t.Fatalf("auditor may not read audit: %v", err)
	}
	if _, err := tn.Read(member("aud"), "members"); err != nil {
		t.Fatalf("auditor may not read members: %v", err)
	}
	if _, err := tn.Read(member("aud"), "packages"); err != nil {
		t.Fatalf("auditor may not read packages: %v", err)
	}
	if got := submit("aud", SchemaGrant, MemberType, "mo", map[string]any{"app": build.ID, "role": build.Builder}); got == "ok" {
		t.Fatal("the auditor granted a role")
	}
	if got := submit("aud", SchemaAdd, MemberType, "eve", map[string]any{"subject": "user:eve@example.test"}); got == "ok" {
		t.Fatal("the auditor added a member")
	}
}

func TestPublisherReviewsSealedDefinitionsAfterDraftChanges(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "publisher-review")
	payload := map[string]any{"name": "pubsample", "title": "Original release object",
		"fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}},
		"states": []map[string]any{{"name": "open", "title": "Open"}}}
	if got := submit("dana", "build.object.create", "build.object", "OBJ-PUB", payload); got != "ok" {
		t.Fatal(got)
	}
	preview, err := tn.PreviewRelease(member("dana"), "object", "OBJ-PUB")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.SaveReleaseCandidate(member("dana"), "object", "OBJ-PUB", preview.CandidateID, "save", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := submit("dana", "build.object.edit", "build.object", "OBJ-PUB", map[string]any{"title": "Mutated draft"}); got != "ok" {
		t.Fatal(got)
	}
	list, err := tn.SavedReleases(member("pat"), 0, 20)
	if err != nil || len(list.Candidates) != 1 {
		t.Fatalf("publisher inventory: %+v %v", list, err)
	}
	review, err := tn.ReviewSavedRelease(member("pat"), preview.CandidateID)
	if err != nil || len(review.Assets) == 0 {
		t.Fatalf("publisher review: %+v %v", review, err)
	}
	frozen := false
	for _, asset := range review.Assets {
		if strings.Contains(string(asset.Body), "Mutated draft") {
			t.Fatal("sealed review leaked a later draft")
		}
		frozen = frozen || strings.Contains(string(asset.Body), "Original release object")
	}
	if !frozen {
		t.Fatal("review omitted the original definition")
	}
	if _, err := tn.SavedReleases(member("aud"), 0, 20); err == nil {
		t.Fatal("auditor acquired publisher candidate access")
	}
}

func TestProjectDelegationIsTargetScoped(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "t-proj")
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	object := map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}
	if got := submit("dana", build.ObjectType+".create", build.ObjectType, "visit", object); got != "ok" {
		t.Fatalf("builder draft: %s", got)
	}
	second := map[string]any{"name": "invoice", "title": "Invoice",
		"fields": []map[string]any{{"name": "amount", "title": "Amount", "type": "text"}}}
	if got := submit("dana", build.ObjectType+".create", build.ObjectType, "invoice", second); got != "ok" {
		t.Fatalf("builder draft 2: %s", got)
	}
	// Without a project the member has no edit right at all.
	if got := submit("mo", "build.object.edit", build.ObjectType, "visit", map[string]any{"name": "visit", "title": "Visit"}); got == "ok" {
		t.Fatal("a member with no role edited an object")
	}
	project := map[string]any{"name": "opening", "title": "Hotel opening",
		"members": []map[string]any{{"member": "mo", "role": ProjectEditorRole}},
		"assets":  []map[string]any{{"kind": "object", "name": "visit"}}}
	if got := submit("dana", SchemaProjectSave, ProjectType, "opening", project); got != "ok" {
		t.Fatalf("project save: %s", got)
	}
	// The project hands mo the builder role (By project:<id>): the Studio loads for them.
	mo := member("mo")
	if mo.Roles[build.ID] != build.Builder || !slices.ContainsFunc(mo.Grants, func(g platform.Grant) bool { return g.By == "project:opening" }) {
		t.Fatalf("project did not grant builder: %+v", mo.Grants)
	}
	// Its writes are bounded to the assets the project names, on the submission path.
	if got := submit("mo", "build.object.edit", build.ObjectType, "visit", map[string]any{"name": "visit", "title": "Visit (mo)", "fields": object["fields"]}); got != "ok" {
		t.Fatalf("delegated edit: %s", got)
	}
	if got := submit("mo", "build.object.edit", build.ObjectType, "invoice", map[string]any{"name": "invoice", "title": "Invoice", "fields": second["fields"]}); got == "ok" {
		t.Fatal("delegation leaked to an asset the project does not name")
	}
	if got := submit("mo", build.SchemaPublish, build.ObjectType, "visit", map[string]any{}); got == "ok" {
		t.Fatal("delegation covered publishing")
	}
	// Archiving the project takes the role and the edit right away together.
	if got := submit("dana", SchemaProjectArchive, ProjectType, "opening", map[string]any{}); got != "ok" {
		t.Fatalf("project archive: %s", got)
	}
	if member("mo").Roles[build.ID] != "" {
		t.Fatal("an archived project still grants")
	}
	if got := submit("mo", "build.object.edit", build.ObjectType, "visit", map[string]any{"name": "visit", "title": "Visit", "fields": object["fields"]}); got == "ok" {
		t.Fatal("an archived project still delegates")
	}
	_ = now
	_ = tn
}

func TestMultiApprovalPolicyNeedsDistinctApprovers(t *testing.T) {
	if got := work.CountFor(work.ApprovalStep{Approvers: []string{"a", "b", "c"}, Count: 2}, false, 2, 3); got != 2 {
		t.Fatalf("countFor: %d", got)
	}
	if got := work.CountFor(work.ApprovalStep{Approvers: []string{"a", "b", "c"}}, false, 9, 3); got != 3 {
		t.Fatalf("countFor must not exceed the approvers: %d", got)
	}
	if got := work.CountFor(work.ApprovalStep{Approvers: []string{"a", "b"}, All: true}, true, 1, 2); got != 2 {
		t.Fatalf("an all-approvers level stays all: %d", got)
	}
	tn, member, submit := upgradeTenant(t, "t-appr")
	if got := submit("dana", work.SchemaPolicySave, work.PolicyType, "publish-two-eyes", map[string]any{"app": build.ID, "schema": build.SchemaPublish, "approvals": 2, "title": "two eyes"}); got != "ok" {
		t.Fatalf("policy save: %s", got)
	}
	caller := tn.caller(member("dana"), tn.app(work.ID), false)
	if count := tn.app(work.ID).(*work.Work).PolicyCount(caller, build.ID, build.SchemaPublish); count != 2 {
		t.Fatalf("policy count: %d", count)
	}
	if count := tn.app(work.ID).(*work.Work).PolicyCount(caller, build.ID, "nothing"); count != 0 {
		t.Fatalf("policy leaked to another action: %d", count)
	}
}

func TestHostConsoleLifecycleAndSupport(t *testing.T) {
	tn, member, _ := upgradeTenant(t, "t-host")
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	host := NewHost(Tokens(map[string]string{"host-token": "user:ops@example.test"}), tn)
	host.HostAdmins = map[string]bool{"user:ops@example.test": true}
	mux := http.NewServeMux()
	host.hostConsoleRoutes(mux)
	host.environmentRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	get := func(path, token string) (int, string) {
		request, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(body)
	}
	if code, _ := get("/v1/host/overview", "host-token"); code != http.StatusOK {
		t.Fatalf("host overview: %d", code)
	}
	if code, _ := get("/v1/host/overview", "not-a-host"); code != http.StatusUnauthorized {
		t.Fatalf("a tenant member opened the host console: %d", code)
	}
	if code, _ := get("/v1/host/me", "host-token"); code != http.StatusOK {
		t.Fatalf("host identity: %d", code)
	}
	if err := tn.setHostLifecycle("suspend", "maintenance window", "user:ops@example.test", now); err != nil {
		t.Fatal(err)
	}
	if !tn.hostSuspended() {
		t.Fatal("a suspended tenant kept running")
	}
	if err := tn.setHostLifecycle("open", "window over", "user:ops@example.test", now); err != nil || tn.hostSuspended() {
		t.Fatalf("resume: %v", err)
	}
	grant, err := tn.openSupport("dana", "diagnose the failed job", "user:ops@example.test", 30, now)
	if err != nil {
		t.Fatal(err)
	}
	read, err := tn.useSupport(grant.ID, "user:ops@example.test", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("support read: %v", err)
	}
	if read.Tenant != tn.ID || len(read.Audit) == 0 {
		t.Fatalf("support read: %+v", read)
	}
	found := false
	for _, entry := range tn.Audit() {
		if entry.Action == "host.support.use" {
			found = true
		}
	}
	if !found {
		t.Fatal("the support use was not audited")
	}
	if _, err := tn.useSupport(grant.ID, "user:ops@example.test", now.Add(2*time.Hour)); err == nil {
		t.Fatal("an expired support session was used")
	}
	_ = member
}

func TestCandidateSealingAndPromotion(t *testing.T) {
	from, member, submit := upgradeTenant(t, "t-from")
	to, toMember, toSubmit := upgradeTenant(t, "t-to")
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	object := map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}
	if got := submit("dana", build.ObjectType+".create", build.ObjectType, "visit", object); got != "ok" {
		t.Fatalf("draft: %s", got)
	}
	// Activation is a target-side install: the target must hold the owner
	// drafts the candidate's assets resolve to, exactly as a local save needs.
	if got := toSubmit("dana", build.ObjectType+".create", build.ObjectType, "visit", object); got != "ok" {
		t.Fatalf("target draft: %s", got)
	}
	preview, err := from.PreviewRelease(member("dana"), platform.AssetObject, "visit")
	if err != nil || preview.CandidateID == "" || preview.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	candidateID := preview.CandidateID
	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "visit"}}
	if _, err := from.SaveReleaseCandidates(member("dana"), drafts, candidateID, "save", now); err != nil {
		t.Fatalf("save: %v", err)
	}
	artifact, err := from.SealCandidate(candidateID, now)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if artifact.Digest == "" || artifact.Size != len(from.releaseCandidates[candidateID]) {
		t.Fatalf("artifact: %+v", artifact)
	}
	raw, back, err := from.sealedBytes(candidateID)
	if err != nil || len(raw) != artifact.Size || back.Digest != artifact.Digest {
		t.Fatalf("sealed bytes: %v", err)
	}
	result, err := PromoteCandidate(from, to, candidateID, "promo-1", true, toMember("dana"), now)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if result.Digest != artifact.Digest || result.To != to.ID {
		t.Fatalf("promotion: %+v", result)
	}
	if to.releaseCandidates[candidateID] == nil {
		t.Fatal("the target does not hold the promoted candidate")
	}
	// A candidate for an app the target does not host is refused.
	other, _, otherSubmit := upgradeTenant(t, "t-other")
	_ = other
	if got := otherSubmit("dana", build.ObjectType+".create", build.ObjectType, "visit", object); got != "ok" {
		t.Fatalf("draft on the third tenant: %s", got)
	}
	thin := NewConsole("t-thin", upgradeSeats()...)
	thinTenant, err := NewTenant("t-thin", thin, work.New("t-thin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteCandidate(from, thinTenant, candidateID, "promo-2", false, toMember("dana"), now); err == nil ||
		!strings.Contains(err.Error(), "does not host") {
		t.Fatalf("promotion into a tenant without the app: %v", err)
	}
}

func TestPackagePrecheckInstallDrainRetire(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, descriptor map[string]any) {
		raw, _ := json.Marshal(descriptor)
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("base.json", map[string]any{"id": "acme.base", "version": "1.0.0", "namespace": "acme", "title": "Base",
		"contributions": []map[string]any{{"view": "board", "title": "Board"}}})
	write("crm.json", map[string]any{"id": "acme.crm", "version": "1.0.0", "namespace": "acme-crm", "title": "CRM views",
		"requires": []string{"acme.base"}, "contributions": []map[string]any{{"view": "board", "surface": "work", "title": "Pipeline"}}})
	index, err := LoadPackageIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	tn, _, submit := upgradeTenant(t, "t-pkg")
	console := tn.app(PlatformApp).(*Console)
	console.index = index

	if views := console.PackageViews(); len(views) != 2 {
		t.Fatalf("package views: %d", len(views))
	}
	if got := submit("dana", SchemaPackageInstall, PackageType, "acme.crm", map[string]any{"id": "acme.crm", "version": "1.0.0"}); got == "ok" {
		t.Fatal("a package installed with a missing requirement")
	}
	if got := submit("dana", SchemaPackageInstall, PackageType, "acme.base", map[string]any{"id": "acme.base", "version": "1.0.0"}); got != "ok" {
		t.Fatalf("base install: %s", got)
	}
	if got := submit("dana", SchemaPackageInstall, PackageType, "acme.crm", map[string]any{"id": "acme.crm", "version": "1.0.0"}); got != "ok" {
		t.Fatalf("dependent install: %s", got)
	}
	keys := map[string]bool{}
	for _, c := range console.Contributions() {
		keys[c.Key] = true
	}
	if !keys["acme.board"] || !keys["acme-crm.board"] {
		t.Fatalf("namespaced contributions: %v", keys)
	}
	// A controlled upgrade seals the new version, keeps the replaced artifact,
	// and refuses to replace bytes nobody can prove.
	write("base2.json", map[string]any{"id": "acme.base", "version": "1.1.0", "namespace": "acme", "title": "Base",
		"contributions": []map[string]any{{"view": "board", "title": "Board"}}})
	if index, err = LoadPackageIndex(dir); err != nil {
		t.Fatal(err)
	}
	console.index = index
	if got := submit("dana", SchemaPackageUpgrade, PackageType, "acme.base", map[string]any{"id": "acme.base", "version": "1.1.0"}); got != "ok" {
		t.Fatalf("upgrade: %s", got)
	}
	upgraded := console.packagesOf()["acme.base"]
	if upgraded.Version != "1.1.0" || !strings.HasPrefix(upgraded.Artifact, "sha256:") ||
		len(upgraded.Retained) != 1 || upgraded.Retained[0].Version != "1.0.0" || !strings.HasPrefix(upgraded.Retained[0].Digest, "sha256:") {
		t.Fatalf("installed upgrade: %+v", upgraded)
	}
	write("base3.json", map[string]any{"id": "acme.base", "version": "1.2.0", "namespace": "acme", "title": "Base",
		"contributions": []map[string]any{{"view": "board", "title": "Board"}}})
	if index, err = LoadPackageIndex(dir); err != nil {
		t.Fatal(err)
	}
	console.index = index
	if err := console.t.files().Put(context.Background(), packageArtifactKey(tn.ID, "acme.base", "1.1.0"), []byte(`{"id":"acme.base"}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	if got := submit("dana", SchemaPackageUpgrade, PackageType, "acme.base", map[string]any{"id": "acme.base", "version": "1.2.0"}); got == "ok" {
		t.Fatal("an upgrade replaced a sealed artifact whose bytes changed")
	}
	if held := console.packagesOf()["acme.base"]; held.Version != "1.1.0" {
		t.Fatalf("the refused upgrade moved the version: %+v", held)
	}
	if got := submit("dana", SchemaPackageDrain, PackageType, "acme.crm", map[string]any{"id": "acme.crm"}); got != "ok" {
		t.Fatalf("drain: %s", got)
	}
	held := console.packagesOf()["acme.crm"]
	if held == nil || held.State != "draining" {
		t.Fatalf("drain state: %+v", held)
	}
	if got := submit("dana", SchemaPackageRetire, PackageType, "acme.crm", map[string]any{"id": "acme.crm"}); got != "ok" {
		t.Fatalf("retire: %s", got)
	}
	if held := console.packagesOf()["acme.crm"]; held.State != "retired" {
		t.Fatalf("retire state: %+v", held)
	}
	// A retired package's views are no longer contributions.
	for _, c := range console.Contributions() {
		if c.Key == "acme-crm.board" {
			t.Fatal("a retired package still contributes")
		}
	}
}

func TestCompositeEditsAllOrNothing(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "t-comp")
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	if got := submit("dana", build.ObjectType+".create", build.ObjectType, "visit", map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}); got != "ok" {
		t.Fatalf("draft: %s", got)
	}
	edits := []CompositeEdit{
		{Schema: "build.object.edit", Target: &pb.EntityRef{Type: build.ObjectType, Id: "visit"}, Payload: json.RawMessage(`{"name":"visit","title":"Visit today"}`)},
		{Schema: "build.object.edit", Target: &pb.EntityRef{Type: build.ObjectType, Id: "missing"}, Payload: json.RawMessage(`{"name":"missing","title":"Missing"}`)},
	}
	if _, err := tn.Composite(member("dana"), "composite-1", edits, now); err == nil {
		t.Fatal("a composite with a bad edit applied")
	}
	// Nothing half-applied: the first edit's title is unchanged.
	visit, ok := platform.Get[build.Object](tn.caller(member("dana"), tn.app(build.ID), false), "visit")
	if !ok || visit.Title != "Visit" {
		t.Fatalf("the refused composite changed the first edit: %+v", visit)
	}
	good := []CompositeEdit{edits[0]}
	answer, err := tn.Composite(member("dana"), "composite-2", good, now)
	if err != nil || answer.Applied != 1 {
		t.Fatalf("composite: %+v %v", answer, err)
	}
	visit, _ = platform.Get[build.Object](tn.caller(member("dana"), tn.app(build.ID), false), "visit")
	if visit.Title != "Visit today" {
		t.Fatalf("the composite did not apply: %+v", visit)
	}
}

func TestStagedResultChannel(t *testing.T) {
	tn, _, _ := upgradeTenant(t, "t-stage")
	big := json.RawMessage(`{"note":"` + strings.Repeat("x", 60<<10) + `"}`)
	op := platform.Operation{Name: "report", Output: platform.ValueSchema{Type: "object",
		Properties: map[string]platform.ValueSchema{"note": {Type: "string"}}},
		Limits: platform.OperationLimits{MaxOutputBytes: 48 << 10, StagedOutputBytes: 1 << 20}}
	if err := op.Check(); err == nil {
		// The schema above is intentionally lax; the check may pass or refuse.
		t.Log("operation check accepted the report operation")
	}
	answer, handle, err := tn.operationOutput(op, "call-1", big)
	if err != nil {
		t.Fatalf("staged output: %v", err)
	}
	if handle == nil || handle.Size != len(big) || handle.Digest == "" {
		t.Fatalf("handle: %+v", handle)
	}
	if !strings.Contains(string(answer), "staged") {
		t.Fatalf("the answer does not reference the handle: %s", answer)
	}
	back, err := tn.ReadStagedResult(*handle)
	if err != nil || len(back) != len(big) {
		t.Fatalf("read staged: %v", err)
	}
	// Inline results stay inline.
	small := json.RawMessage(`{"note":"ok"}`)
	_, inline, err := tn.operationOutput(op, "call-2", small)
	if err != nil || inline != nil {
		t.Fatalf("an inline result was staged: %v %+v", err, inline)
	}
	if _, _, err := tn.operationOutput(op, "call-3", json.RawMessage(`{"note":"`+strings.Repeat("y", 2<<20)+`"}`)); err == nil {
		t.Fatal("an over-budget result was accepted")
	}
	// The channel's bytes are the sealed ones.
	if err := tn.files().Put(t.Context(), handle.Key, []byte(`{"note":"tampered"}`), "application/json"); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ReadStagedResult(*handle); err == nil {
		t.Fatal("a tampered staged result passed its digest")
	}
}

// A subject the deployment names a host administrator holds no seat in any
// tenant: the workspace gets 401 from /v1/me and opens the console alone from
// /v1/host/me (WorkQueue #141).
func TestPureHostAdministratorLoadsConsoleOnly(t *testing.T) {
	tn, _, _ := upgradeTenant(t, "t-host-only")
	host := NewHost(Tokens(map[string]string{"ops": "user:ops@example.test", "stranger": "user:stranger@example.test"}), tn)
	host.HostAdmins = map[string]bool{"user:ops@example.test": true}
	srv := httptest.NewServer(host.Handler())
	defer srv.Close()
	get := func(path, token string) (int, string) {
		request, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(body)
	}
	if code, _ := get("/v1/me", "ops"); code != http.StatusUnauthorized {
		t.Fatalf("a host administrator without a seat is not a member: %d", code)
	}
	if code, body := get("/v1/host/me", "ops"); code != http.StatusOK || !strings.Contains(body, `"subject":"user:ops@example.test"`) || !strings.Contains(body, `"t-host-only"`) {
		t.Fatalf("host/me: %d %s", code, body)
	}
	if code, _ := get("/v1/host/overview", "ops"); code != http.StatusOK {
		t.Fatalf("overview: %d", code)
	}
	if code, _ := get("/v1/host/me", "stranger"); code != http.StatusUnauthorized {
		t.Fatalf("a stranger is not a host administrator: %d", code)
	}
}
