package platformserver

import (
	"encoding/json"
	"platformserver/apps/ai"
	"platformserver/platform"
	"testing"
	"time"
)

func declaredRecord(t *testing.T, tn *Tenant, member, typ, id string, at time.Time) map[string]any {
	t.Helper()
	answer, err := tn.RecordOf(memberOf(t, tn, member), typ, id, at)
	if err != nil {
		t.Fatalf("%s/%s: %v", typ, id, err)
	}
	var record map[string]any
	if err := json.Unmarshal(platform.Raw(answer.Record), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func configureDeclaredModel(t *testing.T, tn *Tenant, at time.Time) {
	t.Helper()
	decide(t, tn, "builder", PlatformApp, SchemaGrant, MemberType, "builder", map[string]any{"app": ai.ID, "role": ai.Admin}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "fixture", map[string]any{"kind": "local", "baseUrl": "http://localhost:1", "wire": "openai"}, at)
	decide(t, tn, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "fixture/probe", map[string]any{"access": "users"}, at)
	for _, setting := range []string{"ai/app-model", "agent/model"} {
		decide(t, tn, "builder", PlatformApp, SchemaSettingSet, SettingType, setting, map[string]any{"value": "fixture/probe"}, at)
	}
}
