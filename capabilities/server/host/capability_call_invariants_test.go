package host

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK46BoundConnectorServesCapabilityCallsThroughOutboundEffects(t *testing.T) {
	const tenant, contract, connector = "invariant-k46", "telephony.call/1", build.ID + ":cloudtalk"
	admin := runtimeprotocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	live := newInvariantTenant(t, tenant, admin)
	var sent []string
	answer := func(status int, body string, failure error) {
		live.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
			raw := []byte{}
			if req.Body != nil {
				raw, _ = io.ReadAll(req.Body)
			}
			sent = append(sent, req.Method+" "+req.URL.String()+" "+string(raw))
			if failure != nil {
				return nil, failure
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
	}
	deliver := func(tenant *Tenant, at time.Time) {
		for _, send := range tenant.dispatches(at) {
			send()
		}
	}
	call := func(key, number string) (string, *platform.Error) {
		t.Helper()
		target, _, refusal := live.Invoke(live.member(t), contract, "dial", "", platform.Raw(map[string]any{"number": number}), key, invariantClock)
		if refusal != nil {
			return "", refusal
		}
		return target.GetId(), nil
	}
	record := func(tenant *Tenant, id string) build.CapabilityCall {
		found, _ := platform.Get[build.CapabilityCall](tenant.automation(build.ID, false), id)
		return found
	}

	live.accept(t, "build.contract.create", build.ContractType, "CALL", map[string]any{"name": "telephony.call", "major": 1, "title": "Phone calls",
		"actions": []runtimeprotocol.ContractAction{{Name: "dial", Title: "Dial", Payload: []platform.Field{{Name: "number", Label: "Number", Type: "text", Required: true}}}},
		"tests":   []runtimeprotocol.ContractTest{{Name: "dials", Action: "dial", Payload: map[string]any{"number": "+15550100"}, Expect: []string{"call"}}}})
	live.accept(t, "build.contract.publish", build.ContractType, "CALL", map[string]any{})
	live.accept(t, "build.implementation.create", build.ImplementationType, "CLOUDTALK", map[string]any{"name": "cloudtalk", "title": "CloudTalk", "contract": contract,
		"kind": "connector", "origins": []string{"https://api.cloudtalk.example"}, "requests": []runtimeprotocol.ImplementationRequest{{Action: "dial", Method: "POST", Path: "/calls/{number}"}}})
	live.accept(t, "build.implementation.publish", build.ImplementationType, "CLOUDTALK", map[string]any{})
	live.accept(t, "build.connectorsetup.create", build.ConnectorSetupType, "CLOUDTALK", map[string]any{"implementation": connector, "address": "https://api.cloudtalk.example"})

	if _, refused := call("before-bind", "+15550101"); refused == nil {
		t.Fatal("a capability with no bound connector does not call out")
	}
	connectorState := func() string {
		for _, info := range live.router().Protocols() {
			for _, i := range info.Implementations {
				if i.ID == connector {
					return i.State
				}
			}
		}
		return ""
	}
	if got := connectorState(); got != "untested" {
		t.Fatalf("the capability page shows an untested connector: %q", got)
	}
	answer(http.StatusOK, `{"call":"T1"}`, nil)
	live.accept(t, "build.connectorsetup.test", build.ConnectorSetupType, "CLOUDTALK", map[string]any{})
	live.CheckConnections(invariantClock)
	if got := connectorState(); got != "passed" {
		t.Fatalf("the capability page shows the passing run: %q", got)
	}
	bind := live.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": connector})
	bind.PrincipalId, bind.Authority = "admin", controlowner.PlatformApp
	member, _ := live.Member("admin")
	if _, refused := live.Submit(member, bind, invariantClock); refused != nil {
		t.Fatalf("a tested connector binds: %+v", refused)
	}
	tested := len(sent)

	if _, refused := call("", "+15550102"); refused == nil {
		t.Fatal("a connector call needs an idempotency key")
	}
	direct := live.submission(runtimeprotocol.SchemaCapabilityCallSend, runtimeprotocol.CapabilityCallType, "forged", map[string]any{"protocol": contract, "action": "dial", "implementation": connector})
	direct.IdempotencyKey = "forged"
	if _, refused := live.Submit(live.member(t), direct, invariantClock); refused == nil {
		t.Fatal("a call record whose id does not come from the capability call is refused")
	}

	answer(http.StatusOK, `{"call":"C1"}`, nil)
	first, refused := call("order-1", "+15550123")
	if refused != nil {
		t.Fatalf("the bound connector accepts the call: %+v", refused)
	}
	if got := record(live.Tenant, first); got.State != "sent" || len(sent) != tested {
		t.Fatalf("the call is recorded before anything is sent: %+v, %d sent", got, len(sent)-tested)
	}
	if again, _ := call("order-1", "+15550123"); again != first {
		t.Fatalf("the same key names the same call: %s %s", first, again)
	}
	deliver(live.Tenant, invariantClock.Add(time.Second))
	if len(sent) != tested+1 || sent[tested] != `POST https://api.cloudtalk.example/calls/+15550123 {"number":"+15550123"}` {
		t.Fatalf("the outbound effect sends the mapped request once: %v", sent[tested:])
	}
	if got := record(live.Tenant, first); got.State != "answered" || !strings.Contains(got.Answer, "C1") || got.Effect == "" {
		t.Fatalf("the connector answer is written back to the call: %+v", got)
	}

	answer(0, "", errors.New("connection reset"))
	second, refused := call("order-2", "+15550124")
	if refused != nil {
		t.Fatalf("second call: %+v", refused)
	}
	deliver(live.Tenant, invariantClock.Add(2*time.Second))
	deliver(live.Tenant, invariantClock.Add(time.Hour))
	if got := record(live.Tenant, second); got.State != "unknown" || len(sent) != tested+2 {
		t.Fatalf("a call without an answer is not resent and waits for a decision: %+v, %d sent", got, len(sent)-tested)
	}
	if effect, found := live.outbound.Get(record(live.Tenant, second).Effect); !found || effect.State != "unknown" {
		t.Fatalf("the unknown outcome is left in the outbound effects for a person to settle: %+v", effect)
	}

	before := live.recordImage(t)
	replayed := live.restore(t, live.entries, admin)
	replayed.Outbound = live.Outbound
	deliver(replayed.Tenant, invariantClock.Add(2*time.Hour))
	if after := replayed.recordImage(t); !reflect.DeepEqual(before, after) || len(sent) != tested+2 {
		t.Fatalf("replay rebuilds the calls without calling the connector again: %d sent", len(sent)-tested)
	}
}
