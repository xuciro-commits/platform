package platformserver

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

const (
	CredentialType           = "platform.credential"
	SchemaCredentialSave     = "platform.credential.save"
	SchemaCredentialDisable  = "platform.credential.disable"
	credentialEnvelopePrefix = "aes256gcm-v1:"
)

type CredentialBinding = platform.CredentialBinding
type CredentialDefinition = platform.CredentialDefinition
type CredentialInfo = platform.CredentialInfo
type CredentialWrite = platform.CredentialWrite
type CredentialSealRequest = platform.CredentialSealRequest
type CredentialCatalog = platform.CredentialCatalog
type credentialRecord = host.CredentialState

var credentialName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func credentialActions() []platform.Action {
	fields := []platform.Field{{Name: "id", Type: "string", Required: true}, {Name: "title", Type: "string", Required: true}, {Name: "kind", Type: "string", Required: true}, {Name: "source", Type: "string", Required: true}, {Name: "reference", Type: "string"}, {Name: "bindings", Type: "json", Required: true}, {Name: "expectedVersion", Type: "integer", Required: true}, {Name: "sealed", Type: "string"}}
	return []platform.Action{
		{Schema: SchemaCredentialSave, Target: CredentialType, Capability: "credentials", Title: "Save credential", Description: "Save an authenticated encrypted value or a deployment reference; never submit plaintext.", Roles: []string{Admin}, Payload: fields},
		{Schema: SchemaCredentialDisable, Target: CredentialType, Capability: "credentials", Title: "Disable credential", Description: "Stop new credential use without deleting encrypted history", Roles: []string{Admin}, Payload: []platform.Field{{Name: "expectedVersion", Type: "integer", Required: true}}},
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
func credentialAAD(tenant string, w CredentialWrite) []byte {
	w.Sealed = ""
	return platform.Raw(struct {
		Tenant string          `json:"tenant"`
		Write  CredentialWrite `json:"write"`
	}{tenant, w})
}
func credentialOrigin(address string) string {
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
func checkCredentialDefinition(w CredentialWrite) error {
	if !credentialName.MatchString(w.ID) || strings.TrimSpace(w.Title) == "" || w.ExpectedVersion < 0 || !slices.Contains([]string{"api-key", "bearer", "password"}, w.Kind) || !slices.Contains([]string{"managed", "deployment"}, w.Source) || len(w.Bindings) == 0 || len(w.Bindings) > 32 {
		return fmt.Errorf("Credential identity, type, source and bounded use bindings are required")
	}
	if w.Source == "deployment" && (!credentialName.MatchString(w.Reference) || w.Sealed != "") || w.Source == "managed" && w.Reference != "" {
		return fmt.Errorf("Credential source and value reference differ")
	}
	for _, binding := range w.Bindings {
		kind, id, ok := strings.Cut(binding.Consumer, "/")
		if !ok || id == "" || !slices.Contains([]string{"ai", "connection", "endpoint"}, kind) || credentialOrigin(binding.Origin) != binding.Origin {
			return fmt.Errorf("Each use needs a consumer and an exact network origin without credentials")
		}
	}
	return nil
}
func openCredential(tenant string, w CredentialWrite) ([]byte, error) {
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
func decodeCredentialWrite(tenant string, raw []byte) (CredentialWrite, error) {
	var w CredentialWrite
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

func (d *Console) Credential(id string) (credentialRecord, bool) {
	d.credentialMu.RLock()
	defer d.credentialMu.RUnlock()
	value, ok := d.credentials[id]
	return value, ok
}
func (d *Console) credentialCatalog() CredentialCatalog {
	d.credentialMu.RLock()
	defer d.credentialMu.RUnlock()
	_, err := credentialCipher()
	out := CredentialCatalog{Credentials: []CredentialInfo{}, ManagedAvailable: err == nil}
	for _, value := range d.credentials {
		out.Credentials = append(out.Credentials, value.CredentialInfo)
	}
	slices.SortFunc(out.Credentials, func(a, b CredentialInfo) int { return strings.Compare(a.ID, b.ID) })
	return out
}
func (d *Console) decideCredential(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	previous, exists := d.Credential(id)
	version := previous.Version
	var next credentialRecord
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
		w, err := decodeCredentialWrite(d.tenant, s.GetPayload())
		if err != nil || w.ID != id {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid credential configuration or encrypted value")
		}
		if w.ExpectedVersion != version {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Credential version changed; reload before saving")
		}
		next = credentialRecord{CredentialInfo: CredentialInfo{CredentialDefinition: w.CredentialDefinition, State: "active"}, Sealed: w.Sealed, SealedVersion: w.ExpectedVersion}
	}
	next.Version, next.UpdatedAt, next.UpdatedBy = version+1, now, c.ID
	return func(*pb.ChangeRecord) { d.credentialMu.Lock(); defer d.credentialMu.Unlock(); d.credentials[id] = next }, nil
}

// credentialValue is shared by AI, connections and effects. A managed name never
// falls back to the process environment when disabled, unbound or unreadable.
func (t *Tenant) resolveCredential(name, consumer, address string) ([]byte, string, bool) {
	owner, available := t.app(PlatformApp).(host.Credentials)
	if !available {
		value, ok := t.secret(name)
		return value, "", ok
	}
	record, found := owner.Credential(name)
	if !found {
		value, ok := t.secret(name)
		return value, "", ok
	}
	origin := credentialOrigin(address)
	if record.State != "active" || !slices.ContainsFunc(record.Bindings, func(b CredentialBinding) bool { return b.Consumer == consumer && b.Origin == origin }) {
		return nil, "", false
	}
	if record.Source == "deployment" {
		value, ok := t.secret(record.Reference)
		return value, record.Kind, ok
	}
	value, err := openCredential(t.ID, CredentialWrite{CredentialDefinition: record.CredentialDefinition, ExpectedVersion: record.SealedVersion, Sealed: record.Sealed})
	return value, record.Kind, err == nil
}
func (t *Tenant) credentialValue(name, consumer, address string) ([]byte, bool) {
	value, _, ok := t.resolveCredential(name, consumer, address)
	return value, ok
}
func (t *Tenant) credentialHeader(name, consumer, address string) ([]byte, bool) {
	value, kind, ok := t.resolveCredential(name, consumer, address)
	if ok && kind == "bearer" {
		value = []byte("Bearer " + string(value))
	}
	return value, ok
}

func (h *Host) routesCredentials(rt *routes) {
	rt.authorized(Route{Pattern: "POST /v1/credentials/seal", Summary: "Encrypt a transient credential before ordinary submission", Body: CredentialSealRequest{}, Answer: CredentialWrite{}}, PlatformApp, []string{Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request CredentialSealRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || request.Source != "managed" || request.Sealed != "" || len(request.Value) > 8192 || strings.ContainsAny(request.Value, "\r\n") || checkCredentialDefinition(request.CredentialWrite) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A valid credential and nonempty single-line value are required"))
			return
		}
		if request.Value == "" {
			record, exists := t.app(PlatformApp).(host.Credentials).Credential(request.ID)
			if !exists || record.Source != "managed" || record.Version != request.ExpectedVersion {
				Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Enter a value for a new credential or reload the current version"))
				return
			}
			value, err := openCredential(t.ID, CredentialWrite{CredentialDefinition: record.CredentialDefinition, ExpectedVersion: record.SealedVersion, Sealed: record.Sealed})
			if err != nil {
				Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The current encrypted value is unavailable"))
				return
			}
			request.Value = string(value)
		}
		aead, err := credentialCipher()
		if err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error()))
			return
		}
		request.Sealed = credentialEnvelopePrefix + base64.StdEncoding.EncodeToString(aead.Seal(nil, nil, []byte(request.Value), credentialAAD(t.ID, request.CredentialWrite)))
		request.Value = ""
		w.Header().Set("Cache-Control", "no-store")
		WriteJSON(w, http.StatusOK, request.CredentialWrite)
	})
}

// Provider failures may echo a submitted token. Scrub transmitted auth values
// before a remote error is exposed to a person or recorded as an outcome.
func redactCredentialFailure(response *http.Response, request *http.Request) *http.Response {
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
