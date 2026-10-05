package platformserver

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

// LocalIdP is the lightweight profile's identity provider: one HMAC-SHA256 key
// in the data directory, tokens the host signs and verifies itself, and the
// same seat directory as ever (ADR-0049 D3). The delivery profile keeps an
// external OpenID provider, and development tokens stay in the development
// host; a lightweight host accepts neither.
type LocalIdP struct {
	key []byte
}

const (
	idpKeyPrefix = "platform-idp-key-v1 "
	idpIssuer    = "platform-lightweight"
	// idpMinKey is the shortest signing key accepted: a key that could be
	// guessed is not a credential, so a short one fails at start-up.
	idpMinKey = 32
)

// NewLocalIdP takes the bytes of the signing key.
func NewLocalIdP(key []byte) (*LocalIdP, error) {
	if len(key) < idpMinKey {
		return nil, fmt.Errorf("idp: the signing key is %d bytes; at least %d are required", len(key), idpMinKey)
	}
	return &LocalIdP{key: append([]byte{}, key...)}, nil
}

// NewIDPKey makes a fresh signing key.
func NewIDPKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("idp: %w", err)
	}
	return key, nil
}

// SaveIDPKey writes a key where only the host's user can read it.
func SaveIDPKey(path string, key []byte) error {
	return writeFileAtomic(path, []byte(idpKeyPrefix+base64.StdEncoding.EncodeToString(key)+"\n"))
}

// LoadIDPKey reads the key file. Its absence is told apart from its being
// unreadable, so a first start says how to make one.
func LoadIDPKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("idp: no signing key at %s: run the host once with -idp-new-key to make one", path)
	}
	if err != nil {
		return nil, fmt.Errorf("idp: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, idpKeyPrefix) {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(text, idpKeyPrefix)))
		if err != nil {
			return nil, fmt.Errorf("idp: %s is not a key file", path)
		}
		return key, nil
	}
	if len(raw) >= idpMinKey && !strings.ContainsAny(text, " \n\t") { // raw bytes, as an operator may have made them
		return raw, nil
	}
	return nil, fmt.Errorf("idp: %s is not a key file", path)
}

// Mint signs a token for a subject: the payload names the subject, when the
// token was made and when it stops being accepted, and the signature is this
// host's key (HS256, the algorithm a JWT header names).
func (p *LocalIdP) Mint(subject string, ttl time.Duration, now time.Time) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("idp: a token needs a subject")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("idp: a token needs a life longer than nothing")
	}
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": idpIssuer, "sub": subject,
		"iat": now.UTC().Unix(), "exp": now.UTC().Add(ttl).Unix()})
	signing := p.b64(header) + "." + p.b64(claims)
	return signing + "." + p.b64(p.sign(signing)), nil
}

// Verify answers the subject of a token this host signed and has not expired.
// The signature is compared in constant time, and a token with another
// algorithm is refused rather than read (an unsigned one is not a credential).
func (p *LocalIdP) Verify(token string) (string, bool) {
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
	if json.Unmarshal(payload, &claims) != nil || claims.Issuer != idpIssuer || claims.Subject == "" {
		return "", false
	}
	now := time.Now().UTC()
	if claims.ExpiresAt == 0 || now.Unix() >= claims.ExpiresAt {
		return "", false
	}
	if claims.IssuedAt > now.Add(time.Minute).Unix() { // a token from the future is not one
		return "", false
	}
	return claims.Subject, true
}

// Authenticate is how the host takes this provider's tokens: the subject, as
// every authenticate function answers (K6, ADR-0010).
func (p *LocalIdP) Authenticate() Authenticate {
	return func(credential string) (string, bool) { return p.Verify(credential) }
}

func (p *LocalIdP) sign(signing string) []byte {
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(signing))
	return mac.Sum(nil)
}

func (p *LocalIdP) b64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }
