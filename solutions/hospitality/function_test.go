package hospitality

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"crm"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/platform"
)

func TestTypedFunctionOnOpportunity(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct{ Messages []platformserver.Message }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var input map[string]any
		if len(request.Messages) != 2 || json.Unmarshal([]byte(request.Messages[1].Content), &input) != nil || len(input) != 2 || input["title"] != "Board offsite" || input["stage"] != "open" {
			t.Errorf("unexpected CRM input: %+v", request)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"Review the offsite","category":"review","review":true}`}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 12}})
	}))
	defer provider.Close()
	compose := func() *platformserver.Tenant {
		tn, err := Compose("hotel-a", nil,
			platformserver.Seat{Subjects: []string{"sales"}, Member: platform.Member{ID: "sales", Roles: map[string]string{crm.ID: string(crm.Sales), ai.ID: ai.Admin, platformserver.PlatformApp: platformserver.Admin}}},
			platformserver.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{crm.ID: string(crm.Sales)}}})
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var journal []platformserver.Entry
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	tn.AcceptResult = func(e platformserver.Entry, _, _ string) ([]byte, error) {
		journal = append(journal, e)
		return e.Body, nil
	}
	sales, _ := tn.Member("sales")
	keys := 0
	submit := func(m platform.Member, app, schema, typ, id string, payload any) bool {
		keys++
		_, err := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: app, IdempotencyKey: fmt.Sprint(keys), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, t0)
		return err == nil
	}
	must := func(app, schema, typ, id string, payload any) {
		t.Helper()
		if !submit(sales, app, schema, typ, id, payload) {
			t.Fatalf("refused %s", schema)
		}
	}
	must(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": provider.URL})
	must(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]string{"access": "users"})
	must(platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType, "ai/app-model", map[string]string{"value": "local/probe"})
	must(crm.ID, crm.SchemaAccount, crm.AccountType, "ACME", map[string]string{"name": "Acme", "kind": "company"})
	must(crm.ID, crm.SchemaOpen, crm.OpportunityType, "OPP-1", map[string]string{"account": "ACME", "title": "Board offsite"})
	other, _ := tn.Member("other")
	if submit(other, crm.ID, crm.SchemaAdvice, crm.OpportunityType, "OPP-1", struct{}{}) || submit(sales, crm.ID, crm.SchemaAdviceAnswer, crm.OpportunityType, "OPP-1", platform.Answer{Outcome: "accepted", Text: `{}`}) {
		t.Fatal("source or automatic reply authorization escaped")
	}
	must(crm.ID, crm.SchemaAdvice, crm.OpportunityType, "OPP-1", struct{}{})
	if calls.Load() != 0 {
		t.Fatal("submission called the provider")
	}
	platformserver.CheckReplay(t, tn, journal, compose)
	tn.Dispatch(t0.Add(time.Second))
	view, err := tn.RecordOf(sales, crm.OpportunityType, "OPP-1", t0.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	o := view.Record.(crm.Opportunity)
	if calls.Load() != 1 || o.AdviceState != "ready" || o.Advice != "Review the offsite" || !o.AdviceReview || o.AdviceCategory != "review" || o.Stage != "open" || o.AdviceDefinition == "" || o.AdviceModel != "local/probe" || len(o.AdviceSources) != 2 {
		t.Fatalf("incorrect advice: %+v calls=%d", o, calls.Load())
	}
	platformserver.CheckReplay(t, tn, journal, compose)
	if calls.Load() != 1 {
		t.Fatal("recovery called the provider")
	}
}
