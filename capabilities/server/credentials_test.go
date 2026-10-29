package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
	"net/http/httptest"
	controlowner "platformserver/kernel/control"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/platform"
	"platformserver/studio/build"
)

const testCredentialKey = "1919191919191919191919191919191919191919191919191919191919191919"

func credentialTenant(t *testing.T, id string) *Tenant {
	return composeTenant(t, id, []platform.Seat{seatOf("admin", controlowner.PlatformApp+":"+controlowner.Admin, "ai:admin"), seatOf("ai-admin", "ai:admin"), seatOf("user", "ai:user")}, ai.New(id))
}
func sealCredential(t *testing.T, tn *Tenant, who string, w platform.CredentialWrite, value string) platform.CredentialWrite {
	t.Helper()
	handler := NewHost(Tokens(map[string]string{who: who}), tn).Handler()
	request := httptest.NewRequest("POST", "/v1/credentials/seal", bytes.NewReader(platform.Raw(platform.CredentialSealRequest{CredentialWrite: w, Value: value})))
	request.Header.Set("Authorization", "Bearer "+who)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("credential seal: %d %s", response.Code, response.Body.String())
	}
	if value != "" && strings.Contains(response.Body.String(), value) {
		t.Fatal("seal response exposed plaintext")
	}
	var sealed platform.CredentialWrite
	if err := json.Unmarshal(response.Body.Bytes(), &sealed); err != nil {
		t.Fatal(err)
	}
	return sealed
}
func TestCredentialAcceptedLifecycleAndRecovery(t *testing.T) {
	t.Setenv("PLATFORM_CREDENTIAL_KEY", testCredentialKey)
	tn := credentialTenant(t, "credential-test")
	var entries []Entry
	fail := false
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append unavailable")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	config := platform.CredentialWrite{CredentialDefinition: platform.CredentialDefinition{ID: "models", Title: "Models", Kind: "api-key", Source: "managed", Bindings: []platform.CredentialBinding{{Consumer: "ai/models", Origin: "https://models.example.test"}}}}
	sealed := sealCredential(t, tn, "admin", config, "private-test-key-01")
	if len(entries) != 0 {
		t.Fatal("sealing created durable state")
	}
	original := submission(tn, memberOf(t, tn, "admin"), controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "models", sealed)
	fail = true
	if _, refusal := tn.Submit(memberOf(t, tn, "admin"), original, now); refusal == nil {
		t.Fatal("failed append accepted")
	}
	if _, found := testConsoleOf(tn).Credential("models"); found {
		t.Fatal("failed append leaked state")
	}
	fail = false
	accepted, refusal := tn.Submit(memberOf(t, tn, "admin"), original, now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	again, refusal := tn.Submit(memberOf(t, tn, "admin"), original, now.Add(time.Second))
	if refusal != nil || !proto.Equal(again, accepted) || len(entries) != 1 {
		t.Fatalf("retry changed receipt: %v", refusal)
	}
	if value, ok := tn.credentialValue("models", "ai/models", "https://MODELS.example.test:443/v1"); !ok || string(value) != "private-test-key-01" {
		t.Fatal("authorized credential unavailable")
	}
	for _, usage := range []struct{ consumer, address string }{{"ai/other", "https://models.example.test"}, {"ai/models", "https://another.example.test"}, {"connection/models", "https://models.example.test"}} {
		if _, ok := tn.credentialValue("models", usage.consumer, usage.address); ok {
			t.Fatal("unbound use obtained key")
		}
	}
	for _, who := range []string{"admin", "ai-admin"} {
		answer, refusal := tn.Read(memberOf(t, tn, who), "credentials")
		if refusal != nil || strings.Contains(string(platform.Raw(answer)), "private-test-key-01") || strings.Contains(string(platform.Raw(answer)), sealed.Sealed) {
			t.Fatalf("metadata exposed value or was unreadable: %v", refusal)
		}
	}
	if _, refusal := tn.Read(memberOf(t, tn, "user"), "credentials"); refusal == nil {
		t.Fatal("ordinary user read credential catalog")
	}
	if got := refuse(t, tn, "ai-admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "models", sealed, now); got == "ok" {
		t.Fatal("AI administration granted credential management")
	}
	changed := sealed
	changed.Title = "Changed"
	before := len(entries)
	if got := refuse(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "models", changed, now); got == "ok" {
		t.Fatal("tampered configuration accepted")
	}
	if len(entries) != before {
		t.Fatal("invalid envelope entered durable refusal history")
	}
	if got := refuse(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "models", map[string]any{"value": "never-save-this-plaintext"}, now); got == "ok" || len(entries) != before {
		t.Fatal("plaintext entered ordinary journal")
	}
	other := credentialTenant(t, "another-tenant")
	if got := refuse(t, other, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "models", sealed, now); got == "ok" {
		t.Fatal("envelope crossed tenants")
	}
	config.ExpectedVersion = 1
	rotated := sealCredential(t, tn, "admin", config, "private-test-key-02")
	decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "models", rotated, now)
	if value, ok := tn.credentialValue("models", "ai/models", "https://models.example.test"); !ok || string(value) != "private-test-key-02" {
		t.Fatal("rotation did not take effect")
	}
	decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialDisable, controlowner.CredentialType, "models", map[string]int{"expectedVersion": 2}, now)
	t.Setenv("PLATFORM_SECRET_MODELS", "must-not-fallback")
	if _, ok := tn.credentialValue("models", "ai/models", "https://models.example.test"); ok {
		t.Fatal("disabled credential fell back to environment")
	}
	// A disabled value can be explicitly re-enabled without displaying or re-entering it.
	config.ExpectedVersion = 3
	metadata := sealCredential(t, tn, "admin", config, "")
	decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "models", metadata, now)
	if value, ok := tn.credentialValue("models", "ai/models", "https://models.example.test"); !ok || string(value) != "private-test-key-02" {
		t.Fatal("metadata edit lost the sealed value")
	}
	// Recovery applies the accepted encrypted image without the encryption key.
	t.Setenv("PLATFORM_CREDENTIAL_KEY", "")
	restored := credentialTenant(t, "credential-test")
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	retry, refusal := restored.Submit(memberOf(t, restored, "admin"), original, now.Add(time.Hour))
	if refusal != nil || !proto.Equal(retry, accepted) {
		t.Fatal("saved encrypted request required the decryption key to retry")
	}
	record, ok := testConsoleOf(restored).Credential("models")
	if !ok || record.Version != 4 || record.State != "active" {
		t.Fatalf("recovered credential: %+v", record)
	}
	for _, entry := range entries {
		if bytes.Contains(entry.Body, []byte("private-test-key")) {
			t.Fatal("journal exposed plaintext")
		}
	}
}

func TestCredentialSharedAIAndConnectionUse(t *testing.T) {
	t.Setenv("PLATFORM_CREDENTIAL_KEY", testCredentialKey)
	tn := credentialTenant(t, "credential-consumers")
	var headers []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("Authorization"))
		fmt.Fprint(w, `{"data":[{"id":"example"}]}`)
	}))
	defer upstream.Close()
	origin := controlowner.CredentialOrigin(upstream.URL)
	w := platform.CredentialWrite{CredentialDefinition: platform.CredentialDefinition{ID: "shared", Title: "Shared API", Kind: "bearer", Source: "managed", Bindings: []platform.CredentialBinding{{Consumer: "ai/local", Origin: origin}, {Consumer: "connection/read", Origin: origin}}}}
	sealed := sealCredential(t, tn, "admin", w, "shared-test-token")
	at := time.Now()
	decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "shared", sealed, at)
	decide(t, tn, "admin", "ai", "ai.provider.add", "ai.provider", "local", map[string]string{"kind": "local", "baseUrl": upstream.URL, "secret": "shared"}, at)
	decide(t, tn, "admin", "ai", "ai.model.enable", "ai.model", "local/example", map[string]string{"access": "users"}, at)
	if _, refusal, failure := tn.ProviderModels(memberOf(t, tn, "admin"), "local", true); refusal != nil || failure != nil {
		t.Fatalf("provider check: %v %v", refusal, failure)
	}
	connection := build.Connection{Record: platform.Record{ID: "read"}, Kind: "http", Address: upstream.URL, Secret: "shared", AllowPrivate: true}
	rows, err := tn.readSourceWithConnection(build.Source{URL: "records", Path: "data"}, &connection)
	if err != nil || len(rows) != 1 || rows[0]["id"] != "example" || len(headers) != 2 || headers[0] != "Bearer shared-test-token" || headers[1] != "Bearer shared-test-token" {
		t.Fatalf("shared AI and real connection read failed: %v %v", err, headers)
	}
	if _, err := tn.readSourceWithConnection(build.Source{URL: "https://different.invalid/records", Path: "data"}, &connection); err == nil || len(headers) != 2 {
		t.Fatal("source escaped the credential's bound origin")
	}
	t.Setenv("PLATFORM_SECRET_DEPLOYED", "deployment-test-token")
	deployment := platform.CredentialWrite{CredentialDefinition: platform.CredentialDefinition{ID: "deployment", Title: "Deployment reference", Kind: "bearer", Source: "deployment", Reference: "deployed", Bindings: []platform.CredentialBinding{{Consumer: "connection/read", Origin: origin}}}}
	decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "deployment", deployment, at)
	connection.Secret = "deployment"
	if _, err := tn.readSourceWithConnection(build.Source{URL: "records", Path: "data"}, &connection); err != nil || headers[len(headers)-1] != "Bearer deployment-test-token" {
		t.Fatalf("deployment reference did not use the same authorized connection path: %v", err)
	}
	decide(t, tn, "admin", "ai", "ai.provider.edit", "ai.provider", "local", map[string]string{"kind": "local", "baseUrl": upstream.URL + "/v2", "secret": "shared"}, at)
	models, refusal := tn.Read(memberOf(t, tn, "admin"), "ai-models")
	if refusal != nil || len(models.([]platform.AIModel)) != 1 {
		t.Fatal("editing removed enabled models")
	}
	tn.AIClient = func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("rejected shared-test-token"))}, nil
	}
	if _, _, failure := tn.ProviderModels(memberOf(t, tn, "admin"), "local", true); failure == nil || strings.Contains(failure.Detail, "shared-test-token") {
		t.Fatal("provider failure exposed a transmitted key")
	}
	decide(t, tn, "admin", "ai", "ai.provider.edit", "ai.provider", "local", map[string]string{"kind": "local", "baseUrl": "http://different.invalid", "secret": "shared"}, at)
	if _, failure := tn.modelProvider(platform.AIProvider{ID: "local", Secret: "shared", BaseURL: "http://different.invalid"}).Send(context.Background(), platform.AIProvider{ID: "local", Secret: "shared", BaseURL: "http://different.invalid"}, "GET", "/models", nil); failure == nil {
		t.Fatal("provider edit sent credential to unbound target")
	}
}

func TestCredentialBindingRejectsEmptyOriginAndRepairsSavedUse(t *testing.T) {
	t.Setenv("PLATFORM_CREDENTIAL_KEY", testCredentialKey)
	tn := credentialTenant(t, "credential-binding-repair")
	config := platform.CredentialWrite{CredentialDefinition: platform.CredentialDefinition{ID: "DeepseekApiKey", Title: "Model key", Kind: "api-key", Source: "managed", Bindings: []platform.CredentialBinding{{Consumer: "ai/Deepseek", Origin: ""}}}}
	handler := NewHost(Tokens(map[string]string{"admin": "admin"}), tn).Handler()
	request := httptest.NewRequest("POST", "/v1/credentials/seal", bytes.NewReader(platform.Raw(platform.CredentialSealRequest{CredentialWrite: config, Value: "binding-test-key"})))
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "binding-test-key") {
		t.Fatalf("empty origin was accepted or exposed a value: status %d", response.Code)
	}
	config.Bindings[0].Origin = "https://api.example.test"
	sealed := sealCredential(t, tn, "admin", config, "binding-test-key")
	decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, config.ID, sealed, txnAt)
	provider := platform.AIProvider{ID: "deepseek", Secret: config.ID, BaseURL: "https://api.example.test/v1"}
	calls := 0
	tn.AIClient = func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("Authorization") != "Bearer binding-test-key" {
			t.Fatal("binding repair lost the saved key")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	}
	if _, failure := tn.modelProvider(provider).Send(context.Background(), provider, "GET", "/models", nil); failure == nil || !strings.Contains(failure.Detail, "not authorized") || strings.Contains(failure.Detail, "missing") || calls != 0 {
		t.Fatalf("binding mismatch was reported as a missing key or sent it: %v", failure)
	}
	config.ExpectedVersion = 1
	config.Bindings[0].Consumer = "ai/deepseek"
	rebound := sealCredential(t, tn, "admin", config, "")
	decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, config.ID, rebound, txnAt)
	answer, failure := tn.modelProvider(provider).Send(context.Background(), provider, "GET", "/models", nil)
	if failure != nil || calls != 1 {
		t.Fatalf("saved key was unavailable after binding repair: %v", failure)
	}
	answer.Body.Close()
}

func TestCredentialJournalCheckpointAndFullRecovery(t *testing.T) {
	t.Setenv("PLATFORM_CREDENTIAL_KEY", testCredentialKey)
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			id := fmt.Sprintf("credential-journal-%s-%d", backend.name, time.Now().UnixNano())
			t.Cleanup(func() { backend.cleanup(t, id) })
			ctx := context.Background()
			journal := backend.open(t, id)
			defer journal.Close()
			if _, err := journal.Entries(ctx, id, 0); err != nil {
				t.Fatal(err)
			}
			compose := func() *Tenant { return credentialTenant(t, id) }
			live := compose()
			live.attachJournal(ctx, journal)
			if err := live.migrateRuntimeState(ctx, journal); err != nil {
				t.Fatal(err)
			}
			config := platform.CredentialWrite{CredentialDefinition: platform.CredentialDefinition{ID: "api", Title: "API", Kind: "api-key", Source: "managed", Bindings: []platform.CredentialBinding{{Consumer: "ai/provider", Origin: "https://api.example.test"}}}}
			sealed := sealCredential(t, live, "admin", config, "journal-private-credential")
			decide(t, live, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, "api", sealed, time.Now())
			if _, err := live.checkpointRuntime(ctx, journal, "credential-checkpoint"); err != nil {
				t.Fatal(err)
			}
			decide(t, live, "admin", controlowner.PlatformApp, controlowner.SchemaCredentialDisable, controlowner.CredentialType, "api", map[string]int{"expectedVersion": 1}, time.Now())
			restored := compose()
			deployment := Deployment{SnapshotEvery: 1, Rebuild: func(string) (*Tenant, error) { return compose(), nil }}
			if err := deployment.restoreTenants(ctx, journal, "credential-new-build", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil {
				t.Fatal(err)
			}
			credential, exists := testConsoleOf(restored).Credential("api")
			if !exists || credential.Version != 2 || credential.State != "disabled" {
				t.Fatalf("checkpoint suffix: %+v", credential)
			}
			position := journal.Position(id)
			restored.quarantine(fmt.Errorf("exercise credential full-log recovery"))
			registry := newTenantRegistry([]*Tenant{restored})
			if err := deployment.retryTenant(ctx, journal, registry, CodeOf(restored), id); err != nil {
				t.Fatal(err)
			}
			final, exists := testConsoleOf(registry.current(id)).Credential("api")
			if !exists || !sameStoredJSON(platform.Raw(final), platform.Raw(credential)) || journal.Position(id) != position {
				t.Fatal("full recovery changed credential state or repeated writes")
			}
			entries, err := journal.Entries(ctx, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if bytes.Contains(entry.Body, []byte("journal-private-credential")) {
					t.Fatal("durable journal contains plaintext")
				}
			}
		})
	}
}
