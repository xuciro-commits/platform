package idp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLocalIdPSignsAndVerifies: the lightweight host takes only tokens it
// signed, for itself and unexpired (ADR-0049 D3).
func TestLocalIdPSignsAndVerifies(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal([]byte("short key")); err == nil {
		t.Fatal("a guessable key was accepted")
	}
	idp, err := NewLocal(key)
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
	otherKey, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewLocal(otherKey)
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
	if _, ok := idp.Verify(claim(map[string]any{"iss": issuer, "sub": "user:ana",
		"iat": now.Add(-2 * time.Hour).Unix(), "exp": now.Add(-time.Hour).Unix()})); ok {
		t.Fatal("an expired token was accepted")
	}
	if _, ok := idp.Verify(claim(map[string]any{"iss": issuer, "sub": "user:ana",
		"iat": now.Add(5 * time.Minute).Unix(), "exp": now.Add(time.Hour).Unix()})); ok {
		t.Fatal("a token from the future was accepted")
	}
	if _, ok := idp.Verify(claim(map[string]any{"iss": "somewhere-else", "sub": "user:ana",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})); ok {
		t.Fatal("another issuer's token was accepted")
	}
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	body, _ := json.Marshal(map[string]any{"iss": issuer, "sub": "user:ana", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	if _, ok := idp.Verify(idp.b64(header) + "." + idp.b64(body) + "."); ok {
		t.Fatal("an unsigned token was accepted")
	}
	if _, ok := idp.Verify("not a token"); ok {
		t.Fatal("something that is not a token was accepted")
	}
	// The key file is what the host keeps, and its absence says how to make one.
	path := filepath.Join(t.TempDir(), "idp.key")
	if _, err := LoadKey(path); err == nil || !strings.Contains(err.Error(), "-idp-new-key") {
		t.Fatalf("a missing key file: %v", err)
	}
	if err := SaveKey(path, key); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the key file is readable by others: %v", info.Mode())
	}
	loaded, err := LoadKey(path)
	if err != nil || !bytes.Equal(loaded, key) {
		t.Fatalf("the key did not survive its file: %v", err)
	}
	reloaded, err := NewLocal(loaded)
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
	if _, err := LoadKey(notAKey); err == nil {
		t.Fatal("something that is not a key file was read as one")
	}
}
