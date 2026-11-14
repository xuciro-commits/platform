package control

import (
	"encoding/json"
	"fmt"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func CandidateTestMember(tenant string) platform.Member {
	return platform.Member{ID: "app:candidate-test", Tenant: tenant, Roles: map[string]string{PlatformApp: Admin, platform.AIApp: platform.AIAdmin}}
}

func FixtureModelSubmissions(tenant, model string) (platform.Member, []*pb.Submission, error) {
	provider, name, ok := strings.Cut(model, "/")
	if !ok || provider == "" || name == "" {
		return platform.Member{}, nil, fmt.Errorf("fixture model must identify provider/model")
	}
	admin := CandidateTestMember(tenant)
	settings := []struct {
		app, schema, kind, id string
		payload               any
	}{
		{platform.AIApp, platform.AISchemaProviderAdd, platform.AIProviderType, provider, map[string]string{"kind": "local", "baseUrl": "http://candidate.invalid", "wire": "openai"}},
		{platform.AIApp, platform.AISchemaModelEnable, platform.AIModelType, model, map[string]string{"access": "users"}},
		{PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]string{"value": model}},
	}
	out := make([]*pb.Submission, len(settings))
	for i, setting := range settings {
		payload, _ := json.Marshal(setting.payload)
		out[i] = &pb.Submission{TenantId: tenant, PrincipalId: admin.ID, Authority: setting.app,
			IdempotencyKey: fmt.Sprintf("fixture-model-%d", i), Target: &pb.EntityRef{Type: setting.kind, Id: setting.id},
			Schema: &pb.SchemaRef{Name: setting.schema, Version: 1}, Payload: payload}
	}
	return admin, out, nil
}
