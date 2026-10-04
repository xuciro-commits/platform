package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestConversationFunctionBindsOriginalQuestionsHistoryAndPermissions(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("conversation", NewConsole("conversation", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, PlatformApp: Admin, ai.ID: ai.Admin}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}, Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: build.User}}}), ai.New("conversation"), build.New("conversation"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"Original answer","category":"routine","review":false}`}}}})
	}))
	defer provider.Close()
	tn := compose()
	at := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	keys := 0
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(who, app, typ, id, schema string, payload any) error {
		keys++
		m, _ := tn.Member(who)
		_, issue := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: who, Authority: app, IdempotencyKey: fmt.Sprint(keys), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at)
		if issue != nil {
			return fmt.Errorf("%s", issue.Message)
		}
		return nil
	}
	must := func(who, app, typ, id, schema string, payload any) {
		t.Helper()
		if err := submit(who, app, typ, id, schema, payload); err != nil {
			t.Fatal(schema, err)
		}
	}
	must("builder", build.ID, build.ObjectType, "object", build.ObjectType+".create", map[string]any{"name": "source", "title": "Source", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}})
	must("builder", build.ID, build.ObjectType, "object", build.ObjectType+".publish", map[string]any{})
	f := platform.RecordAdviceFunction("build.source", []string{"name"}, []string{build.Builder, build.User})
	f.Name = "chat"
	f.Conversation = true
	must("builder", build.ID, build.FunctionType, "function", build.FunctionType+".create", f)
	must("builder", build.ID, build.FunctionType, "function", build.SchemaFunction, map[string]any{})
	must("builder", ai.ID, ai.ProviderType, "local", ai.SchemaProviderAdd, map[string]string{"kind": "local", "baseUrl": provider.URL})
	must("builder", ai.ID, ai.ModelType, "local/probe", ai.SchemaModelEnable, map[string]string{"access": "users"})
	must("builder", PlatformApp, SettingType, "ai/app-model", SchemaSettingSet, map[string]string{"value": "local/probe"})
	must("reader", build.ID, "build.source", "A", "build.source.create", map[string]string{"name": "Original record"})
	must("reader", build.ID, "build.source", "B", "build.source.create", map[string]string{"name": "Other record"})
	call := func(source, question string, history []string) map[string]any {
		return map[string]any{"name": "chat", "version": 1, "source": source, "question": question, "history": history}
	}
	must("reader", build.ID, build.FunctionCallType, "first", build.SchemaFunctionCall, call("A", "First question", nil))
	var ask modelAsk
	json.Unmarshal([]byte(tn.outbound[0].Body), &ask)
	var input struct {
		Record   map[string]any
		Question string
		History  []any
	}
	if json.Unmarshal([]byte(ask.Prompt.User), &input) != nil || input.Record["name"] != "Original record" || input.Question != "First question" || len(input.History) != 0 {
		t.Fatal("question did not reach the accepted model input", input)
	}
	out, usage := tn.sendModel(tn.outbound[0].Effect, at.Add(time.Second))
	if out.Result != "delivered" {
		t.Fatal(out)
	}
	tn.settleWithUsage(tn.outbound[0].ID, out, usage, at.Add(time.Second))
	must("reader", build.ID, build.FunctionCallType, "second", build.SchemaFunctionCall, call("A", "Second question", []string{"first"}))
	json.Unmarshal([]byte(tn.outbound[len(tn.outbound)-1].Body), &ask)
	var next struct {
		History []struct {
			Question string
			Answer   map[string]any
		}
	}
	json.Unmarshal([]byte(ask.Prompt.User), &next)
	if len(next.History) != 1 || next.History[0].Question != "First question" || next.History[0].Answer["summary"] != "Original answer" {
		t.Fatal("history was not the original retained answer", next)
	}
	before := len(tn.outbound)
	for _, c := range []struct {
		who, source string
		history     []string
	}{{"other", "A", []string{"first"}}, {"reader", "B", []string{"first"}}, {"reader", "A", []string{"second"}}, {"reader", "A", []string{"first", "first"}}} {
		if submit(c.who, build.ID, build.FunctionCallType, fmt.Sprintf("bad%d", keys), build.SchemaFunctionCall, call(c.source, "Question", c.history)) == nil {
			t.Fatal("foreign, pending or duplicated history accepted")
		}
	}
	if len(tn.outbound) != before {
		t.Fatal("refused history created a model effect")
	}
	snapshot, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	kept, ok := restored.records.types[build.FunctionCallType].rows["second"].value.Interface().(build.FunctionRun)
	if !ok || kept.Question != "Second question" || len(kept.History) != 1 || kept.History[0] != "first" || !kept.Contract.Conversation {
		t.Fatal("conversation snapshot lost its original call binding")
	}
	must("builder", build.ID, "build.source", "A", "build.source.archive", map[string]any{})
	out, _ = tn.sendModel(tn.outbound[len(tn.outbound)-1].Effect, at.Add(2*time.Second))
	if out.Result == "delivered" {
		t.Fatal("an archived source released accepted conversation input")
	}
	CheckReplay(t, tn, entries, compose)
}
