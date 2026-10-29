package control

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Credentials owns encrypted accepted values and outbound reads. The console's
// original accepted ledger applies Prepare's result; Restore never decrypts.
type Credentials struct {
	mu     sync.RWMutex
	tenant string
	values map[string]platform.CredentialState
}

func NewCredentials(tenant string) *Credentials {
	return &Credentials{tenant: tenant, values: map[string]platform.CredentialState{}}
}
func (d *Credentials) SnapshotState() map[string]platform.CredentialState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return maps.Clone(d.values)
}
func (d *Credentials) Restore(state map[string]platform.CredentialState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.values = state
	if d.values == nil {
		d.values = map[string]platform.CredentialState{}
	}
}

const (
	CredentialType           = "platform.credential"
	SchemaCredentialSave     = "platform.credential.save"
	SchemaCredentialDisable  = "platform.credential.disable"
	credentialEnvelopePrefix = "aes256gcm-v1:"
)

var credentialName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func CredentialActions() []platform.Action {
	fields := []platform.Field{{Name: "id", Type: "string", Required: true}, {Name: "title", Type: "string", Required: true}, {Name: "kind", Type: "string", Required: true}, {Name: "source", Type: "string", Required: true}, {Name: "reference", Type: "string"}, {Name: "bindings", Type: "json", Required: true}, {Name: "expectedVersion", Type: "integer", Required: true}, {Name: "sealed", Type: "string"}}
	return []platform.Action{
		{Schema: SchemaCredentialSave, Target: CredentialType, Capability: "credentials", Title: "Save credential", Description: "Save an authenticated encrypted value or a deployment reference; never submit plaintext.", Roles: []string{"admin"}, Payload: fields},
		{Schema: SchemaCredentialDisable, Target: CredentialType, Capability: "credentials", Title: "Disable credential", Description: "Stop new credential use without deleting encrypted history", Roles: []string{"admin"}, Payload: []platform.Field{{Name: "expectedVersion", Type: "integer", Required: true}}},
	}
}

// credentialCipher is only needed for new writes and actual use, never saved-result recovery.
func credentialCipher() (cipher.AEAD, error) {
	key, err := hex.DecodeString(os.Getenv("PLATFORM_CREDENTIAL_KEY"))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("Managed credentials require PLATFORM_CREDENTIAL_KEY with 32 bytes encoded as hex")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}
func credentialAAD(tenant string, w platform.CredentialWrite) []byte {
	w.Sealed = ""
	return platform.Raw(struct {
		Tenant string                   `json:"tenant"`
		Write  platform.CredentialWrite `json:"write"`
	}{tenant, w})
}
func CredentialOrigin(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if port := u.Port(); scheme == "https" && port == "443" || scheme == "http" && port == "80" {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return scheme + "://" + host
}
func checkCredentialDefinition(w platform.CredentialWrite) error {
	if !credentialName.MatchString(w.ID) || strings.TrimSpace(w.Title) == "" || w.ExpectedVersion < 0 || !slices.Contains([]string{"api-key", "bearer", "password"}, w.Kind) || !slices.Contains([]string{"managed", "deployment"}, w.Source) || len(w.Bindings) == 0 || len(w.Bindings) > 32 {
		return fmt.Errorf("Credential identity, type, source and bounded use bindings are required")
	}
	if w.Source == "deployment" && (!credentialName.MatchString(w.Reference) || w.Sealed != "") || w.Source == "managed" && w.Reference != "" {
		return fmt.Errorf("Credential source and value reference differ")
	}
	for _, binding := range w.Bindings {
		kind, id, ok := strings.Cut(binding.Consumer, "/")
		if !ok || strings.TrimSpace(id) == "" || !slices.Contains([]string{"ai", "connection", "endpoint"}, kind) || CredentialOrigin(binding.Origin) == "" || CredentialOrigin(binding.Origin) != binding.Origin {
			return fmt.Errorf("Each use needs a consumer and an exact network origin without credentials")
		}
	}
	return nil
}
func openCredential(tenant string, w platform.CredentialWrite) ([]byte, error) {
	aead, err := credentialCipher()
	if err != nil {
		return nil, err
	}
	encoded, valid := strings.CutPrefix(w.Sealed, credentialEnvelopePrefix)
	if !valid {
		return nil, fmt.Errorf("Unsupported credential envelope format")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < aead.Overhead() {
		return nil, fmt.Errorf("Invalid credential envelope")
	}
	value, err := aead.Open(nil, nil, raw, credentialAAD(tenant, w))
	if err != nil {
		return nil, fmt.Errorf("Credential envelope does not match this tenant or configuration")
	}
	return value, nil
}
func DecodeCredentialWrite(tenant string, raw []byte) (platform.CredentialWrite, error) {
	var w platform.CredentialWrite
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&w); err != nil {
		return w, fmt.Errorf("Only encrypted credential submissions are accepted")
	}
	if err := checkCredentialDefinition(w); err != nil {
		return w, err
	}
	if w.Source == "managed" {
		if _, err := openCredential(tenant, w); err != nil {
			return w, err
		}
	}
	return w, nil
}

func (d *Credentials) Credential(id string) (platform.CredentialState, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	value, ok := d.values[id]
	return value, ok
}
func (d *Credentials) Catalog() platform.CredentialCatalog {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, err := credentialCipher()
	out := platform.CredentialCatalog{Credentials: []platform.CredentialInfo{}, ManagedAvailable: err == nil}
	for _, value := range d.values {
		out.Credentials = append(out.Credentials, value.CredentialInfo)
	}
	slices.SortFunc(out.Credentials, func(a, b platform.CredentialInfo) int { return strings.Compare(a.ID, b.ID) })
	return out
}
func (d *Credentials) Prepare(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	previous, exists := d.Credential(id)
	version := previous.Version
	var next platform.CredentialState
	if s.GetSchema().GetName() == SchemaCredentialDisable {
		var request struct {
			ExpectedVersion int `json:"expectedVersion"`
		}
		if json.Unmarshal(s.GetPayload(), &request) != nil || !exists || request.ExpectedVersion != version {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Credential version changed")
		}
		next = previous
		next.State = "disabled"
	} else {
		w, err := DecodeCredentialWrite(d.tenant, s.GetPayload())
		if err != nil || w.ID != id {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid credential configuration or encrypted value")
		}
		if w.ExpectedVersion != version {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Credential version changed; reload before saving")
		}
		next = platform.CredentialState{CredentialInfo: platform.CredentialInfo{CredentialDefinition: w.CredentialDefinition, State: "active"}, Sealed: w.Sealed, SealedVersion: w.ExpectedVersion}
	}
	next.Version, next.UpdatedAt, next.UpdatedBy = version+1, now, c.ID
	return func(*pb.ChangeRecord) { d.mu.Lock(); defer d.mu.Unlock(); d.values[id] = next }, nil
}

func ResolveCredential(tenant string, owner platform.Credentials, secret func(string) ([]byte, bool), name, consumer, address string) ([]byte, string, error) {
	deployment := func(reference, kind string) ([]byte, string, error) {
		value, ok := secret(reference)
		if !ok {
			return nil, kind, fmt.Errorf("Deployment credential is missing. Configure it in the host environment or use a platform-managed credential.")
		}
		return value, kind, nil
	}
	if owner == nil {
		return deployment(name, "")
	}
	record, found := owner.Credential(name)
	if !found {
		return deployment(name, "")
	}
	origin := CredentialOrigin(address)
	if record.State != "active" {
		return nil, "", fmt.Errorf("Credential is disabled. Enable or replace it in Credentials.")
	}
	if !slices.ContainsFunc(record.Bindings, func(b platform.CredentialBinding) bool { return b.Consumer == consumer && b.Origin == origin }) {
		return nil, "", fmt.Errorf("Credential is saved but is not authorized for this service and address. Edit its allowed uses in Credentials.")
	}
	if record.Source == "deployment" {
		return deployment(record.Reference, record.Kind)
	}
	value, err := openCredential(tenant, platform.CredentialWrite{CredentialDefinition: record.CredentialDefinition, ExpectedVersion: record.SealedVersion, Sealed: record.Sealed})
	if err != nil {
		return nil, record.Kind, fmt.Errorf("Saved credential cannot be decrypted. Check the host encryption key or replace the credential in Credentials.")
	}
	return value, record.Kind, nil
}

// SealCredential is the static encryption boundary before ordinary submission.
func SealCredential(tenant string, owner platform.Credentials, request platform.CredentialSealRequest) (platform.CredentialWrite, *kernel.Error) {
	if request.Source != "managed" || request.Sealed != "" || len(request.Value) > 8192 || strings.ContainsAny(request.Value, "\r\n") || checkCredentialDefinition(request.CredentialWrite) != nil {
		return platform.CredentialWrite{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A valid credential and nonempty single-line value are required")
	}
	if request.Value == "" {
		record, exists := owner.Credential(request.ID)
		if !exists || record.Source != "managed" || record.Version != request.ExpectedVersion {
			return platform.CredentialWrite{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Enter a value for a new credential or reload the current version")
		}
		value, err := openCredential(tenant, platform.CredentialWrite{CredentialDefinition: record.CredentialDefinition, ExpectedVersion: record.SealedVersion, Sealed: record.Sealed})
		if err != nil {
			return platform.CredentialWrite{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The current encrypted value is unavailable")
		}
		request.Value = string(value)
	}
	aead, err := credentialCipher()
	if err != nil {
		return platform.CredentialWrite{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	request.Sealed = credentialEnvelopePrefix + base64.StdEncoding.EncodeToString(aead.Seal(nil, nil, []byte(request.Value), credentialAAD(tenant, request.CredentialWrite)))
	return request.CredentialWrite, nil
}

// Provider failures may echo a submitted token. Scrub transmitted auth values
// before a remote error is exposed to a person or recorded as an outcome.
func RedactCredentialFailure(response *http.Response, request *http.Request) *http.Response {
	if response == nil || response.Body == nil || response.StatusCode < 400 {
		return response
	}
	values := []string{request.Header.Get("Authorization"), request.Header.Get("X-Api-Key")}
	if values[0] == "" && values[1] == "" {
		return response
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	response.Body.Close()
	if err != nil {
		raw = []byte(`{"error":{"message":"Remote request failed"}}`)
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, token, found := strings.Cut(value, " "); found {
			raw = bytes.ReplaceAll(raw, []byte(token), []byte("[redacted]"))
		}
		raw = bytes.ReplaceAll(raw, []byte(value), []byte("[redacted]"))
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	response.ContentLength = int64(len(raw))
	return response
}
