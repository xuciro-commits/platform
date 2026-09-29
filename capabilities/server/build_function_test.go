package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestBuilderFunctionVersionsCallsAndRecovery(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		tn, err := NewTenant("builder-functions", NewConsole("builder-functions",
			Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder, PlatformApp: Admin, ai.ID: ai.Admin}}},
			Seat{Subjects: []string{"eli"}, Member: platform.Member{ID: "eli", Roles: map[string]string{build.ID: build.User}}},
			Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{build.ID: build.User}}}), ai.New("builder-functions"), build.New("builder-functions"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var entries []Entry
	fail := false
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append unavailable")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	keys := 0
	submit := func(who, app, schema, typ, id string, payload any) *kernel.Error {
		keys++
		member, _ := tn.Member(who)
		_, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: who, Authority: app, IdempotencyKey: fmt.Sprint(keys), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at)
		return err
	}
	must := func(who, app, schema, typ, id string, payload any) {
		t.Helper()
		if err := submit(who, app, schema, typ, id, payload); err != nil {
			t.Fatalf("%s: %v fault=%+v", schema, err, tn.fault.Load())
		}
	}
	must("dana", build.ID, build.ObjectType+".create", build.ObjectType, "O1", map[string]any{"name": "intake", "title": "Intake", "access": []build.Access{{Role: build.User, Read: "all", Create: true, Edit: true}, {Role: "analyst", Read: "all"}}, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	must("dana", build.ID, build.SchemaPublish, build.ObjectType, "O1", struct{}{})
	definition := platform.RecordAdviceFunction("build.intake", []string{"note"}, []string{build.Builder, build.User, "analyst"})
	definition.Name = "advice"
	must("dana", build.ID, build.FunctionType+".create", build.FunctionType, "F1", definition)
	owner := tn.app(build.ID).(*build.Build)
	if _, _, ok := owner.FunctionDefinition("advice", 0); ok {
		t.Fatal("draft function installed")
	}
	fail = true
	if err := submit("dana", build.ID, build.SchemaFunction, build.FunctionType, "F1", struct{}{}); err == nil {
		t.Fatal("failed publication append accepted")
	}
	if _, _, ok := owner.FunctionDefinition("advice", 0); ok {
		t.Fatal("failed append installed the function")
	}
	fail = false
	must("dana", build.ID, build.SchemaFunction, build.FunctionType, "F1", struct{}{})
	installed, version, ok := owner.FunctionDefinition("advice", 0)
	if !ok || version != 1 || installed.Instructions != definition.Instructions {
		t.Fatal("first publication missing")
	}
	dana, _ := tn.Member("dana")
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetFunction, Name: "advice"}
	if !slices.ContainsFunc(tn.Definitions(dana), func(d platform.Definition) bool {
		return d.Ref == ref && d.Source == "tenant" && d.Version == "1.function-1"
	}) {
		t.Fatal("published function missing from shared registry")
	}
	before, after, _, next, _, err := owner.DraftReleaseAssets(platform.AssetFunction, "F1")
	if err != nil || next != ref {
		t.Fatalf("function draft release: %v", err)
	}
	c1, err := platform.Candidate([]platform.AssetRef{ref}, before)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := platform.Candidate([]platform.AssetRef{ref}, after)
	if err != nil || c1.ID == c2.ID {
		t.Fatalf("version not in candidate identity: %v", err)
	}
	// Establish a saved release to test exact runtime binding. This is not a
	// proof of the future function candidate evaluation/activation journey.
	tn.releaseCandidates, tn.activeRelease = map[string]json.RawMessage{c1.ID: c1.Bytes}, c1.ID
	if closure, release, err := tn.functionClosure(build.ID, definition, 1, nil); err != nil || closure.ID != c1.ID || release != c1.ID {
		t.Fatalf("published function did not bind its saved release: %s %s %v", closure.ID, release, err)
	}
	tn.activeRelease = ""
	tn.releaseCandidates = nil
	must("dana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": "http://localhost:1"})
	must("dana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]string{"access": "users"})
	must("dana", PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]string{"value": "local/probe"})
	must("eli", build.ID, "build.intake.create", "build.intake", "S1", map[string]string{"note": "Original note"})
	call := map[string]any{"name": "advice", "source": "S1"}
	if err := submit("eli", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "impersonation", map[string]any{"name": "advice", "source": "S1", "onBehalf": "dana"}); err == nil || len(tn.outbound) != 0 {
		t.Fatal("person borrowed another member's grants")
	}
	fail = true
	if err := submit("eli", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "R1", call); err == nil || len(tn.outbound) != 0 {
		t.Fatal("failed append leaked model input")
	}
	fail = false
	must("eli", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "R1", call)
	var ask modelAsk
	json.Unmarshal([]byte(tn.outbound[0].Body), &ask)
	if ask.Function == nil || ask.Function.Call.Source != "build.intake/S1" || ask.Record != build.FunctionCallType+"/R1" || ask.Function.Call.Version != 1 || ask.Function.Member != "eli" {
		t.Fatalf("call confused source and reply: %+v", ask)
	}
	if err := submit("eli", build.ID, build.SchemaFunctionAnswer, build.FunctionCallType, "R1", platform.Answer{Call: "R1", Outcome: "accepted", Text: `{"summary":"Forged","category":"routine","review":false}`}); err == nil {
		t.Fatal("a person forged the automatic answer")
	}
	must("eli", build.ID, "build.intake.edit", "build.intake", "S1", map[string]string{"note": "New source note"})
	must("dana", build.ID, build.FunctionType+".edit", build.FunctionType, "F1", map[string]string{"instructions": "New instructions"})
	if f, v, _ := owner.FunctionDefinition("advice", 0); v != 1 || f.Instructions != definition.Instructions {
		t.Fatal("draft edit changed installed calls")
	}
	must("dana", build.ID, build.SchemaFunction, build.FunctionType, "F1", struct{}{})
	if f, v, _ := owner.FunctionDefinition("advice", 0); v != 2 || f.Instructions != "New instructions" {
		t.Fatal("second publication not installed")
	}
	if f, v, ok := owner.FunctionDefinition("advice", 1); !ok || v != 1 || f.Instructions != definition.Instructions {
		t.Fatal("old publication lost")
	}
	tn.releaseCandidates, tn.activeRelease = map[string]json.RawMessage{c1.ID: c1.Bytes}, c1.ID
	if err := submit("eli", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "wrong-release", call); err == nil {
		t.Fatal("latest function silently escaped its activated version")
	}
	// The manually established release has no journal entry; remove it before
	// comparing only journalled state.
	tn.activeRelease = ""
	tn.releaseCandidates = nil
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	if f, v, ok := restored.app(build.ID).(*build.Build).FunctionDefinition("advice", 1); !ok || v != 1 || f.Instructions != definition.Instructions {
		t.Fatal("restore lost retained version")
	}
	var wire string
	restored.AIClient = func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		wire = string(body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"summary\":\"Saved advice\",\"category\":\"review\",\"review\":true}"}}],"usage":{"prompt_tokens":4,"completion_tokens":8,"cost":0.0125}}`)), Header: make(http.Header)}, nil
	}
	effect := restored.outbound[0].Effect
	out, usage := restored.sendModel(effect, at.Add(time.Second))
	if out.Result != "delivered" || usage == nil || strings.Contains(wire, "New instructions") || strings.Contains(wire, "New source note") || !strings.Contains(wire, "Original note") {
		t.Fatalf("accepted binding drifted: %+v %s", out, wire)
	}
	tn.AIClient = restored.AIClient
	effect = tn.outbound[0].Effect
	out, usage = tn.sendModel(effect, at.Add(time.Second))
	fail = true
	tn.settleWithUsage(effect.ID, out, usage, at.Add(time.Second))
	eli, _ := tn.Member("eli")
	view, refusal := tn.RecordOf(eli, build.FunctionCallType, "R1", at)
	if refusal != nil || view.Record.(build.FunctionRun).Metered || len(tn.ai.Usage()) != 0 {
		t.Fatal("failed effect append exposed uncommitted function metrics")
	}
	fail = false
	tn.settleWithUsage(effect.ID, out, usage, at.Add(time.Second))
	if tn.quarantined() {
		t.Fatal(tn.fault.Load())
	}
	view, refusal = tn.RecordOf(eli, build.FunctionCallType, "R1", at)
	if refusal != nil {
		t.Fatal(refusal)
	}
	run := view.Record.(build.FunctionRun)
	if run.State != "ready" || run.Output == "" || run.Version != 1 || run.Model != "local/probe" ||
		!run.Metered || !run.TokensReported || run.InputTokens != 4 || run.OutputTokens != 8 ||
		!run.CostReported || run.CostUSD != 0.0125 || run.LatencyMillis < 0 {
		t.Fatalf("answer not retained: %+v", run)
	}
	bo, _ := tn.Member("bo")
	if _, err := tn.RecordOf(bo, build.FunctionCallType, "R1", at); err == nil {
		t.Fatal("another person saw the call")
	}
	source, refusal := tn.RecordOf(eli, "build.intake", "S1", at)
	body, _ := json.Marshal(source.Record)
	if refusal != nil || !strings.Contains(string(body), "New source note") || strings.Contains(string(body), "Saved advice") {
		t.Fatal("function changed source record")
	}
	// Owning a call does not grant access to its sources after a field grant
	// changes. The shared derived-content projection must withhold the answer.
	must("dana", build.ID, build.ObjectType+".edit", build.ObjectType, "O1", map[string]any{"fields": []build.Field{{Name: "note", Title: "Note", Type: "text", Read: []string{build.Builder}}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	must("dana", build.ID, build.SchemaPublish, build.ObjectType, "O1", struct{}{})
	view, refusal = tn.RecordOf(eli, build.FunctionCallType, "R1", at)
	if refusal != nil || view.Record.(build.FunctionRun).Output != "" || !view.Record.(build.FunctionRun).Withheld {
		t.Fatal("source field revocation left the derived answer readable")
	}
	must("dana", build.ID, build.ObjectType+".edit", build.ObjectType, "O1", map[string]any{"fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	must("dana", build.ID, build.SchemaPublish, build.ObjectType, "O1", struct{}{})
	// Removing a function input must not silently break its installed binding.
	must("dana", build.ID, build.ObjectType+".edit", build.ObjectType, "O1", map[string]any{"fields": []build.Field{{Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	if err := submit("dana", build.ID, build.SchemaPublish, build.ObjectType, "O1", struct{}{}); err == nil {
		t.Fatal("source republish removed function input")
	}
	// A changed function role cannot borrow a field it still cannot read.
	must("dana", build.ID, build.FunctionType+".edit", build.FunctionType, "F1", map[string]any{"fields": []string{"secret"}})
	must("dana", build.ID, build.SchemaFunction, build.FunctionType, "F1", struct{}{})
	if err := submit("eli", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "private", call); err == nil {
		t.Fatal("private field escaped through function call")
	}
	// Explicit old version remains callable, with current source permissions.
	old := map[string]any{"name": "advice", "source": "S1", "version": 1}
	must("dana", PlatformApp, SchemaGrant, MemberType, "bo", map[string]string{"app": build.ID, "role": "analyst"})
	must("bo", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "custom-role", old)
	must("eli", build.ID, build.SchemaFunctionCall, build.FunctionCallType, "R2", old)
	must("dana", PlatformApp, SchemaRevoke, MemberType, "eli", map[string]string{"app": build.ID})
	tn.AIClient = func(*http.Request) (*http.Response, error) {
		t.Fatal("revoked input reached provider")
		return nil, nil
	}
	effect = tn.outbound[len(tn.outbound)-1].Effect
	out, usage = tn.sendModel(effect, at.Add(time.Minute))
	if out.Result != "rejected" || usage != nil {
		t.Fatal("revocation ignored")
	}
	tn.settleWithUsage(effect.ID, out, usage, at.Add(time.Minute))
	CheckReplay(t, tn, entries, compose)
}
