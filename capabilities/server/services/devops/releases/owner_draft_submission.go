package releases

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func DraftLogicalName(ref platform.AssetRef) string {
	if ref.Kind == platform.AssetObject || ref.Kind == platform.AssetFlow {
		return strings.TrimPrefix(ref.Name, build.ID+".")
	}
	return ref.Name
}

func DraftSubmission(tenant, candidate string, context platform.AssetReleaseContext, request *OwnerDraftRequest, ordinal int, draft platform.AssetDraft, info platform.EntityInfo) (*pb.Submission, error) {
	raw, err := AssetDraftPayload(info, draft.Image)
	if err != nil {
		return nil, err
	}
	if context.Project != "" && build.BranchDraftType(draft.Type) && draft.Type != build.AppType {
		var payload map[string]json.RawMessage
		if json.Unmarshal(raw, &payload) != nil {
			return nil, fmt.Errorf("asset draft %s/%s is unreadable", draft.Type, draft.Name)
		}
		payload["project"] = platform.Raw(context.Project)
		raw, _ = json.Marshal(payload)
	}
	key := "release-bootstrap:" + candidate + ":" + context.Member.ID + ":" + draft.Type + "/" + draft.Name
	if request != nil {
		key = request.Key + "#" + strconv.Itoa(ordinal)
	}
	return &pb.Submission{TenantId: tenant, PrincipalId: context.Member.ID, Authority: info.App, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: draft.Type, Id: draft.Name}, Schema: &pb.SchemaRef{Name: draft.Schema, Version: 1}, Payload: raw}, nil
}

func DraftRefusal(app platform.App, draft platform.AssetDraft, rejected *kernel.Error) error {
	if rejected.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		action, _ := app.Manifest().Actions.Action(draft.Schema)
		return fmt.Errorf("create target draft %s/%s needs the %s role and matching token scope: %w", draft.Type, draft.Name, strings.Join(action.Roles, " role or "), rejected)
	}
	if rejected.Message != "" {
		return fmt.Errorf("create target draft %s/%s: %s: %w", draft.Type, draft.Name, rejected.Message, rejected)
	}
	return fmt.Errorf("create target draft %s/%s: %w", draft.Type, draft.Name, rejected)
}
