package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK35GanttViewRequiresDateFieldsOnSaveAndPackage(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k35")
	fields := []ontology.Field{
		{Name: "title", Title: "Title", Type: "text", Required: true},
		{Name: "start", Title: "Start", Type: "date"},
		{Name: "end", Title: "End", Type: "datetime"},
		{Name: "status", Title: "Status", Type: "choice", Choices: "todo,doing,done"},
	}
	live.accept(t, "build.object.create", build.ObjectType, "TASK", map[string]any{
		"name": "task", "title": "Task", "display": "title", "fields": fields,
	})
	cases := []struct {
		name string
		view string
		want string
	}{
		{"missing dates", `{"version":1,"layout":"gantt","until":"end"}`, "dates"},
		{"missing until", `{"version":1,"layout":"gantt","dates":"start"}`, "until"},
		{"unknown dates", `{"version":1,"layout":"gantt","dates":"missing","until":"end"}`, "missing"},
		{"non-date until", `{"version":1,"layout":"gantt","dates":"start","until":"title"}`, "title"},
	}
	for _, tc := range cases {
		sub := live.submission("build.object.edit", build.ObjectType, "TASK", map[string]any{"view": json.RawMessage(tc.view)})
		if _, refusal := live.Submit(live.member(t), sub, invariantClock); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || !strings.Contains(refusal.Message, tc.want) {
			t.Fatalf("%s on save: got refusal %+v, want INVALID_ARGUMENT naming %q", tc.name, refusal, tc.want)
		}
		assetBody := platform.Raw(map[string]any{
			"type": "invariantproject.task", "name": "task", "namespace": "invariantproject",
			"title": "Task", "display": "title", "fields": fields, "view": json.RawMessage(tc.view),
		})
		_, err := PublicationResolver().WithDependencies(platform.ReleaseAsset{
			Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.task"}, ContractVersion: 1, SourceVersion: "1", Body: assetBody,
		})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s on package: got err %v, want error naming %q", tc.name, err, tc.want)
		}
	}
	validView := json.RawMessage(`{"version":1,"layout":"gantt","dates":"start","until":"end","group":"status","lanes":"status"}`)
	live.accept(t, "build.object.edit", build.ObjectType, "TASK", map[string]any{"view": validView})
	live.accept(t, "build.object.publish", build.ObjectType, "TASK", nil)
	_, handler := observationHost(live.Tenant)
	request := httptest.NewRequest(http.MethodGet, "/v1/entities", nil)
	request.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
	request.Header.Set(TenantHeader, live.ID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var catalog []platform.RecordTypeInfo
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &catalog) != nil {
		t.Fatalf("entities: HTTP %d %s", response.Code, response.Body)
	}
	var delivered json.RawMessage
	for _, info := range catalog {
		if info.Type == "invariantproject.task" {
			delivered = info.View
		}
	}
	if !bytes.Equal(delivered, validView) {
		t.Fatalf("delivered gantt view = %s, want %s", delivered, validView)
	}
	assetBody := platform.Raw(map[string]any{
		"type": "invariantproject.task", "name": "task", "namespace": "invariantproject",
		"title": "Task", "display": "title", "fields": fields, "view": validView,
	})
	if _, err := PublicationResolver().WithDependencies(platform.ReleaseAsset{
		Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.task"}, ContractVersion: 1, SourceVersion: "1", Body: assetBody,
	}); err != nil {
		t.Fatalf("valid gantt view failed package resolution: %v", err)
	}
}
