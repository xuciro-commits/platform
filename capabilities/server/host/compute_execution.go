package host

import (
	"context"
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/functions/runtime"
)

type operationExecution struct{ t *Tenant }

func (h operationExecution) Authorised(ctx context.Context, x platform.Effect, b compute.OperationBinding, now time.Time) (platform.Member, *kernel.Error) {
	return recordRead(h.t, ctx, func(t *Tenant) (platform.Member, *kernel.Error) {
		member, ok := t.Member(b.Member)
		if !ok || member.Tenant != t.ID {
			return member, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation member is no longer authorised")
		}
		if !t.operationCurrent(x.ID, x.Generation) {
			return member, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation was cancelled")
		}
		return member, authorizationRefusal(t.operationAuthorization(member, x.App, b, now))
	})
}

func (h operationExecution) StillAuthorised(ctx context.Context, x platform.Effect, b compute.OperationBinding, now time.Time) (bool, *kernel.Error) {
	return recordRead(h.t, ctx, func(t *Tenant) (bool, *kernel.Error) {
		latest, ok := t.Member(b.Member)
		return ok && t.operationAuthorization(latest, x.App, b, now).Allow && t.operationCurrent(x.ID, x.Generation), nil
	})
}

func (h operationExecution) RulesRevoked(ctx context.Context, member platform.Member, rules platform.RuleTablePlan) (string, *kernel.Error) {
	return recordRead(h.t, ctx, func(t *Tenant) (string, *kernel.Error) {
		for _, set := range rules.Reads.Collections {
			query, _, ok := t.memberQueryVersion(member, platform.AssetRef{App: set.App, Kind: platform.AssetQuery, Name: set.Query}, set.Version)
			if !ok || query.Object != set.Object {
				return "Rule collection permission was revoked", nil
			}
			info, _ := t.Entity(set.Object)
			if set.Policy == "complete" && info.Scope.LevelFor(member.RolesIn(info.App), t.services.Authorization) != platform.ScopeTenant {
				return "Complete collection permission was revoked", nil
			}
		}
		return "", nil
	})
}

func (h operationExecution) BuildCode(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, error) {
	return h.t.buildCode(ctx, q)
}

func (h operationExecution) ReadInput(ctx context.Context, ref platform.OperationInputArtifact) ([]byte, error) {
	return h.t.staged.ReadInput(ctx, ref)
}

func (h operationExecution) StageOutput(op platform.Operation, call string, output []byte) (json.RawMessage, *platform.StagedResult, error) {
	return h.t.staged.Output(op, call, output)
}

func (h operationExecution) App(id string) platform.App { return h.t.App(id) }

func (h operationExecution) ReadArtifact(ctx context.Context, digest string) ([]byte, error) {
	return h.t.readArtifact(ctx, digest)
}

func (h operationExecution) Rules() platform.RuleEngine { return h.t.services.Rules }
