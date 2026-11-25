package host

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK45ConnectorBindsOnlyAfterItsConformanceTestsPass(t *testing.T) {
	const tenant, contract, connector = "invariant-k45", "telephony.call/1", build.ID + ":cloudtalk"
	admin := runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	live := newInvariantTenant(t, tenant, admin)
	try := func(schema, id string, payload any) *platform.Error {
		t.Helper()
		_, refusal := live.Submit(live.member(t), live.submission(schema, build.ConnectorSetupType, id, payload), invariantClock)
		return refusal
	}
	bind := func() *platform.Error {
		sub := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": connector})
		sub.PrincipalId, sub.Authority = "admin", controlowner.PlatformApp
		member, _ := live.Member("admin")
		_, refusal := live.Submit(member, sub, invariantClock)
		return refusal
	}
	bound := func(tenant *Tenant) string {
		for _, info := range tenant.router().Protocols() {
			if info.ID == contract {
				return info.Bound
			}
		}
		return ""
	}
	setup := func() build.ConnectorSetup {
		found, _ := platform.Get[build.ConnectorSetup](live.automation(build.ID, false), "CLOUDTALK")
		return found
	}
	var sent []*http.Request
	var bodies []string
	answer := func(status int, body string) {
		live.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
			raw := []byte{}
			if req.Body != nil {
				raw, _ = io.ReadAll(req.Body)
			}
			sent, bodies = append(sent, req), append(bodies, string(raw))
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
	}
	run := func() {
		t.Helper()
		live.accept(t, "build.connectorsetup.test", build.ConnectorSetupType, "CLOUDTALK", map[string]any{})
		live.CheckConnections(invariantClock)
	}
	implementation := func(credential string) map[string]any {
		return map[string]any{"name": "cloudtalk", "title": "CloudTalk", "contract": contract, "kind": "connector", "credential": credential,
			"origins":  []string{"https://api.cloudtalk.example", "https://eu.cloudtalk.example"},
			"requests": []runtimeprotocol.ImplementationRequest{{Action: "dial", Method: "POST", Path: "/calls/{number}"}}}
	}

	number := []platform.Field{{Name: "number", Label: "Number", Type: "text", Required: true}}
	live.accept(t, "build.contract.create", build.ContractType, "CALL", map[string]any{"name": "telephony.call", "major": 1, "title": "Phone calls",
		"actions": []runtimeprotocol.ContractAction{{Name: "dial", Title: "Dial", Payload: number}},
		"tests":   []runtimeprotocol.ContractTest{{Name: "dials", Action: "dial", Payload: map[string]any{"number": "+15550100"}, Expect: []string{"call"}}}})
	live.accept(t, "build.contract.publish", build.ContractType, "CALL", map[string]any{})
	live.accept(t, "build.implementation.create", build.ImplementationType, "CLOUDTALK", implementation("bearer"))
	live.accept(t, "build.implementation.publish", build.ImplementationType, "CLOUDTALK", map[string]any{})

	if refused := try("build.connectorsetup.create", "CLOUDTALK", map[string]any{"implementation": connector, "address": "https://evil.example"}); refused == nil || !strings.Contains(refused.Message, "outbound origins") {
		t.Fatalf("a setup may only call one of the connector's outbound origins: %+v", refused)
	}
	live.accept(t, "build.connectorsetup.create", build.ConnectorSetupType, "CLOUDTALK", map[string]any{"implementation": connector, "address": "https://api.cloudtalk.example"})
	if refused := try("build.connectorsetup.create", "SECOND", map[string]any{"implementation": connector}); refused == nil || !strings.Contains(refused.Message, "already has a setup") {
		t.Fatalf("a connector has one setup per tenant: %+v", refused)
	}
	if refused := bind(); refused == nil || !strings.Contains(refused.Message, "conformance tests") {
		t.Fatalf("an untested connector must not be bound: %+v", refused)
	}
	if refused := try("build.connectorsetup.test", "CLOUDTALK", map[string]any{}); refused == nil || !strings.Contains(refused.Message, "credential") {
		t.Fatalf("a connector that needs a credential is not tested without one: %+v", refused)
	}

	answer(http.StatusOK, `{"call":"C1"}`)
	live.accept(t, "build.connectorsetup.edit", build.ConnectorSetupType, "CLOUDTALK", map[string]any{"secret": "callkey"})
	run()
	if got := setup(); got.State != "failed" || got.Last == nil || got.Last.Passed || len(sent) != 0 || !strings.Contains(got.Last.Cases[0].Detail, "credential") {
		t.Fatalf("a credential not allowed for this connector fails the run before anything is sent: %+v, %d sent", got, len(sent))
	}

	live.accept(t, "build.implementation.edit", build.ImplementationType, "CLOUDTALK", implementation(""))
	live.accept(t, "build.implementation.publish", build.ImplementationType, "CLOUDTALK", map[string]any{})
	live.accept(t, "build.connectorsetup.edit", build.ConnectorSetupType, "CLOUDTALK", map[string]any{"secret": ""})
	answer(http.StatusOK, `{}`)
	run()
	if got := setup(); got.State != "failed" || got.Last == nil || !strings.Contains(got.Last.Cases[0].Detail, "call") {
		t.Fatalf("an answer without the expected field fails the test: %+v", got.Last)
	}
	if bind() == nil {
		t.Fatal("a connector whose last run failed must not be bound")
	}

	answer(http.StatusOK, `{"call":"C1"}`)
	run()
	if got := setup(); got.State != "passed" || got.Last == nil || !got.Last.Passed || got.Requested {
		t.Fatalf("a run where every test answers as expected passes: %+v", got)
	}
	last := sent[len(sent)-1]
	if last.Method != http.MethodPost || last.URL.String() != "https://api.cloudtalk.example/calls/+15550100" || !strings.Contains(bodies[len(bodies)-1], `"number":"+15550100"`) || last.Header.Get("Idempotency-Key") == "" {
		t.Fatalf("the test calls the mapped request at the tenant's address: %s %s %s", last.Method, last.URL, bodies[len(bodies)-1])
	}
	if first := sent[len(sent)-2]; first.Header.Get("Idempotency-Key") == last.Header.Get("Idempotency-Key") {
		t.Fatal("a fresh test run must reach the connector with its own idempotency key")
	}
	if refused := bind(); refused != nil {
		t.Fatalf("a connector that passed with its current setup binds: %+v", refused)
	}
	if bound(live.Tenant) != connector {
		t.Fatalf("the contract lists the bound connector: %q", bound(live.Tenant))
	}

	live.accept(t, "build.connectorsetup.edit", build.ConnectorSetupType, "CLOUDTALK", map[string]any{"address": "https://eu.cloudtalk.example"})
	if got := setup(); got.State != "draft" || got.Last != nil {
		t.Fatalf("changing the address clears the last run: %+v", got)
	}
	if bind() == nil {
		t.Fatal("after the setup changed, binding needs a new passing run")
	}
	answer(http.StatusOK, `{"call":"C2"}`)
	run()
	live.accept(t, "build.contract.edit", build.ContractType, "CALL", map[string]any{"title": "Phone calls and dialling"})
	live.accept(t, "build.contract.publish", build.ContractType, "CALL", map[string]any{})
	if refused := bind(); refused == nil || !strings.Contains(refused.Message, "changed since its last test run") {
		t.Fatalf("a contract that changed since the last run needs the tests again: %+v", refused)
	}

	before := live.recordImage(t)
	replayed := live.restore(t, live.entries, admin)
	if after := replayed.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must rebuild the setup and its test results")
	}
	if bound(replayed.Tenant) != connector {
		t.Fatal("replay must restore the binding")
	}
}
