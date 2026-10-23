package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/enterprise"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/internal/construction"
	"platformserver/journal"
	"platformserver/platform"
)

type declaredFixtureOptions struct {
	Organization *platform.OrgSeed
	Entries      *[]Entry
	Channel      http.Handler
	Lodging      http.Handler
	Production   http.Handler
	Activator    string
	Observations http.Handler
	Supplier     http.Handler
	Apps         []platform.App
	Journal      *journal.Postgres
}

func composeDeclaredTenant(t *testing.T, id string, seats []Seat, extra ...platform.App) *Tenant {
	t.Helper()
	apps := append([]platform.App{build.New(id), core.New(id), work.New(id), flow.New(id), ai.New(id), NewAgents(id)}, extra...)
	hasEnterprise := false
	for _, app := range extra {
		if app.Manifest().ID == enterprise.ID {
			hasEnterprise = true
		}
	}
	if !hasEnterprise {
		apps = append(apps, enterprise.New(id, platform.OrgSeed{}))
	}
	return composeTenant(t, id, seats, apps...)
}

func declaredIndustryDrafts(t *testing.T, id, file string, seats []Seat, at time.Time, options ...declaredFixtureOptions) (*Tenant, []construction.JointDraftRef) {
	t.Helper()
	var option declaredFixtureOptions
	if len(options) > 0 {
		option = options[0]
	}
	if option.Organization != nil {
		option.Apps = append(option.Apps, enterprise.New(id, *option.Organization))
	}
	tn := composeDeclaredTenant(t, id, seats, option.Apps...)
	if option.Journal != nil {
		ctx := context.Background()
		if _, err := option.Journal.Entries(ctx, id, 0); err != nil {
			t.Fatal(err)
		}
		tn.attachJournal(ctx, option.Journal)
		if err := tn.migrateRuntimeState(ctx, option.Journal); err != nil {
			t.Fatal(err)
		}
	}
	if option.Entries != nil {
		tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
			*option.Entries = append(*option.Entries, entry)
			return entry.Body, nil
		}
	}
	raw, err := os.ReadFile(filepath.Join("../../deploy/packages", file+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var descriptor PackageDescriptor
	if err = json.Unmarshal(raw, &descriptor); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err = os.WriteFile(filepath.Join(directory, file+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "builder", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
	drafts, err := tn.CreatePackageDrafts(memberOf(t, tn, "builder"), descriptor.ID, descriptor.Version, "industry-drafts", at)
	if err != nil {
		t.Fatal(err)
	}
	configureDeclaredModel(t, tn, at)
	for _, draft := range drafts.Drafts {
		if draft.Ref.Kind == platform.AssetEvaluation {
			decide(t, tn, "builder", "build", "build.testplan.edit", construction.TestPlanType, draft.ID, map[string]any{"model": "fixture/probe"}, at)
		}
	}
	// The fixture configures the same empty portable Connection a builder edits.
	if file == "hospitality" {
		feed := option.Channel
		if feed == nil {
			feed = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"messages":[]}`))
			})
		}
		server := httptest.NewServer(feed)
		t.Cleanup(server.Close)
		decide(t, tn, "builder", "build", "build.connection.edit", construction.ConnectionType, "hotelchannel", map[string]any{"address": server.URL, "allowPrivate": true}, at)
		decide(t, tn, "builder", "build", "build.connection.check", construction.ConnectionType, "hotelchannel", nil, at)
		lodging := option.Lodging
		if lodging == nil {
			lodging = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				WriteJSON(w, http.StatusOK, map[string]any{"records": []any{}})
			})
		}
		provider := httptest.NewServer(lodging)
		t.Cleanup(provider.Close)
		tn.Secrets = func(name string) ([]byte, bool) { return []byte("Bearer lodging-service"), name == "lodging-upstream" }
		decide(t, tn, "builder", "build", "build.connection.edit", construction.ConnectionType, "lodgingupstream", map[string]any{"address": provider.URL, "secret": "lodging-upstream", "allowPrivate": true}, at)
		decide(t, tn, "builder", "build", "build.connection.check", construction.ConnectionType, "lodgingupstream", nil, at)
		tn.CheckConnections(at)
	}
	if file == "manufacturing" {
		supplier := option.Supplier
		if supplier == nil {
			supplier = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, http.StatusOK, map[string]any{}) })
		}
		supplierAgent := httptest.NewServer(supplier)
		t.Cleanup(supplierAgent.Close)
		decide(t, tn, "builder", "build", "build.connection.edit", construction.ConnectionType, "supplieragent", map[string]any{"address": supplierAgent.URL, "allowPrivate": true}, at)
		decide(t, tn, "builder", "build", "build.connection.check", construction.ConnectionType, "supplieragent", nil, at)
		observations := option.Observations
		if observations == nil {
			observations = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, http.StatusOK, map[string]any{}) })
		}
		stateStore := httptest.NewServer(observations)
		t.Cleanup(stateStore.Close)
		decide(t, tn, "builder", "build", "build.connection.edit", construction.ConnectionType, "mesobservations", map[string]any{"address": stateStore.URL, "allowPrivate": true}, at)
		decide(t, tn, "builder", "build", "build.connection.check", construction.ConnectionType, "mesobservations", nil, at)
		upstream := option.Production
		if upstream == nil {
			upstream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "No production provider configured in this fixture"})
					return
				}
				WriteJSON(w, http.StatusOK, map[string]any{"records": []any{}})
			})
		}
		provider := httptest.NewServer(upstream)
		t.Cleanup(provider.Close)
		tn.Secrets = func(name string) ([]byte, bool) {
			return []byte("Bearer production-service"), name == "production-upstream"
		}
		decide(t, tn, "builder", "build", "build.connection.edit", construction.ConnectionType, "productionupstream", map[string]any{"address": provider.URL, "secret": "production-upstream", "allowPrivate": true}, at)
		decide(t, tn, "builder", "build", "build.connection.check", construction.ConnectionType, "productionupstream", nil, at)
		tn.CheckConnections(at)
	}
	selection := make([]construction.JointDraftRef, 0, len(drafts.Drafts))
	for _, draft := range drafts.Drafts {
		selection = append(selection, construction.JointDraftRef{Kind: draft.Ref.Kind, ID: draft.ID})
	}
	return tn, selection
}

func declaredIndustryTenant(t *testing.T, id, file string, seats []Seat, at time.Time, options ...declaredFixtureOptions) *Tenant {
	t.Helper()
	tn, selection := declaredIndustryDrafts(t, id, file, seats, at, options...)
	preview, err := tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("industry candidate: %v %s", err, preview.Diagnostic)
	}
	candidate, err := tn.SaveReleaseCandidates(memberOf(t, tn, "builder"), selection, preview.CandidateID, "industry-candidate", at)
	if err != nil {
		t.Fatal(err)
	}
	measureDeclaredCandidate(t, tn, candidate, at)
	activator := "builder"
	if len(options) > 0 && options[0].Activator != "" {
		activator = options[0].Activator
	}
	if _, err = tn.ActivateRelease(memberOf(t, tn, activator), candidate, "industry-activation", at); err != nil {
		t.Fatal(err)
	}
	return tn
}

// The fixed provider verifies the original measured-call and activation paths.
// It is not evidence of production model quality or explanation usefulness.
func configureDeclaredModel(t *testing.T, tn *Tenant, at time.Time) {
	t.Helper()
	decide(t, tn, "builder", PlatformApp, SchemaGrant, MemberType, "builder", map[string]any{"app": ai.ID, "role": ai.Admin}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "fixture", map[string]any{"kind": "local", "baseUrl": "http://localhost:1", "wire": "openai"}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]any{"access": "users"}, at)
	for _, setting := range []string{"ai/app-model", "agent/model"} {
		decide(t, tn, "builder", PlatformApp, SchemaSettingSet, SettingType, setting, map[string]any{"value": "fixture/probe"}, at)
	}
	tn.AIClient = func(request *http.Request) (*http.Response, error) {
		var body struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		message := map[string]any{"content": `{"summary":"Review the actual record and confirm missing facts with the responsible person.","category":"review","review":true,"confidence":0.9}`}
		if len(body.Tools) > 0 {
			message = map[string]any{"content": "", "tool_calls": []any{map[string]any{"id": "ask-target", "type": "function", "function": map[string]any{"name": "ask", "arguments": `{"question":"Which confirmed record and facts should I review?","answers":["Provide the target"],"rationale":"A confirmed target is required before a business change."}`}}}}
		}
		raw := platform.Raw(map[string]any{"model": "probe", "choices": []any{map[string]any{"message": message}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "cost": 0.001}})
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	}
}

func measureDeclaredCandidate(t *testing.T, tn *Tenant, candidateID string, at time.Time) {
	t.Helper()
	candidate, err := tn.releases.candidate(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range candidate.Assets {
		request := ReleaseEvaluationRequest{CandidateID: candidateID, Key: "measure:" + candidateID + ":" + asset.Ref.Name}
		switch asset.Ref.Kind {
		case platform.AssetEvaluation:
			request.PlanID = asset.Ref.Name
		case platform.AssetAgent:
			request.Agent = asset.Ref.App + "." + asset.Ref.Name
		default:
			continue
		}
		id, problem := tn.EvaluateRelease(memberOf(t, tn, "builder"), request, at)
		if problem != nil {
			t.Fatalf("candidate measurement %s: %v", asset.Ref, problem)
		}
		if request.Agent != "" {
			tn.Evaluate(at)
			continue
		}
		for _, effect := range tn.Effects(at) {
			var ask modelAsk
			if settled(effect.State) || json.Unmarshal([]byte(effect.Body), &ask) != nil || !ask.Evaluation || !strings.HasPrefix(ask.Call, id+":") {
				continue
			}
			outcome, usage := tn.sendModel(effect, at)
			if outcome.Result != "delivered" || usage == nil {
				t.Fatalf("measured model call: %+v", outcome)
			}
			tn.settleWithUsage(effect.ID, outcome, usage, at)
		}
	}
	gate := func() error {
		tn.mu.Lock()
		defer tn.mu.Unlock()
		return tn.evaluationsReadyLocked(candidateID, candidate.Bytes)
	}
	if err := gate(); err != nil {
		t.Fatalf("candidate measurement did not satisfy the original gate: %v", err)
	}
}

func TestDeclaredHospitalityReservations(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	var entries []Entry
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("manager", "build:hotelmanager"), seatOf("desk", "build:desk"), seatOf("outsider")}
	tn := declaredIndustryTenant(t, "hospitality-declared", "hospitality", seats, at, declaredFixtureOptions{Entries: &entries})
	decide(t, tn, "manager", "build", "build.pmsroomtype.create", "build.pmsroomtype", "daily", map[string]any{"name": "Daily rooms", "unit": "day", "capacity": 1, "minunits": 1, "allowance": 0, "oversell": false, "active": true}, at)
	create := func(id, guest string) {
		t.Helper()
		decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", id, map[string]any{"guest": guest, "roomtype": "daily", "unit": "day", "mode": "booked", "dayfrom": "2026-10-22", "dayuntil": "2026-10-24"}, at)
	}
	create("one", "First guest")
	before := txnState(tn)
	if got := refuse(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "over", map[string]any{"guest": "Second guest", "roomtype": "daily", "unit": "day", "mode": "booked", "dayfrom": "2026-10-21", "dayuntil": "2026-10-23"}, at); got == "ok" || txnState(tn) != before {
		t.Fatalf("late capacity refusal was not atomic: %s", got)
	}
	occupied, failure := tn.Records(memberOf(t, tn, "desk"), "build.pmsslot", platform.Query{Domain: platform.Raw([]any{[]any{"used", ">", 0}}), Limit: 100}, at)
	if failure != nil || occupied.Total != 2 {
		t.Fatalf("occupied slots: %+v %v", occupied, failure)
	}
	for _, value := range occupied.Records {
		var row struct{ ID string }
		_ = json.Unmarshal(platform.Raw(value), &row)
		if got := refuse(t, tn, "desk", "build", "build.pmsslot.edit", "build.pmsslot", row.ID, map[string]any{"used": 0}, at); got == "ok" {
			t.Fatal("raw slot edit bypassed actual occupancy")
		}
	}
	if got := refuse(t, tn, "outsider", "build", "build.pmsreservation.cancel", "build.pmsreservation", "one", nil, at); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("ungranted cancel: %s", got)
	}
	decide(t, tn, "desk", "build", "build.pmsreservation.cancel", "build.pmsreservation", "one", nil, at)
	create("two", "Second guest")

	// An amendment retains overlap, rolls back old-only releases on a later
	// failure, and can move between room types and booking units.
	change := func(id, room, unit, from, until string) map[string]any {
		values := map[string]any{"roomtype": room, "unit": unit}
		if unit == "day" {
			values["dayfrom"], values["dayuntil"] = from, until
		} else {
			values["fromat"], values["untilat"] = from, until
		}
		return values
	}
	decide(t, tn, "desk", "build", "build.pmsreservation.modify", "build.pmsreservation", "two", change("two", "daily", "day", "2026-10-23", "2026-10-25"), at)
	decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "later", map[string]any{"guest": "Later guest", "roomtype": "daily", "unit": "day", "mode": "booked", "dayfrom": "2026-10-26", "dayuntil": "2026-10-27"}, at)
	before = txnState(tn)
	if got := refuse(t, tn, "desk", "build", "build.pmsreservation.modify", "build.pmsreservation", "two", change("two", "daily", "day", "2026-10-24", "2026-10-27"), at); got == "ok" || txnState(tn) != before {
		t.Fatalf("failed amendment retained an earlier interval delta: %s", got)
	}
	decide(t, tn, "manager", "build", "build.pmsroomtype.create", "build.pmsroomtype", "hourly", map[string]any{"name": "Hourly rooms", "unit": "hour", "capacity": 1, "minunits": 2, "allowance": 1, "oversell": false, "active": true}, at)
	decide(t, tn, "desk", "build", "build.pmsreservation.modify", "build.pmsreservation", "two", change("two", "hourly", "hour", "2026-10-21T12:00:00Z", "2026-10-21T14:00:00Z"), at)
	hourly := map[string]any{"guest": "Meeting", "roomtype": "hourly", "unit": "hour", "mode": "booked", "fromat": "2026-10-21T13:00:00Z", "untilat": "2026-10-21T15:00:00Z"}
	before = txnState(tn)
	if got := refuse(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "meeting", hourly, at); got == "ok" || txnState(tn) != before {
		t.Fatalf("hourly physical capacity bypassed: %s", got)
	}
	decide(t, tn, "manager", "build", "build.pmsroomtype.edit", "build.pmsroomtype", "hourly", map[string]any{"oversell": true}, at)
	decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "meeting", hourly, at)
	if got := refuse(t, tn, "manager", "build", "build.pmsroomtype.edit", "build.pmsroomtype", "hourly", map[string]any{"oversell": false}, at); got == "ok" {
		t.Fatal("disabling overbooking silently violated existing occupied capacity")
	}
	hourly["fromat"], hourly["untilat"] = "2026-10-21T16:00:00Z", "2026-10-21T17:00:00Z"
	if got := refuse(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "short", hourly, at); got == "ok" {
		t.Fatal("minimum stay bypassed")
	}
	hourly["fromat"], hourly["untilat"] = "2026-10-21T16:30:00Z", "2026-10-21T18:30:00Z"
	if got := refuse(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "unaligned", hourly, at); got == "ok" {
		t.Fatal("unaligned hourly interval accepted")
	}
	decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "hold", map[string]any{"guest": "Held guest", "roomtype": "daily", "unit": "day", "mode": "held", "dayfrom": "2026-10-22", "dayuntil": "2026-10-23", "expires": "2026-10-21T10:00:00Z"}, at)
	if got := refuse(t, tn, "desk", "build", "build.pmsreservation.confirm", "build.pmsreservation", "hold", nil, at.Add(2*time.Hour)); got == "ok" {
		t.Fatal("expired hold confirmed")
	}
	decide(t, tn, "desk", "build", "build.pmsreservation.release", "build.pmsreservation", "hold", nil, at.Add(2*time.Hour))
	if got := refuse(t, tn, "desk", "build", "build.pmsreservation.confirm", "build.pmsreservation", "hold", nil, at.Add(2*time.Hour)); got == "ok" {
		t.Fatal("released hold confirmed")
	}
	laterAt := at.Add(2 * time.Hour)
	decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "confirmable", map[string]any{"guest": "Confirmed guest", "roomtype": "daily", "unit": "day", "mode": "held", "dayfrom": "2026-10-22", "dayuntil": "2026-10-23", "expires": "2026-10-21T13:00:00Z"}, laterAt)
	decide(t, tn, "desk", "build", "build.pmsreservation.confirm", "build.pmsreservation", "confirmable", nil, laterAt)
	decide(t, tn, "manager", "build", "build.pmsroomtype.prepare", "build.pmsroomtype", "daily", map[string]any{"fromat": "2026-12-01T00:00:00Z", "untilat": "2026-12-03T00:00:00Z"}, laterAt)
	decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", "future", map[string]any{"guest": "Future guest", "roomtype": "daily", "unit": "day", "mode": "booked", "dayfrom": "2026-12-01", "dayuntil": "2026-12-03"}, laterAt)
	recovered := composeDeclaredTenant(t, tn.ID, seats)
	if err := recovered.Replay(entries); err != nil || snapshot(tn) != snapshot(recovered) {
		t.Fatalf("declared hospitality recovery changed the accepted model or occupied interval: %v", err)
	}

}

func TestDeclaredHospitalitySalesScope(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tn := declaredIndustryTenant(t, "sales-declared", "hospitality", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("sales", "build:sales"), seatOf("other", "build:sales"), seatOf("manager", "build:salesmanager"), seatOf("outsider")}, at)
	decide(t, tn, "sales", "build", "build.crmcustomer.create", "build.crmcustomer", "customer", map[string]any{"name": "Group customer", "kind": "company", "contact": "Coordinator"}, at)
	decide(t, tn, "sales", "build", "build.crmcustomer.edit", "build.crmcustomer", "customer", map[string]any{"phone": "5550100"}, at)
	for _, owner := range []string{"sales", "other"} {
		decide(t, tn, owner, "build", "build.crmopportunity.create", "build.crmopportunity", owner, map[string]any{"customer": "customer", "title": "Group event"}, at)
	}
	for _, scope := range []struct {
		member string
		count  int
	}{{"sales", 1}, {"other", 1}, {"manager", 2}} {
		page, err := tn.Records(memberOf(t, tn, scope.member), "build.crmopportunity", platform.Query{Limit: 100}, at)
		if err != nil || page.Total != scope.count {
			t.Fatalf("sales scope %s: total=%d error=%v", scope.member, page.Total, err)
		}
	}
	if got := refuse(t, tn, "other", "build", "build.crmopportunity.win", "build.crmopportunity", "sales", nil, at); got == "ok" {
		t.Fatal("another salesperson closed an unowned opportunity")
	}
	if got := refuse(t, tn, "sales", "build", "build.crmopportunity.edit", "build.crmopportunity", "sales", map[string]any{"margin": 25}, at); got == "ok" {
		t.Fatal("salesperson wrote the manager's margin")
	}
	decide(t, tn, "manager", "build", "build.crmopportunity.edit", "build.crmopportunity", "sales", map[string]any{"margin": 25}, at)
	salesCSV, err := tn.Export(memberOf(t, tn, "sales"), "build.crmopportunity", platform.Query{}, at)
	if err != nil || strings.Contains(string(salesCSV), "margin") {
		t.Fatalf("sales export disclosed margin: %s %v", salesCSV, err)
	}
	decide(t, tn, "sales", "build", "build.crmopportunity.win", "build.crmopportunity", "sales", nil, at)
	tn.Work(at.Add(time.Second))
	if declaredRecord(t, tn, "sales", "build.crmopportunity", "sales", at)["state"] != "won" {
		t.Fatal("an opportunity without group rooms did not finish its win")
	}
	if got := refuse(t, tn, "sales", "build", "build.crmopportunity.lose", "build.crmopportunity", "sales", nil, at); got == "ok" {
		t.Fatal("won opportunity closed again")
	}
	decide(t, tn, "manager", "build", "build.crmopportunity.lose", "build.crmopportunity", "other", nil, at)
	if got := refuse(t, tn, "outsider", "build", "build.crmcustomer.create", "build.crmcustomer", "unauthorized", map[string]any{"name": "No grant", "kind": "company"}, at); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("customer grant: %s", got)
	}
	declaredAgentDraft(t, tn, "sales", "SALES", "build.salesassistant", "build.crmopportunity.create", "drafted", map[string]any{"customer": "customer", "title": "Human-confirmed group event"}, at)
	if _, refusal := tn.RecordOf(memberOf(t, tn, "sales"), "build.crmopportunity", "drafted", at); refusal == nil {
		t.Fatal("the sales assistant created an opportunity before confirmation")
	}
	decide(t, tn, "sales", AgentApp, SchemaRunConfirm, RunType, "SALES", map[string]any{}, at)
	if got := declaredRecord(t, tn, "sales", "build.crmopportunity", "drafted", at); got["title"] != "Human-confirmed group event" || got["state"] != "open" {
		t.Fatalf("the original sales action did not accept the confirmed tool: %s", platform.Raw(got))
	}
}

func TestDeclaredHospitalityHoldDeadlineRecovery(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin", "flow:admin"), seatOf("manager", "build:hotelmanager"), seatOf("desk", "build:desk")}
	var entries []Entry
	tn := declaredIndustryTenant(t, "hold-deadlines", "hospitality", seats, at, declaredFixtureOptions{Entries: &entries})
	decide(t, tn, "manager", "build", "build.pmsroomtype.create", "build.pmsroomtype", "daily", map[string]any{"name": "Daily", "unit": "day", "capacity": 3, "minunits": 1, "allowance": 0, "oversell": false, "active": true}, at)
	for _, id := range []string{"expires", "alsoexpires", "confirmed"} {
		decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", id, map[string]any{"guest": id, "roomtype": "daily", "unit": "day", "mode": "held", "dayfrom": "2026-10-22", "dayuntil": "2026-10-23", "expires": "2026-10-21T10:00:00Z"}, at)
	}
	tn.Work(at.Add(time.Second))
	var instances []flow.FlowInstance
	for _, instance := range platform.Records[flow.FlowInstance](tn.automation("flow", false)) {
		if instance.Flow == "build.pmsexpiry" {
			instances = append(instances, instance)
		}
	}
	if len(instances) != 3 {
		t.Fatalf("deadline instances: %+v", instances)
	}
	for _, instance := range instances {
		if instance.State != "waiting" || len(instance.Tokens) != 1 || !instance.Tokens[0].Due.Equal(at.Add(time.Hour)) {
			t.Fatalf("deadline was not captured: %+v", instance)
		}
	}
	decide(t, tn, "desk", "build", "build.pmsreservation.confirm", "build.pmsreservation", "confirmed", nil, at.Add(2*time.Second))
	recovered := composeDeclaredTenant(t, tn.ID, seats)
	if err := recovered.Replay(entries); err != nil || snapshot(tn) != snapshot(recovered) {
		t.Fatalf("waiting hold recovery: %v", err)
	}
	recovered.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	recovered.Work(at.Add(time.Hour - time.Second))
	view, refusal := recovered.RecordOf(memberOf(t, recovered, "desk"), "build.pmsreservation", "expires", at.Add(time.Hour-time.Second))
	if refusal != nil || !strings.Contains(string(platform.Raw(view.Record)), `"state":"held"`) {
		t.Fatalf("hold expired early: %s %v", platform.Raw(view.Record), refusal)
	}
	for i := 0; i < 3; i++ {
		runJob(t, recovered, "flow", "timers", at.Add(time.Hour+time.Duration(i)*time.Second))
		recovered.Work(at.Add(time.Hour + time.Duration(i)*time.Second))
	}
	for _, result := range []struct{ id, state string }{{"expires", "released"}, {"alsoexpires", "released"}, {"confirmed", "booked"}} {
		view, refusal := recovered.RecordOf(memberOf(t, recovered, "desk"), "build.pmsreservation", result.id, at.Add(2*time.Hour))
		if refusal != nil || !strings.Contains(string(platform.Raw(view.Record)), `"state":"`+result.state+`"`) {
			t.Fatalf("deadline result %s: %s %v flows=%s fault=%v", result.id, platform.Raw(view.Record), refusal, platform.Raw(platform.Records[flow.FlowInstance](recovered.automation("flow", false))), recovered.fault.Load())
		}
	}
	slots, refusal := recovered.Records(memberOf(t, recovered, "desk"), "build.pmsslot", platform.Query{Domain: platform.Raw([]any{[]any{"used", ">", 0}})}, at.Add(2*time.Hour))
	if refusal != nil || slots.Total != 1 || !strings.Contains(string(platform.Raw(slots.Records)), `"used":1`) {
		t.Fatalf("expiry released another hold's capacity: %+v %v", slots, refusal)
	}
	final := composeDeclaredTenant(t, tn.ID, seats)
	if err := final.Replay(entries); err != nil || snapshot(recovered) != snapshot(final) {
		t.Fatalf("accepted expiry recovery: %v", err)
	}
}

func TestDeclaredHospitalityChannelSourceAndRecovery(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	ingestor := seatOf("ingestor", "build:publisher")
	ingestor.Member.Grants = append(ingestor.Member.Grants, platform.Grant{App: "ai", Role: "user"})
	for _, role := range []string{"publisher", "integrator", "channel"} {
		ingestor.Member.Grants = append(ingestor.Member.Grants, platform.Grant{App: "build", Role: role})
	}
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("manager", "build:hotelmanager"), seatOf("desk", "build:desk"), ingestor}
	rows := []map[string]any{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": rows})
	})
	var entries []Entry
	tn := declaredIndustryTenant(t, "channel-declared", "hospitality", seats, at, declaredFixtureOptions{Entries: &entries, Channel: handler, Activator: "ingestor"})
	files := &memoryFiles{}
	tn.Files = files
	decide(t, tn, "manager", "build", "build.pmsroomtype.create", "build.pmsroomtype", "daily", map[string]any{"name": "Daily", "unit": "day", "capacity": 1, "minunits": 1, "allowance": 0, "oversell": false, "active": true}, at)
	rows = []map[string]any{{"key": "channel-a-event-1", "channel": "channel-a", "externalid": "event-1", "reservation": "channel-a-booking-1", "kind": "reserve", "unit": "day", "guest": "Channel guest", "roomtype": "daily", "dayfrom": "2026-10-22", "dayuntil": "2026-10-23"}}
	tn.PullSources(at)
	source, known := platform.Get[construction.Source](tn.automation("build", false), "hotelchannelinput")
	if !known || source.Last == nil || source.Last.Applied != 1 || source.Last.Failed != 0 || source.Puller != "ingestor" {
		t.Fatalf("channel source did not accept its frozen input as the publisher: %+v", source)
	}
	before := txnState(tn)
	fail := true
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail && e.App == "flow" {
			fail = false
			return nil, errors.New("injected channel acceptance failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	tn.Round(at.Add(time.Second), 1)
	if fail || txnState(tn) != before {
		t.Fatal("failed channel acceptance exposed a reservation or occupancy")
	}
	tn.Work(at.Add(2 * time.Second))
	booking, refusal := tn.RecordOf(memberOf(t, tn, "desk"), "build.pmsreservation", "channel-a-booking-1", at.Add(2*time.Second))
	if refusal != nil || !strings.Contains(string(platform.Raw(booking.Record)), `"state":"booked"`) {
		t.Fatalf("channel booking not processed: %s %v tasks=%s", platform.Raw(booking.Record), refusal, platform.Raw(tn.Tasks()))
	}
	notificationCount := func(tn *Tenant) int {
		count := 0
		for _, task := range platform.Records[work.WorkTask](tn.automation("work", false)) {
			if task.Ref == "build.pmschannelmessage/channel-a-event-1" {
				count++
			}
		}
		return count
	}
	if notificationCount(tn) != 1 {
		t.Fatal("channel completion did not create one original Work notification")
	}
	decide(t, tn, "ingestor", "build", "build.source.pull", construction.SourceType, "hotelchannelinput", nil, at.Add(3*time.Second))
	tn.PullSources(at.Add(3 * time.Second))
	tn.Work(at.Add(4 * time.Second))
	occupied, refusal := tn.Records(memberOf(t, tn, "desk"), "build.pmsoccupancy", platform.Query{}, at.Add(4*time.Second))
	if refusal != nil || occupied.Total != 1 || notificationCount(tn) != 1 {
		t.Fatalf("repeated channel message duplicated capacity or notification: %+v %v", occupied, refusal)
	}

	original := rows[0]
	full := map[string]any{}
	for key, value := range original {
		full[key] = value
	}
	full["key"], full["externalid"], full["reservation"] = "channel-a-full", "event-full", "unavailable-booking"
	rows = []map[string]any{full}
	decide(t, tn, "ingestor", "build", "build.source.pull", construction.SourceType, "hotelchannelinput", nil, at.Add(4500*time.Millisecond))
	tn.PullSources(at.Add(4500 * time.Millisecond))
	tn.Work(at.Add(4600 * time.Millisecond))
	failedMessage, refusal := tn.RecordOf(memberOf(t, tn, "manager"), "build.pmschannelmessage", "channel-a-full", at.Add(4600*time.Millisecond))
	if refusal != nil || !strings.Contains(string(platform.Raw(failedMessage.Record)), `"state":"failed"`) {
		t.Fatalf("channel capacity refusal was not retained for review: %s %v", platform.Raw(failedMessage.Record), refusal)
	}
	if _, refusal := tn.RecordOf(memberOf(t, tn, "desk"), "build.pmsreservation", "unavailable-booking", at.Add(4600*time.Millisecond)); refusal == nil {
		t.Fatal("failed channel booking left a reservation")
	}
	reviews := 0
	for _, task := range platform.Records[work.WorkTask](tn.automation("work", false)) {
		if task.Ref == "build.pmschannelmessage/channel-a-full" && task.State == "open" {
			reviews++
		}
	}
	if reviews != 1 {
		t.Fatalf("channel business refusal left %d review tasks", reviews)
	}
	rows = []map[string]any{original}
	rows[0]["guest"] = "Changed under original external identity"
	decide(t, tn, "ingestor", "build", "build.source.pull", construction.SourceType, "hotelchannelinput", nil, at.Add(5*time.Second))
	tn.PullSources(at.Add(5 * time.Second))
	source, _ = platform.Get[construction.Source](tn.automation("build", false), "hotelchannelinput")
	if source.Last == nil || source.Last.Failed != 1 {
		t.Fatalf("rewritten channel message was accepted: %+v", source.Last)
	}
	rows = []map[string]any{{"key": "channel-a-event-2", "channel": "channel-a", "externalid": "event-2", "reservation": "channel-a-booking-1", "kind": "cancel", "unit": "command"}}
	decide(t, tn, "ingestor", "build", "build.source.pull", construction.SourceType, "hotelchannelinput", nil, at.Add(6*time.Second))
	tn.PullSources(at.Add(6 * time.Second))
	tn.Work(at.Add(7 * time.Second))
	booking, refusal = tn.RecordOf(memberOf(t, tn, "desk"), "build.pmsreservation", "channel-a-booking-1", at.Add(7*time.Second))
	if refusal != nil || !strings.Contains(string(platform.Raw(booking.Record)), `"state":"canceled"`) {
		t.Fatalf("channel cancellation did not use the original booking action: %s %v", platform.Raw(booking.Record), refusal)
	}
	recovered := composeDeclaredTenant(t, tn.ID, seats)
	recovered.Files = files
	if err := recovered.Replay(entries); err != nil || snapshot(tn) != snapshot(recovered) {
		t.Fatalf("frozen channel recovery changed accepted messages or booking results: %v", err)
	}
}

func TestDeclaredJointQueryStillRejectsInvalidFieldsBeforePublication(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tn, selection := declaredIndustryDrafts(t, "joint-query-fields", "hospitality", []Seat{seatOf("builder", "build:builder", "platform:admin")}, at)
	decide(t, tn, "builder", "build", "build.query.edit", construction.QueryType, "pmsarrivals", map[string]any{"domain": []any{[]any{"notafield", "=", "booked"}}}, at)
	preview, err := tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), selection)
	if err == nil && preview.Diagnostic == "" {
		t.Fatal("joint query skipped field validation in the private complete candidate")
	}
	if _, installed := tn.entity("build.pmsreservation"); installed || tn.ActiveRelease() != "" {
		t.Fatal("invalid query partially published its object or application")
	}
	decide(t, tn, "builder", "build", "build.query.edit", construction.QueryType, "pmsarrivals", map[string]any{"domain": []any{[]any{"state", "=", "booked"}}}, at)
	preview, err = tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), selection)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("repaired complete candidate refused: %v %s", err, preview.Diagnostic)
	}
}

func TestDeclaredHospitalityNoticesStayWithOriginalWork(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tn := declaredIndustryTenant(t, "hospitality-notices", "hospitality", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("manager", "build:hotelmanager"), seatOf("desk", "build:desk")}, at)
	decide(t, tn, "manager", "build", "build.pmsroomtype.create", "build.pmsroomtype", "daily", map[string]any{"name": "Daily", "unit": "day", "capacity": 1, "minunits": 1, "allowance": 1, "oversell": true, "active": true}, at)
	for _, id := range []string{"physical", "overbooked"} {
		decide(t, tn, "desk", "build", "build.pmsreservation.create", "build.pmsreservation", id, map[string]any{"guest": id, "roomtype": "daily", "unit": "day", "mode": "booked", "dayfrom": "2026-10-22", "dayuntil": "2026-10-23"}, at)
	}
	for _, tick := range []time.Time{at.Add(time.Second), at.Add(time.Hour), at.Add(2 * time.Hour)} {
		tn.Work(tick)
	}
	counts := map[string]int{}
	for _, task := range platform.Records[work.WorkTask](tn.automation("work", false)) {
		counts[task.Title+":"+task.Ref]++
	}
	if counts["Review overbooked room capacity:build.pmsreservation/overbooked"] != 1 || counts["Review overbooked room capacity:build.pmsreservation/physical"] != 0 {
		t.Fatalf("overbooking notices: %+v", counts)
	}
	for _, id := range []string{"physical", "overbooked"} {
		if counts["Review the upcoming guest arrival:build.pmsreservation/"+id] != 1 {
			t.Fatalf("arrival notice was missing or duplicated: %+v", counts)
		}
	}
	decide(t, tn, "desk", "build", "build.pmsreservation.cancel", "build.pmsreservation", "overbooked", nil, at.Add(2*time.Hour))
	for _, task := range platform.Records[work.WorkTask](tn.automation("work", false)) {
		if task.Ref == "build.pmsreservation/overbooked" && task.State == "open" {
			t.Fatalf("cancellation retained an obsolete review task: %+v", task)
		}
	}
}

func TestDeclaredHospitalityPortablePlansUseSelectedBusinessMembers(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	tn := declaredIndustryTenant(t, "hospitality-plan", "hospitality", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("manager", "build:hotelmanager"), seatOf("sales", "build:sales")}, at)
	raw, err := os.ReadFile("../../deploy/packages/hospitality.json")
	if err != nil {
		t.Fatal(err)
	}
	var descriptor PackageDescriptor
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		t.Fatal(err)
	}
	before := snapshot(tn)
	for _, asset := range descriptor.Blueprint.Assets {
		if asset.Ref.Kind != platform.AssetTestPlan {
			continue
		}
		plan, err := platform.ReadTestPlan(asset.Body)
		if err != nil {
			t.Fatal(err)
		}
		actor := "manager"
		if plan.Name == "hospitalitysales" {
			actor = "sales"
		}
		request := CandidateSimulationRequest{Roots: plan.Roots, Actors: []string{actor}, At: plan.At}
		for _, step := range plan.Steps {
			request.Steps = append(request.Steps, SimulationStep{Type: step.Type, ID: step.ID, Action: step.Action, Payload: json.RawMessage(step.Payload), Expect: step.Expect})
		}
		result, refusal := tn.SimulateCandidate(memberOf(t, tn, "builder"), request)
		if refusal != nil {
			t.Fatalf("portable plan %s: %s: %s", plan.Name, refusal.Code, refusal.Message)
		}
		if len(result.Actors) != 1 || !result.Actors[0].Recovered {
			t.Fatalf("portable plan %s: %s", plan.Name, platform.Raw(result))
		}
		for index, step := range result.Actors[0].Steps {
			if step.Accepted != (plan.Steps[index].Expect == "accepted") {
				t.Fatalf("portable plan %s step %d: %s", plan.Name, index+1, platform.Raw(step))
			}
		}
	}
	if snapshot(tn) != before {
		t.Fatal("portable candidate scenarios changed live business data")
	}
}
