package platformserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// TestOIDC signs tokens the way Rauthy does (EdDSA, typ Bearer; client
// credentials without sub) and checks which ones yield a subject.
func TestOIDC(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	keys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: public, KeyID: "k1", Algorithm: "EdDSA", Use: "sig"}}})
	}))
	defer keys.Close()
	const issuer = "http://idp.test/auth/v1/"
	sign := func(key ed25519.PrivateKey, claims map[string]any) string {
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		base := map[string]any{"iss": issuer, "aud": "some-client", "exp": time.Now().Add(time.Minute).Unix(), "typ": "Bearer"}
		for k, v := range claims {
			base[k] = v
		}
		raw, _ := json.Marshal(base)
		jws, _ := signer.Sign(raw)
		token, _ := jws.CompactSerialize()
		return token
	}
	auth := OIDC(issuer, keys.URL)
	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	for _, c := range []struct {
		name  string
		token string
		want  string
	}{
		{"verified user", sign(private, map[string]any{"sub": "u1", "email": "ana@plant.test", "email_verified": true}), "user:ana@plant.test"},
		{"machine", sign(private, map[string]any{"azp": "gateway"}), "client:gateway"},
		{"unverified email", sign(private, map[string]any{"sub": "u1", "email": "ana@plant.test"}), ""},
		{"id token", sign(private, map[string]any{"sub": "u1", "email": "ana@plant.test", "email_verified": true, "typ": "Id"}), ""},
		{"expired", sign(private, map[string]any{"azp": "gateway", "exp": time.Now().Add(-time.Minute).Unix()}), ""},
		{"other issuer", sign(private, map[string]any{"azp": "gateway", "iss": "http://evil.test/"}), ""},
		{"other key", sign(stranger, map[string]any{"azp": "gateway"}), ""},
		{"garbage", "supervisor", ""},
	} {
		got, ok := auth(c.token)
		if ok != (c.want != "") || got != c.want {
			t.Errorf("%s: got %v %v, want %q", c.name, got, ok, c.want)
		}
	}
}

// TestJournal needs PostgreSQL: PLATFORM_TEST_DATABASE names a disposable
// database (scripts/verify.sh deploy provides one).
func TestJournal(t *testing.T) {
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
	j.pool.Exec(ctx, `delete from journal where tenant = 't-journal'`)
	j.pool.Exec(ctx, `delete from snapshots where tenant = 't-journal'`)
	entry := Entry{Kind: "submission", Principal: json.RawMessage(`{"id":"p1"}`), Body: json.RawMessage(`{"a":1}`), At: Now()}
	if j.Append(ctx, "t-journal", entry) == nil {
		t.Fatal("append before reading must fail")
	}
	if got, _ := j.Entries(ctx, "t-journal", 0); len(got) != 0 {
		t.Fatalf("fresh tenant has %d entries", len(got))
	}
	for range 2 {
		if err := j.Append(ctx, "t-journal", entry); err != nil {
			t.Fatal(err)
		}
	}
	// A second writer that read before the appends cannot interleave.
	other, _ := OpenJournal(ctx, url)
	defer other.Close()
	other.next["t-journal"] = 2
	if other.Append(ctx, "t-journal", entry) == nil {
		t.Fatal("stale writer appended")
	}
	got, err := j.Entries(ctx, "t-journal", 0)
	if err != nil || len(got) != 2 || !got[1].At.Equal(entry.At) || string(got[0].Body) != `{"a": 1}` {
		t.Fatalf("entries %v %v", got, err)
	}
	// Snapshots (ADR-0019 D6): the newest of the same code, then the entries after it.
	for seq := range int64(3) {
		if err := j.SaveSnapshot(ctx, "t-journal", seq+1, "code-a", []byte(fmt.Sprint("state ", seq+1))); err != nil {
			t.Fatal(err)
		}
	}
	if seq, state, ok, err := j.Snapshot(ctx, "t-journal", "code-a"); err != nil || !ok || seq != 3 || string(state) != "state 3" {
		t.Fatalf("snapshot %d %q %v %v", seq, state, ok, err)
	}
	if _, _, ok, _ := j.Snapshot(ctx, "t-journal", "code-b"); ok {
		t.Fatal("a snapshot of other code was offered")
	}
	var kept int
	j.pool.QueryRow(ctx, `select count(*) from snapshots where tenant = 't-journal'`).Scan(&kept)
	if after, _ := j.Entries(ctx, "t-journal", 1); len(after) != 1 || kept != 2 || j.Position("t-journal") != 2 {
		t.Fatalf("after 1: %d entries, %d snapshots kept, position %d", len(after), kept, j.Position("t-journal"))
	}
}
