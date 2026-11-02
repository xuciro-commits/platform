package idp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Authenticate func(credential string) (subject string, ok bool)

type Attest func(credential string) bool

type Local struct {
	key []byte
}

const (
	keyPrefix = "platform-idp-key-v1 "
	issuer    = "platform-lightweight"
	idpMinKey = 32
)

func NewLocal(key []byte) (*Local, error) {
	if len(key) < idpMinKey {
		return nil, fmt.Errorf("idp: the signing key is %d bytes; at least %d are required", len(key), idpMinKey)
	}
	return &Local{key: append([]byte{}, key...)}, nil
}

func NewKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("idp: %w", err)
	}
	return key, nil
}

func SaveKey(path string, key []byte) error {
	return writeFileAtomic(path, []byte(keyPrefix+base64.StdEncoding.EncodeToString(key)+"\n"))
}

func LoadKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("idp: no signing key at %s: run the host once with -idp-new-key to make one", path)
	}
	if err != nil {
		return nil, fmt.Errorf("idp: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, keyPrefix) {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(text, keyPrefix)))
		if err != nil {
			return nil, fmt.Errorf("idp: %s is not a key file", path)
		}
		return key, nil
	}
	if len(raw) >= idpMinKey && !strings.ContainsAny(text, " \n\t") {
		return raw, nil
	}
	return nil, fmt.Errorf("idp: %s is not a key file", path)
}

func (p *Local) Mint(subject string, ttl time.Duration, now time.Time) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("idp: a token needs a subject")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("idp: a token needs a life longer than nothing")
	}
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": issuer, "sub": subject,
		"iat": now.UTC().Unix(), "exp": now.UTC().Add(ttl).Unix()})
	signing := p.b64(header) + "." + p.b64(claims)
	return signing + "." + p.b64(p.sign(signing)), nil
}

func (p *Local) Verify(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	var h struct{ Alg, Typ string }
	if json.Unmarshal(header, &h) != nil || h.Alg != "HS256" {
		return "", false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, p.sign(parts[0]+"."+parts[1])) {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims struct {
		Issuer    string `json:"iss"`
		Subject   string `json:"sub"`
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Issuer != issuer || claims.Subject == "" {
		return "", false
	}
	now := time.Now().UTC()
	if claims.ExpiresAt == 0 || now.Unix() >= claims.ExpiresAt {
		return "", false
	}
	if claims.IssuedAt > now.Add(time.Minute).Unix() {
		return "", false
	}
	return claims.Subject, true
}

func (p *Local) Authenticate() Authenticate {
	return func(credential string) (string, bool) { return p.Verify(credential) }
}

func (p *Local) sign(signing string) []byte {
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(signing))
	return mac.Sum(nil)
}

func (p *Local) b64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func (p *Local) Key() []byte { return p.key }

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
