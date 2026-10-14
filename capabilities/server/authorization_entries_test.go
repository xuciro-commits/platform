package platformserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/relations"
	"platformserver/platform"
	"platformserver/platform/authz"
)

func TestAuthorizationNineEntryFamilies(t *testing.T) {
	freeze := func(tn *Tenant, app string) {
		tn.app(app).Manifest().Actions.UsePolicies(func() []authz.Policy {
			return []authz.Policy{{ID: "frozen", Effect: "deny", Permission: "*"}}
		})
	}
	denied := func(err *kernel.Error) {
		t.Helper()
		if err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED || !strings.Contains(err.Message, "Authorization denied at deny:") {
			t.Fatalf("entry escaped the ordered decision: %+v", err)
		}
	}
	t.Run("submit", func(t *testing.T) {
		tn := stockTenant(t)
		freeze(tn, "stock")
		_, err := tn.Submit(memberOf(t, tn, "ana"), submission(tn, memberOf(t, tn, "ana"), "stock", "stock.item.create", "stock.item", "blocked", map[string]any{"name": "Press"}), txnAt)
		denied(err)
		if tn.records.types["stock.item"].rows.len() != 0 {
			t.Fatal("refused submit wrote a row")
		}
	})
	t.Run("input", func(t *testing.T) {
		tn, owner, _ := setup(t, nil)
		freeze(tn, "a")
		_, err := tn.Input(memberOf(t, tn, "ana"), "a-feed", []byte("blocked"), txnAt)
		denied(err)
		if len(owner.texts) != 0 {
			t.Fatal("refused input ran its owner")
		}
	})
	t.Run("protocol", func(t *testing.T) {
		tn := newRequestsTenant(t, true)
		freeze(tn, "stock")
		_, _, err := tn.Invoke(memberOf(t, tn, "b"), testProtocol().ID(), "hold", "blocked", platform.Raw(map[string]string{"item": "pen"}), "blocked", txnAt)
		denied(err)
	})
	t.Run("owned work", func(t *testing.T) {
		owner := newResultWorker("work-authz")
		tn := composeTenant(t, "work-authz", []Seat{seatOf("ana")}, owner)
		freeze(tn, "worker")
		var saved acceptedWork
		tn.Record = func(entry Entry) {
			var err error
			saved, err = decodeAcceptedWork(entry.Body)
			if err != nil {
				t.Fatal(err)
			}
		}
		tn.Work(txnAt)
		if saved.Outcome != pb.ErrorCode_ERROR_CODE_POLICY_DENIED.String() || len(saved.Authorizations) != 1 || saved.Authorizations[0].Decision.Policy != "frozen" || owner.calls != 0 {
			t.Fatal("owned work did not retain its denial before executing")
		}
	})
	t.Run("effect callback", func(t *testing.T) {
		tn := stockTenant(t)
		freeze(tn, "stock")
		draft := tn.newStagedDecision()
		err := tn.stageEffectAnswer(draft, platform.Effect{ID: "accepted-effect", App: "stock", Endpoint: "sink"}, platform.Outcome{Effect: "accepted-effect", Result: "delivered"}, nil, txnAt)
		refusal, ok := err.(*kernel.Error)
		if !ok {
			t.Fatalf("callback lost typed authorization refusal: %v", err)
		}
		denied(refusal)
		if len(draft.records.writes) != 0 {
			t.Fatal("refused callback wrote state")
		}
	})
	t.Run("agent step", func(t *testing.T) {
		tn := stockTenant(t, NewAgents("t-1"))
		if err := tn.automation(AgentApp, true).PutAt(txnAt, "fixture", AgentRunRecord{Record: platform.Record{ID: "R"}, Agent: "stock.guide", OnBehalf: "ana", State: "running"}); err != nil {
			t.Fatal(err)
		}
		freeze(tn, AgentApp)
		draft := tn.newStagedDecision()
		c := platform.NewCaller(draft, tn.automation(AgentApp, false).Member, AgentApp, false, true)
		err := draft.stateApp(tn.agents).(AgentRuntime).ApplyAgentStepCaller(c, stepBody{Run: "R", Stop: "stopped"}, txnAt)
		denied(err)
		if len(draft.records.writes) != 0 {
			t.Fatal("refused agent step wrote state")
		}
	})
	t.Run("model usage", func(t *testing.T) {
		tn := composeTenant(t, "usage-authz", []Seat{seatOf("ana")}, ai.New("usage-authz"))
		freeze(tn, platform.AIApp)
		err := tn.meter(memberOf(t, tn, "ana"), platform.AIUsage{Member: "ana", Model: "fixture/model", Input: 1, At: txnAt})
		denied(err)
		if len(tn.ai.Usage()) != 0 {
			t.Fatal("refused usage changed the meter")
		}
	})
	t.Run("compute and source", func(t *testing.T) {
		owner := &computeStock{stock: newStock("compute-authz")}
		tn := composeTenant(t, "compute-authz", []Seat{seatOf("ana", "stock:clerk")}, owner)
		freeze(tn, "stock")
		_, err := tn.InvokeOperation(memberOf(t, tn, "ana"), platform.OperationRequest{App: "stock", Name: "double", Key: "blocked", Inputs: json.RawMessage(`{"value":4}`)}, txnAt)
		denied(err)
		if len(tn.outbound) != 0 || owner.calls != 0 {
			t.Fatal("refused compute reached its executor")
		}
	})
	t.Run("publish composition and host controls", func(t *testing.T) {
		tn := composeTenant(t, "control-authz", []Seat{seatOf("builder", "platform:admin", "build:builder")}, build.New("control-authz"))
		freeze(tn, build.ID)
		h := NewHost(Tokens(map[string]string{"builder-token": "builder"}), tn)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/releases/candidates", strings.NewReader(`{}`))
		request.Header.Set("Authorization", "Bearer builder-token")
		h.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "Authorization denied at deny:") || len(tn.releases.candidates) != 0 {
			t.Fatal("release route skipped the shared decision")
		}
		hostDecision := h.hostAuthorization("builder", true, "POST /v1/host/tenants/{tenant}/lifecycle", tn.ID)
		if hostDecision.Allow || hostDecision.Layers[len(hostDecision.Layers)-1].Name != "verb" {
			t.Fatal("tenant administrator became a host operator")
		}
	})
}

func TestMemberProjectionFiltersRowsAndUndeclaredComments(t *testing.T) {
	tn := stockTenant(t, relations.New("t-1"))
	for _, who := range []string{"ana", "bo"} {
		decide(t, tn, who, "stock", "stock.item.create", "stock.item", who, map[string]any{"name": who, "line": "L1"}, txnAt)
	}
	rows, _, err := platform.Find[Item](tn.automation("stock", false), platform.Query{})
	if err != nil || len(rows) != 2 {
		t.Fatal("fixture owner did not read both rows")
	}
	projection := tn.narrowed(memberOf(t, tn, "ana"), rows, txnAt, false).([]Item)
	if len(projection) != 1 || projection[0].ID != "ana" {
		t.Fatal("named projection escaped the original row scope")
	}
	decide(t, tn, "ana", relations.ID, relations.SchemaComment, platform.CommentType, "C", map[string]any{"target": "stock.item/ana", "text": "Undeclared commentary"}, txnAt)
	view, refusal := tn.RecordOf(memberOf(t, tn, "ana"), "stock.item", "ana", txnAt)
	if refusal != nil || len(view.Comments) != 1 || strings.Contains(string(platform.Raw(view.Comments)), "Undeclared commentary") {
		t.Fatal("undeclared comment escaped the derived detail")
	}
	original, refusal := tn.RecordOf(memberOf(t, tn, "ana"), platform.CommentType, "C", txnAt)
	if refusal != nil || !strings.Contains(string(platform.Raw(original.Record)), "Undeclared commentary") {
		t.Fatal("authoritative comment bytes became unreadable")
	}
}

func TestAuthorizationRejectsInventedPrincipalsAndKeepsSecondaryRoles(t *testing.T) {
	owner := &computeStock{stock: newStock("identity-authz")}
	tn := composeTenant(t, "identity-authz", []Seat{{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "lead"}, Grants: []platform.Grant{{App: "stock", Role: "lead"}, {App: "stock", Role: "clerk"}}}}}, owner)
	ghost := platform.Member{ID: "unregistered", Tenant: tn.ID, Roles: map[string]string{"stock": "clerk"}}
	c := platform.NewCaller(runtime{tn}, ghost, "stock", false, false)
	decision := c.Authorize(platform.AuthzRequest{Principal: ghost.ID, Tenant: tn.ID, Kind: "read", Type: "stock.item"}, txnAt)
	if decision.Allow || decision.Layers[len(decision.Layers)-1].Name != "principal" {
		t.Fatal("a fabricated principal passed authorization")
	}
	call, refusal := tn.InvokeOperation(memberOf(t, tn, "ana"), platform.OperationRequest{App: "stock", Name: "double", Key: "secondary", Inputs: json.RawMessage(`{"value":4}`)}, txnAt)
	if refusal != nil || call.ID == "" {
		t.Fatalf("secondary operation role was discarded: %v", refusal)
	}
}

func TestHostHTTPRetainsItsExplicitAuthorityAndRecoversWithoutAuthorization(t *testing.T) {
	buildTenant := func() *Tenant {
		return composeTenant(t, "host-authz", []Seat{seatOf("tenant-admin", "platform:admin")})
	}
	tn := buildTenant()
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	h := NewHost(Tokens(map[string]string{"host-token": "operator", "tenant-token": "tenant-admin"}), tn)
	h.HostAdmins = map[string]bool{"operator": true}
	handler := h.Handler()
	call := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/host/tenants/host-authz/lifecycle", strings.NewReader(`{"action":"suspend","reason":"maintenance"}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := call("tenant-token"); response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "ERROR_CODE_POLICY_DENIED") || tn.console.suspended() {
		t.Fatal("tenant rights changed host control")
	}
	if response := call("host-token"); response.Code != http.StatusOK || !tn.console.suspended() || len(entries) != 1 {
		t.Fatalf("explicit host authority did not commit: %d %s", response.Code, response.Body.String())
	}
	saved, err := decodeAcceptedHostControl(entries[0].Body)
	if err != nil || len(saved.Authorizations) != 1 || saved.Authorizations[0].Request.Principal != "host:operator" || !slices.Contains(saved.Authorizations[0].Roles, "host-admin") {
		t.Fatal("host result omitted its original authority")
	}
	recovered := buildTenant()
	recovered.app(PlatformApp).Manifest().Actions.UsePolicies(func() []authz.Policy { t.Fatal("host recovery reauthorized"); return nil })
	if err := recovered.Replay(entries); err != nil || !recovered.console.suspended() {
		t.Fatal("recorded host authority was not restored")
	}
}
