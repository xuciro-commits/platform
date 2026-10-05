package platformserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestLocalIdPSignsAndVerifies: the lightweight host takes only tokens it
// signed, for itself and unexpired (ADR-0049 D3).
func TestLocalIdPSignsAndVerifies(t *testing.T) {
	key, err := NewIDPKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocalIdP([]byte("short key")); err == nil {
		t.Fatal("a guessable key was accepted")
	}
	idp, err := NewLocalIdP(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token, err := idp.Mint("user:ana@example.com", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if subject, ok := idp.Verify(token); !ok || subject != "user:ana@example.com" {
		t.Fatalf("its own token reads as %q, %v", subject, ok)
	}
	if subject, ok := idp.Authenticate()(token); !ok || subject != "user:ana@example.com" {
		t.Fatalf("the host's authenticate reads it as %q, %v", subject, ok)
	}
	if _, err := idp.Mint("", time.Hour, now); err == nil {
		t.Fatal("a token without a subject was minted")
	}
	if _, err := idp.Mint("user:ana", 0, now); err == nil {
		t.Fatal("a token that expires at once was minted")
	}
	// Another host's key is not this host's key.
	otherKey, err := NewIDPKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewLocalIdP(otherKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := other.Verify(token); ok {
		t.Fatal("another key verified the token")
	}
	if foreign, err := other.Mint("user:ana@example.com", time.Hour, now); err != nil {
		t.Fatal(err)
	} else if _, ok := idp.Verify(foreign); ok {
		t.Fatal("a token of another host was accepted")
	}
	// A changed payload is not the token that was signed.
	parts := strings.Split(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(payload, []byte("user:ana"), []byte("user:bo!"), 1)
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(changed) + "." + parts[2]
	if _, ok := idp.Verify(tampered); ok {
		t.Fatal("a changed payload was accepted")
	}
	claim := func(claims map[string]any) string {
		header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
		body, _ := json.Marshal(claims)
		signing := idp.b64(header) + "." + idp.b64(body)
		return signing + "." + idp.b64(idp.sign(signing))
	}
	if _, ok := idp.Verify(claim(map[string]any{"iss": idpIssuer, "sub": "user:ana",
		"iat": now.Add(-2 * time.Hour).Unix(), "exp": now.Add(-time.Hour).Unix()})); ok {
		t.Fatal("an expired token was accepted")
	}
	if _, ok := idp.Verify(claim(map[string]any{"iss": idpIssuer, "sub": "user:ana",
		"iat": now.Add(5 * time.Minute).Unix(), "exp": now.Add(time.Hour).Unix()})); ok {
		t.Fatal("a token from the future was accepted")
	}
	if _, ok := idp.Verify(claim(map[string]any{"iss": "somewhere-else", "sub": "user:ana",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})); ok {
		t.Fatal("another issuer's token was accepted")
	}
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	body, _ := json.Marshal(map[string]any{"iss": idpIssuer, "sub": "user:ana", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	if _, ok := idp.Verify(idp.b64(header) + "." + idp.b64(body) + "."); ok {
		t.Fatal("an unsigned token was accepted")
	}
	if _, ok := idp.Verify("not a token"); ok {
		t.Fatal("something that is not a token was accepted")
	}
	// The key file is what the host keeps, and its absence says how to make one.
	path := filepath.Join(t.TempDir(), "idp.key")
	if _, err := LoadIDPKey(path); err == nil || !strings.Contains(err.Error(), "-idp-new-key") {
		t.Fatalf("a missing key file: %v", err)
	}
	if err := SaveIDPKey(path, key); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the key file is readable by others: %v", info.Mode())
	}
	loaded, err := LoadIDPKey(path)
	if err != nil || !bytes.Equal(loaded, key) {
		t.Fatalf("the key did not survive its file: %v", err)
	}
	reloaded, err := NewLocalIdP(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if subject, ok := reloaded.Verify(token); !ok || subject != "user:ana@example.com" {
		t.Fatal("a token did not survive the key file")
	}
	notAKey := filepath.Join(t.TempDir(), "not-a-key")
	if err := os.WriteFile(notAKey, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIDPKey(notAKey); err == nil {
		t.Fatal("something that is not a key file was read as one")
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
	journal, _, idp, err := d.lightweightState()
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
	host.SignWith(idp, time.Hour) // what the lightweight deployment does (ADR-0049 D3)
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
	if subject, ok := idp.Verify(token); !ok || subject != "ana" {
		t.Fatalf("the served token reads as %q, %v", subject, ok)
	}
	if code, body := call("/v1/me", token); code != http.StatusOK || !strings.Contains(body, `"principalId":"ana"`) {
		t.Fatalf("a signed seat at /v1/me: %d %s", code, body)
	}
	foreignKey, err := NewIDPKey()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewLocalIdP(foreignKey)
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
	start := func() (Journals, FileStore, *LocalIdP, *Tenant, int64) {
		d := &Deployment{Profile: "lightweight", Data: dir, SnapshotEvery: 10000, TokenTTL: time.Hour}
		if err := d.validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(d.idpKeyPath()); os.IsNotExist(err) { // the first start makes it; later starts keep it
			if err := d.makeKey(); err != nil {
				t.Fatal(err)
			}
		}
		journal, files, idp, err := d.lightweightState()
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
		return journal, files, idp, tenant, restored["t-lite"]
	}
	journal, files, idp, live, restoredAt := start()
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
	before, err := idp.Mint("admin", time.Hour, at)
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
