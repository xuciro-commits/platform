package platformserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/enterprise"
	"platformserver/idp"
	"platformserver/platform"
)

func TestOIDCRequiresMultipleFactors(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	keys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: public, KeyID: "review", Algorithm: "EdDSA", Use: "sig"}}})
	}))
	defer keys.Close()
	const issuer = "http://review.test/"
	auth, attest := idp.OIDCProvider(issuer, keys.URL)
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: private}, (&jose.SignerOptions{}).WithHeader("kid", "review"))
	for _, tc := range []struct {
		methods []string
		want    bool
	}{
		{nil, false}, {[]string{"user"}, false}, {[]string{"otp"}, false}, {[]string{"hwk", "user"}, false},
		{[]string{"pwd", "user"}, false}, {[]string{"sms", "otp"}, false}, {[]string{"mfa"}, true},
		{[]string{"pwd", "otp"}, true}, {[]string{"pwd", "hwk", "user"}, true},
	} {
		t.Run(strings.Join(tc.methods, "+"), func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"iss": issuer, "sub": "u", "exp": time.Now().Add(time.Minute).Unix(), "typ": "Bearer", "email": "u@example.test", "email_verified": true, "amr": tc.methods})
			signed, _ := signer.Sign(body)
			token, _ := signed.CompactSerialize()
			if _, ok := auth(token); !ok {
				t.Fatal("signed user token refused")
			}
			if got := attest(token); got != tc.want {
				t.Fatalf("amr %v: second factor=%v, want %v", tc.methods, got, tc.want)
			}
		})
	}
	if attest("invalid") {
		t.Fatal("unverified token attested")
	}
}

func TestProjectDelegationWithIndependentReadRole(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "project-mixed-grant")
	project := map[string]any{"name": "limited", "title": "Limited", "members": []map[string]any{{"member": "mo", "role": ProjectEditorRole}}, "assets": []map[string]any{{"kind": "object", "name": "allowed"}}}
	if got := submit("dana", SchemaProjectSave, ProjectType, "limited", project); got != "ok" {
		t.Fatal(got)
	}
	grant, _ := json.Marshal(map[string]string{"app": build.ID, "role": build.User})
	if _, err := tn.Submit(member("dana"), &pb.Submission{TenantId: tn.ID, PrincipalId: "dana", Authority: PlatformApp, IdempotencyKey: "grant-reader", Target: &pb.EntityRef{Type: MemberType, Id: "mo"}, Schema: &pb.SchemaRef{Name: SchemaGrant, Version: 1}, Payload: grant}, time.Now()); err != nil {
		t.Fatal(err)
	}
	object := map[string]any{"name": "outside", "title": "Outside", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}
	if got := submit("mo", "build.object.create", build.ObjectType, "outside", object); got != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("project builder + user created outside scope: %s", got)
	}
	object["name"] = "allowed"
	if got := submit("mo", "build.object.create", build.ObjectType, "allowed", object); got != "ok" {
		t.Fatalf("project asset refused: %s", got)
	}
}

func TestAcceptedInvitationIncludesMailIntent(t *testing.T) {
	_, addr := newMailbox(t)
	compose := func() *Tenant {
		tn, err := NewTenant("invite-review", NewConsole("invite-review", Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	admin, _ := tn.Member("admin")
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append unavailable")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	endpoint, _ := json.Marshal(map[string]any{"kind": "email", "url": "smtp://" + addr, "from": "host@example.test", "notifications": []string{PlatformApp}, "allowPrivate": true})
	if _, err := tn.Submit(admin, &pb.Submission{TenantId: tn.ID, PrincipalId: "admin", Authority: PlatformApp, IdempotencyKey: "mail-endpoint", Target: &pb.EntityRef{Type: EndpointType, Id: "mail"}, Schema: &pb.SchemaRef{Name: SchemaEndpointAdd, Version: 1}, Payload: endpoint}, now); err != nil {
		t.Fatal(err)
	}
	sub := &pb.Submission{TenantId: tn.ID, PrincipalId: "admin", Authority: PlatformApp, IdempotencyKey: "invite", Target: &pb.EntityRef{Type: MemberType, Id: "new"}, Schema: &pb.SchemaRef{Name: SchemaInvite, Version: 1}, Payload: []byte(`{"subject":"user:new@example.test"}`)}
	fail = true
	if _, err := tn.Submit(admin, sub, now); err == nil {
		t.Fatal("failed append accepted invite")
	}
	if _, ok := tn.Member("new"); ok || len(tn.notices) != 0 || len(tn.outbound) != 0 {
		t.Fatal("failed invite leaked state or intent")
	}
	fail = false
	receipt, err := tn.Submit(admin, sub, now)
	if err != nil {
		t.Fatal(err)
	}
	batch, _, errDecode := decodeAcceptedBatch(entries[len(entries)-1].Body)
	if errDecode != nil || batch.Notices == nil || len(batch.Notices.Effects) != 1 {
		t.Fatalf("accepted invitation omitted mail intent: %+v %v", batch.Notices, errDecode)
	}
	if !strings.Contains(string(batch.Notices.Effects[0].Body), "new@example.test") {
		t.Fatalf("wrong invitation recipient: %s", batch.Notices.Effects[0].Body)
	}
	retry, err := tn.Submit(admin, sub, now.Add(time.Minute))
	if err != nil || retry.GetChangeId() != receipt.GetChangeId() || len(entries) != 2 {
		t.Fatal("retry duplicated invitation")
	}
	CheckReplay(t, tn, entries, compose)
}

func TestDomainJoinRequiresMFAAndDoesNotReadmitLeaver(t *testing.T) {
	tn, err := NewTenant("join-review", NewConsole("join-review", Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}, Seat{Subjects: []string{"user:left@allowed.test"}, Member: platform.Member{ID: "left", Roles: map[string]string{}}}))
	if err != nil {
		t.Fatal(err)
	}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	admin, _ := tn.Member("admin")
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	submit := func(schema, typ, id, key string, payload any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(admin, &pb.Submission{TenantId: tn.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			t.Fatal(err)
		}
	}
	submit(SchemaSettingSet, SettingType, PlatformApp+"/"+SettingDomains, "domains", map[string]string{"value": "allowed.test"})
	submit(SchemaSettingSet, SettingType, PlatformApp+"/"+SettingMFA, "mfa-on", map[string]string{"value": "true"})
	h := NewHost(Tokens(map[string]string{"new": "user:new@allowed.test", "left": "user:left@allowed.test"}), tn)
	h.Now = func() time.Time { return now }
	request := func(token string) bool {
		r := httptest.NewRequest("GET", "/v1/me", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		_, _, ok := h.member(r)
		return ok
	}
	if request("new") {
		t.Fatal("joined without MFA")
	}
	if _, ok := tn.Member("new"); ok {
		t.Fatal("unattested request created a seat")
	}
	h.SecondFactor = func(string) bool { return true }
	if !request("new") {
		t.Fatal("attested domain member refused")
	}
	submit(SchemaOffboard, MemberType, "left", "offboard", map[string]any{})
	if request("left") {
		t.Fatal("offboarded identity rejoined through its domain")
	}
	submit(SchemaInvite, MemberType, "returned", "return", map[string]string{"subject": "user:left@allowed.test"})
	if !request("left") {
		t.Fatal("explicit administrative reinvitation was refused")
	}
}

func TestBoundGrantReadsUnionWithinItsStructure(t *testing.T) {
	org := enterprise.New("grant-reads", platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Kind: "site"}, {ID: "other", Kind: "site"}}, Units: []platform.Unit{{ID: "plant", Kind: "plant"}, {ID: "L1", Kind: "line"}, {ID: "L2", Kind: "line"}, {ID: "X", Kind: "line"}}, Edges: []platform.Edge{{Structure: "site", Unit: "L1", Parent: "plant"}, {Structure: "site", Unit: "L2", Parent: "plant"}, {Structure: "other", Unit: "X", Parent: "plant"}}, Memberships: []platform.Membership{{Party: "member:writer", Unit: "plant"}, {Party: "member:reader", Unit: "L1"}}})
	tn, err := NewTenant("grant-reads", NewConsole("grant-reads", Seat{Subjects: []string{"writer"}, Member: platform.Member{ID: "writer", Roles: map[string]string{"stock": "lead"}}}), org, newStock("grant-reads"))
	if err != nil {
		t.Fatal(err)
	}
	writer, _ := tn.Member("writer")
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	create := func(typ, id string, p any) {
		t.Helper()
		raw, _ := json.Marshal(p)
		if _, err := tn.Submit(writer, &pb.Submission{TenantId: tn.ID, PrincipalId: writer.ID, Authority: "stock", IdempotencyKey: id, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".create", Version: 1}, Payload: raw}, now); err != nil {
			t.Fatal(err)
		}
	}
	create("stock.bin", "bin", map[string]any{"code": "bin"})
	for _, line := range []string{"L1", "L2", "X"} {
		create("stock.item", line, map[string]any{"name": line, "line": line, "bin": "bin", "kind": "part"})
	}
	for _, tc := range []struct {
		name   string
		grants []platform.Grant
	}{
		{"structure", []platform.Grant{{App: "stock", Role: "lead", Unit: "plant", Structure: "site"}}},
		{"mixed levels", []platform.Grant{{App: "stock", Role: "lead", Unit: "L1", Structure: "site"}, {App: "stock", Role: "line", Unit: "L2", Structure: "site"}}},
		{"unbound and bound", []platform.Grant{{App: "stock", Role: "lead"}, {App: "stock", Role: "lead", Unit: "L2", Structure: "site"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := tn.Records(platform.Member{ID: "reader", Tenant: tn.ID, Roles: map[string]string{"stock": "lead"}, Grants: tc.grants}, "stock.item", platform.Query{}, now)
			if err != nil || page.Total != 2 {
				t.Fatalf("reads want L1+L2, got %+v %v", page, err)
			}
		})
	}
}

func TestProjectBuilderCannotSaveOrActivateReleases(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "project-release-bound")
	if got := submit("dana", SchemaProjectSave, ProjectType, "limited", map[string]any{"name": "limited", "title": "Limited", "members": []map[string]any{{"member": "mo", "role": ProjectEditorRole}}, "assets": []map[string]any{{"kind": "object", "name": "allowed"}}}); got != "ok" {
		t.Fatal(got)
	}
	for _, run := range []func() (string, error){
		func() (string, error) {
			return tn.SaveReleaseCandidates(member("mo"), nil, "candidate", "save", time.Now())
		},
		func() (string, error) { return tn.ActivateRelease(member("mo"), "candidate", "activate", time.Now()) },
	} {
		if _, err := run(); err == nil || !strings.Contains(err.Error(), "builder or publisher role required") {
			t.Fatalf("project builder passed release authority: %v", err)
		}
	}
}

func TestJournalAcceptedInvitationCrashAfterCommit(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	id := fmt.Sprintf("invite-crash-%d", time.Now().UnixNano())
	defer j.Pool().Exec(ctx, `delete from journal where tenant=$1`, id)
	if _, err := j.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	box, addr := newMailbox(t)
	compose := func() *Tenant {
		tn, err := NewTenant(id, NewConsole(id, Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose()
	admin, _ := live.Member("admin")
	now := Now()
	live.Record = func(e Entry) {
		if err := j.Append(ctx, id, e); err != nil {
			t.Fatal(err)
		}
	}
	endpoint, _ := json.Marshal(map[string]any{"kind": "email", "url": "smtp://" + addr, "from": "host@example.test", "notifications": []string{PlatformApp}, "allowPrivate": true})
	if _, err := live.Submit(admin, &pb.Submission{TenantId: id, PrincipalId: "admin", Authority: PlatformApp, IdempotencyKey: "endpoint", Target: &pb.EntityRef{Type: EndpointType, Id: "mail"}, Schema: &pb.SchemaRef{Name: SchemaEndpointAdd, Version: 1}, Payload: endpoint}, now); err != nil {
		t.Fatal(err)
	}
	sub := &pb.Submission{TenantId: id, PrincipalId: "admin", Authority: PlatformApp, IdempotencyKey: "invite", Target: &pb.EntityRef{Type: MemberType, Id: "new"}, Schema: &pb.SchemaRef{Name: SchemaInvite, Version: 1}, Payload: []byte(`{"subject":"user:new@example.test"}`)}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if _, ok := live.Member("new"); ok {
			t.Fatal("invite exposed before commit")
		}
		if _, err := j.AppendAccepted(ctx, id, e, key, hash); err != nil {
			return nil, err
		}
		return nil, errors.New("lost acknowledgement after commit")
	}
	if _, err := live.Submit(admin, sub, now); err == nil {
		t.Fatal("lost acknowledgement reported success")
	}
	if _, ok := live.Member("new"); ok || len(live.outbound) != 0 {
		t.Fatal("interrupted process applied invite")
	}
	reopened, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 2 {
		t.Fatalf("durable invite: %d %v", len(entries), err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if _, ok := recovered.Member("new"); !ok || len(recovered.outbound) != 1 {
		t.Fatal("recovery omitted member or mail intent")
	}
	box.mu.Lock()
	calls := box.calls
	box.mu.Unlock()
	if calls != 0 {
		t.Fatal("replay sent mail")
	}
	recovered.AcceptResult = func(e Entry, key, hash string) ([]byte, error) { return reopened.AppendAccepted(ctx, id, e, key, hash) }
	if _, err := recovered.Submit(admin, sub, now.Add(time.Minute)); err != nil || reopened.Position(id) != 2 {
		t.Fatalf("retry did not reuse invite: %v", err)
	}
	recovered.Record = func(e Entry) {
		if err := reopened.Append(ctx, id, e); err != nil {
			t.Fatal(err)
		}
	}
	recovered.Dispatch(now.Add(time.Minute))
	recovered.Dispatch(now.Add(2 * time.Minute))
	box.mu.Lock()
	defer box.mu.Unlock()
	if box.calls != 1 || len(box.kept) != 1 {
		t.Fatalf("SMTP delivery repeated or absent: %d %d", box.calls, len(box.kept))
	}
	for key := range box.kept {
		if box.to[key] != "new@example.test" {
			t.Fatalf("recipient: %s", box.to[key])
		}
	}
}

func TestDomainJoinConcurrentRequestsShareOneSeat(t *testing.T) {
	tn, err := NewTenant("join-concurrent", NewConsole("join-concurrent", Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}))
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := tn.Member("admin")
	now := Now()
	if _, err := tn.Submit(admin, &pb.Submission{TenantId: tn.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: "domains", Target: &pb.EntityRef{Type: SettingType, Id: PlatformApp + "/" + SettingDomains}, Schema: &pb.SchemaRef{Name: SchemaSettingSet, Version: 1}, Payload: []byte(`{"value":"allowed.test"}`)}, now); err != nil {
		t.Fatal(err)
	}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	h := NewHost(Tokens(map[string]string{"same": "user:same@allowed.test"}), tn)
	h.Now = func() time.Time { return now }
	start := make(chan struct{})
	answers := make(chan string, 12)
	for range 12 {
		go func() {
			<-start
			r := httptest.NewRequest("GET", "/v1/me", nil)
			r.Header.Set("Authorization", "Bearer same")
			m, _, ok := h.member(r)
			if !ok {
				answers <- "refused"
			} else {
				answers <- m.ID
			}
		}()
	}
	close(start)
	for range 12 {
		if id := <-answers; id != "same" {
			t.Fatalf("concurrent admission: %s", id)
		}
	}
	d := consoleOf(tn)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.members) != 2 || d.subjects["user:same@allowed.test"] != "same" {
		t.Fatal("concurrent join duplicated seat")
	}
}

func TestAcceptedProjectAndPackageState(t *testing.T) {
	for _, kind := range []string{"project", "package"} {
		t.Run(kind, func(t *testing.T) {
			compose := func() *Tenant {
				tn, _, _ := upgradeTenant(t, "accepted-"+kind)
				consoleOf(tn).index = &PackageIndex{Packages: []PackageDescriptor{{ID: "review.base", Version: "1.0.0", Namespace: "review", Title: "Review"}}}
				return tn
			}
			tn := compose()
			admin, _ := tn.Member("dana")
			now := Now()
			var entries []Entry
			fail := true
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
				if fail {
					return nil, errors.New("append unavailable")
				}
				entries = append(entries, e)
				return e.Body, nil
			}
			schema, typ, id, payload := SchemaProjectSave, ProjectType, "limited", map[string]any{"name": "limited", "title": "Limited", "members": []map[string]any{{"member": "mo", "role": ProjectEditorRole}}, "assets": []map[string]any{{"kind": "object", "name": "allowed"}}}
			if kind == "package" {
				schema, typ, id, payload = SchemaPackageInstall, PackageType, "review.base", map[string]any{"id": "review.base", "version": "1.0.0"}
			}
			raw, _ := json.Marshal(payload)
			sub := &pb.Submission{TenantId: tn.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: kind, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
			if _, err := tn.Submit(admin, sub, now); err == nil {
				t.Fatal("failed append accepted state")
			}
			d := consoleOf(tn)
			if len(d.projects) != 0 || len(d.packages) != 0 {
				t.Fatal("failed append leaked state")
			}
			fail = false
			if _, err := tn.Submit(admin, sub, now); err != nil {
				t.Fatal(err)
			}
			if kind == "project" {
				if d.projects[id] == nil {
					t.Fatal("accepted project was not installed")
				}
				mo, _ := tn.Member("mo")
				if !mo.Holds(build.ID, build.Builder) {
					t.Fatal("accepted project did not grant builder")
				}
			} else if d.packages[id] == nil || d.packages[id].State != "active" {
				t.Fatal("accepted package was not installed")
			}
			if _, err := tn.Submit(admin, sub, now.Add(time.Minute)); err != nil || len(entries) != 1 {
				t.Fatal("accepted retry duplicated state")
			}
			CheckReplay(t, tn, entries, compose)
		})
	}
}

func TestDeploymentPackageIndexSurvivesTenantRebuild(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(`{"id":"review.base","version":"1.0.0","namespace":"review","title":"Review"}`), 0600); err != nil {
		t.Fatal(err)
	}
	compose := func(id string) (*Tenant, error) { return NewTenant(id, NewConsole(id)) }
	tn, err := compose("original")
	if err != nil {
		t.Fatal(err)
	}
	deployment := Deployment{Packages: dir, Rebuild: compose}
	if err := deployment.preparePackageIndex([]*Tenant{tn}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"created", "recovered"} {
		fresh, err := deployment.rebuildTenant(id)
		if err != nil {
			t.Fatal(err)
		}
		if views := consoleOf(fresh).PackageViews(); len(views) != 1 || views[0].Descriptor.ID != "review.base" {
			t.Fatalf("%s lost package catalogue: %+v", id, views)
		}
	}
	if len(consoleOf(tn).PackageViews()) != 1 {
		t.Fatal("startup omitted package index")
	}
	deployment.Packages = filepath.Join(dir, "missing")
	if err := deployment.preparePackageIndex([]*Tenant{tn}); err == nil {
		t.Fatal("invalid configured index was ignored")
	}
}
