package manufacturing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"erp"
	"mes"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/platform"
)

func TestTypedFunctionOnOrder(t *testing.T) {
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct{ Messages []platformserver.Message }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var input map[string]any
		if len(request.Messages) != 2 || json.Unmarshal([]byte(request.Messages[1].Content), &input) != nil || len(input) != 3 || input["product"] != "P-100" || input["quantity"] != float64(2) || input["status"] != "released" {
			t.Errorf("unexpected MES input: %+v", request)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"Review the order","category":"review","review":true}`}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 12}})
	}))
	defer provider.Close()
	compose := func() *platformserver.Tenant {
		tn, err := NewTenant(tenant, erp.New(tenant),
			Seat("sup", "sup", map[string]string{mes.ID: string(mes.Supervisor), ai.ID: ai.Admin, platformserver.PlatformApp: platformserver.Admin}, "plant-sz"),
			Seat("op", "op", map[string]string{mes.ID: string(mes.Operator)}, "L1"))
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
	sup, _ := tn.Member("sup")
	keys := 0
	submit := func(m platform.Member, app, schema, typ, id string, payload any) bool {
		keys++
		_, err := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: app, IdempotencyKey: fmt.Sprint(keys), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, now)
		return err == nil
	}
	must := func(app, schema, typ, id string, payload any) {
		t.Helper()
		if !submit(sup, app, schema, typ, id, payload) {
			t.Fatalf("refused %s", schema)
		}
	}
	must(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": provider.URL})
	must(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]string{"access": "users"})
	must(platformserver.PlatformApp, platformserver.SchemaSettingSet, platformserver.SettingType, "ai/app-model", map[string]string{"value": "local/probe"})
	must(mes.ID, mes.SchemaRelease, mes.OrderType, "SO-1", map[string]any{"product": "P-100", "quantity": 2, "sfcs": 1})
	op, _ := tn.Member("op")
	if submit(op, mes.ID, mes.SchemaAdvice, mes.OrderType, "SO-1", struct{}{}) || submit(sup, mes.ID, mes.SchemaAdviceAnswer, mes.OrderType, "SO-1", platform.Answer{Outcome: "accepted", Text: `{}`}) {
		t.Fatal("source or automatic reply authorization escaped")
	}
	must(mes.ID, mes.SchemaAdvice, mes.OrderType, "SO-1", struct{}{})
	if calls.Load() != 0 {
		t.Fatal("submission called the provider")
	}
	platformserver.CheckReplay(t, tn, journal, compose)
	tn.Dispatch(now.Add(time.Second))
	view, err := tn.RecordOf(sup, mes.OrderType, "SO-1", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	o := view.Record.(mes.Order)
	if calls.Load() != 1 || o.AdviceState != "ready" || o.Advice != "Review the order" || !o.AdviceReview || o.AdviceCategory != "review" || o.Status != "released" || o.AdviceDefinition == "" || o.AdviceModel != "local/probe" || len(o.AdviceSources) != 3 {
		t.Fatalf("incorrect advice: %+v calls=%d", o, calls.Load())
	}
	platformserver.CheckReplay(t, tn, journal, compose)
	if calls.Load() != 1 {
		t.Fatal("recovery called the provider")
	}
}
