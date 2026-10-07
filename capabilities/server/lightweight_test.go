package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"platformserver/idp"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// TestLocalFilesKeepsTheSameContract: the lightweight profile's bytes are the
// same bytes under the same content addresses as the S3 store, and a key that
// would leave the directory is refused rather than cleaned up (ADR-0049 D4).
func TestLocalFilesKeepsTheSameContract(t *testing.T) {
	ctx := context.Background()
	local, err := NewLocalFiles(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]FileStore{"memory": &memoryFiles{}, "local": local} {
		t.Run(name, func(t *testing.T) {
			key := "t-1/9f2c0b" // what the host writes: <tenant>/<hash>
			if store.Exists(ctx, key) {
				t.Fatal("a fresh store already holds the key")
			}
			if _, _, err := store.Get(ctx, key); err == nil {
				t.Fatal("a missing file read back")
			}
			data := []byte("the bytes of a picture")
			if err := store.Put(ctx, key, data, "image/png"); err != nil {
				t.Fatal(err)
			}
			if !store.Exists(ctx, key) {
				t.Fatal("the bytes are not stored")
			}
			body, size, err := store.Get(ctx, key)
			if err != nil || size != int64(len(data)) {
				t.Fatalf("get: %d %v", size, err)
			}
			got, err := io.ReadAll(body)
			body.Close()
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("the bytes read back as %q, %v", got, err)
			}
			// The same bytes under the same key are the same file: writing
			// them again is not an error (content addressing).
			if err := store.Put(ctx, key, data, "image/png"); err != nil {
				t.Fatalf("rewriting the same bytes: %v", err)
			}
			if err := store.Delete(ctx, key); err != nil {
				t.Fatal(err)
			}
			if store.Exists(ctx, key) {
				t.Fatal("the deleted file is still there")
			}
			if err := store.Delete(ctx, key); err != nil {
				t.Fatalf("deleting what is gone is not an error: %v", err)
			}
		})
	}
	for _, key := range []string{"", "/etc/passwd", "t-1/", "t-1/../t-2/x", "t-1/./x", "t-1//x"} {
		if err := local.Put(ctx, key, []byte("x"), ""); err == nil {
			t.Errorf("local files accepted the key %q", key)
		}
		if local.Exists(ctx, key) {
			t.Errorf("the key %q is reported held", key)
		}
	}
}

// TestLightweightProfileIsDeclaredNotInferred: a host that names a profile gets
// that profile's storage or a refusal, never the other's (ADR-0049 D1).
func TestLightweightProfileIsDeclaredNotInferred(t *testing.T) {
	for _, c := range []struct {
		name string
		d    Deployment
		want string
	}{
		{"a host that names nothing is a delivery host", Deployment{}, ""},
		{"a delivery host keeps its database", Deployment{Database: "postgres://host/journal", Project: true}, ""},
		{"lightweight needs its data directory", Deployment{Profile: "lightweight"}, "-data"},
		{"delivery refuses -data", Deployment{Data: "/var/lib/platform"}, "-data"},
		{"delivery refuses -idp-key", Deployment{IDPKey: "/etc/platform/idp.key"}, "-idp-key"},
		{"lightweight refuses -database", Deployment{Profile: "lightweight", Data: "/var/lib/platform", Database: "postgres://host/journal"}, "-database"},
		{"lightweight refuses -files", Deployment{Profile: "lightweight", Data: "/var/lib/platform", Files: "http://rustfs:9000/files"}, "-files"},
		{"lightweight refuses -oidc-issuer", Deployment{Profile: "lightweight", Data: "/var/lib/platform", Issuer: "https://id.example/"}, "-oidc-issuer"},
		{"lightweight refuses -oidc-keys", Deployment{Profile: "lightweight", Data: "/var/lib/platform", Keys: "https://id.example/jwks"}, "-oidc-keys"},
		{"lightweight refuses -project", Deployment{Profile: "lightweight", Data: "/var/lib/platform", Project: true}, "-project"},
		{"an unknown profile is refused", Deployment{Profile: "edge"}, "unknown -profile"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.d.validate()
			if c.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if c.d.Profile != "delivery" {
					t.Fatalf("the profile settled as %q", c.d.Profile)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the refusal does not name %s: %v", c.want, err)
			}
		})
	}
}

// TestLightweightProfileServesSignedSeats: the tokens /v1/sign-in serves are
// signed by the host's key, the development tokens a delivery development host
// accepts are not, and /v1/me answers the same member either way (ADR-0049 D3).
func TestLightweightProfileServesSignedSeats(t *testing.T) {
	dir := t.TempDir()
	d := &Deployment{Profile: "lightweight", Data: dir, TokenTTL: time.Hour}
	if err := d.validate(); err != nil {
		t.Fatal(err)
	}
	if err := d.makeKey(); err != nil {
		t.Fatal(err)
	}
	if err := d.makeKey(); err == nil {
		t.Fatal("a second start replaced the signing key")
	}
	journal, _, signer, err := d.lightweightState()
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	tenant, err := NewTenant("t-lite", NewConsole("t-lite", Seat{Subjects: []string{"ana"},
		Member: platform.Member{ID: "ana", Roles: map[string]string{PlatformApp: Admin}}}))
	if err != nil {
		t.Fatal(err)
	}
	host := NewHost(Tokens(map[string]string{"ana": "ana"}), tenant) // as a delivery development host starts
	host.Development = true
	host.SignWith(signer, time.Hour) // what the lightweight deployment does (ADR-0049 D3)
	call := func(path, token string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		host.Handler().ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if code, body := call("/v1/me", "ana"); code != http.StatusUnauthorized {
		t.Fatalf("a lightweight host took the development token: %d %s", code, body)
	}
	code, body := call("/v1/sign-in", "")
	if code != http.StatusOK {
		t.Fatalf("sign-in: %d %s", code, body)
	}
	var signIn struct {
		Identities []Identity `json:"identities"`
	}
	if err := json.Unmarshal([]byte(body), &signIn); err != nil {
		t.Fatal(err)
	}
	if len(signIn.Identities) != 1 || signIn.Identities[0].Member != "ana" {
		t.Fatalf("the served seats: %+v", signIn.Identities)
	}
	token := signIn.Identities[0].Token
	if token == "ana" {
		t.Fatal("the host served the unsigned development token")
	}
	if subject, ok := signer.Verify(token); !ok || subject != "ana" {
		t.Fatalf("the served token reads as %q, %v", subject, ok)
	}
	if code, body := call("/v1/me", token); code != http.StatusOK || !strings.Contains(body, `"principalId":"ana"`) {
		t.Fatalf("a signed seat at /v1/me: %d %s", code, body)
	}
	foreignKey, err := idp.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := idp.NewLocal(foreignKey)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := foreign.Mint("ana", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := call("/v1/me", elsewhere); code != http.StatusUnauthorized {
		t.Fatalf("another host's token: %d", code)
	}
}

// TestLightweightProfileRestartsAndContinues is the M4 walk on the lightweight
// host's own storage: work, stop, start again from -data, and the entries, the
// committed answer and the file bytes are still there (ADR-0049 D6, §3.5).
func TestLightweightProfileRestartsAndContinues(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC()
	dir := t.TempDir()
	compose := func() *Tenant {
		tenant, err := NewTenant("t-lite", NewConsole("t-lite", Seat{Subjects: []string{"admin"},
			Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	start := func() (Journals, FileStore, *idp.Local, *Tenant, int64) {
		d := &Deployment{Profile: "lightweight", Data: dir, SnapshotEvery: 10000, TokenTTL: time.Hour}
		if err := d.validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(d.idpKeyPath()); os.IsNotExist(err) { // the first start makes it; later starts keep it
			if err := d.makeKey(); err != nil {
				t.Fatal(err)
			}
		}
		journal, files, signer, err := d.lightweightState()
		if err != nil {
			t.Fatal(err)
		}
		tenant := compose()
		tenant.Files = files
		restored := map[string]int64{}
		fresh := map[string]bool{}
		if err := d.restoreTenants(ctx, journal, CodeOf(tenant), []*Tenant{tenant}, restored, fresh); err != nil {
			t.Fatal(err)
		}
		return journal, files, signer, tenant, restored["t-lite"]
	}
	journal, files, signer, live, restoredAt := start()
	if live.quarantined() {
		t.Fatalf("the tenant did not start: %s", live.fault.Load().Reason)
	}
	submit := func(tenant *Tenant, key, id, subject string, when time.Time) (*pb.ChangeRecord, *kernel.Error) {
		m, _ := tenant.Member("admin")
		return tenant.Submit(m, &pb.Submission{TenantId: "t-lite", PrincipalId: m.ID, Authority: PlatformApp,
			IdempotencyKey: key, Target: &pb.EntityRef{Type: MemberType, Id: id},
			Schema: &pb.SchemaRef{Name: SchemaAdd, Version: 1}, Payload: []byte(fmt.Sprintf(`{"subject":%q}`, subject))}, when)
	}
	first, failure := submit(live, "k1", "m-ana", "user:ana@example.com", at)
	if failure != nil {
		t.Fatal(failure)
	}
	if _, known := live.app(PlatformApp).(*Console).Member("user:ana@example.com"); !known {
		t.Fatal("the first decision was not applied")
	}
	if err := files.Put(ctx, "t-lite/9f2c0b", []byte("a picture"), "image/png"); err != nil {
		t.Fatal(err)
	}
	// What the host saves at this point, and one more decision after it.
	state, seq, err := live.Snapshot(func() int64 { return journal.Position("t-lite") })
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.SaveSnapshot(ctx, "t-lite", seq, CodeOf(live), state); err != nil {
		t.Fatal(err)
	}
	if _, failure := submit(live, "k2", "m-bo", "user:bo@example.com", at.Add(time.Second)); failure != nil {
		t.Fatal(failure)
	}
	if restoredAt != 0 {
		t.Fatalf("a fresh tenant restored from %d", restoredAt)
	}
	before, err := signer.Mint("admin", time.Hour, at)
	if err != nil {
		t.Fatal(err)
	}
	journal.Close()

	// The host starts again on the same directory.
	journal2, files2, idp2, again, restoredAt := start()
	defer journal2.Close()
	if again.quarantined() {
		t.Fatalf("the tenant did not restart: %s", again.fault.Load().Reason)
	}
	if restoredAt != seq {
		t.Fatalf("the restart took the snapshot at %d, not %d", restoredAt, seq)
	}
	for _, subject := range []string{"user:ana@example.com", "user:bo@example.com"} {
		if _, known := again.app(PlatformApp).(*Console).Member(subject); !known {
			t.Fatalf("%s is missing after the restart", subject)
		}
	}
	// The committed answer is still the committed answer, and work continues.
	if retry, failure := submit(again, "k1", "m-ana", "user:ana@example.com", at); failure != nil {
		t.Fatal(failure)
	} else if retry.GetChangeId() != first.GetChangeId() {
		t.Fatalf("the retried decision is %s, not %s", retry.GetChangeId(), first.GetChangeId())
	}
	if _, failure := submit(again, "k3", "m-cy", "user:cy@example.com", at.Add(2*time.Second)); failure != nil {
		t.Fatal(failure)
	}
	if journal2.Position("t-lite") != seq+2 {
		t.Fatalf("the restarted journal holds %d entries after the snapshot at %d", journal2.Position("t-lite"), seq)
	}
	// The bytes are still there, and so is the key that signed the token.
	body, size, err := files2.Get(ctx, "t-lite/9f2c0b")
	if err != nil || size != int64(len("a picture")) {
		t.Fatalf("the file bytes after the restart: %d %v", size, err)
	}
	got, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(got) != "a picture" {
		t.Fatalf("the file bytes read back as %q, %v", got, err)
	}
	if subject, ok := idp2.Verify(before); !ok || subject != "admin" {
		t.Fatal("a token did not survive the restart's key file")
	}
}
