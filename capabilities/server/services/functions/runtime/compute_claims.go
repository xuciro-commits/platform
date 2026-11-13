package compute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func StartClaim(works *kernel.Works, id string) (generation uint32, prior, work json.RawMessage, ok bool) {
	if old, err := works.Get(id); err == nil {
		prior, _ = protojson.Marshal(old)
		if old.GetState() == pb.WorkState_WORK_STATE_RUNNING && works.Cancel(id) != nil {
			return 0, nil, nil, false
		}
	}
	generation, _, refusal := works.Start(id, "host-compute")
	if refusal != nil {
		return 0, nil, nil, false
	}
	started, _ := works.Get(id)
	work, _ = protojson.Marshal(started)
	return generation, prior, work, true
}

func ReplayClaim(works *kernel.Works, before, after platform.Effect, key string, savedPrior, savedWork json.RawMessage) error {
	var prior json.RawMessage
	if old, err := works.Get(before.ID); err == nil {
		prior, _ = protojson.Marshal(old)
	}
	priorHash, _ := platform.CanonicalDigest(prior)
	savedHash, _ := platform.CanonicalDigest(savedPrior)
	if priorHash != savedHash {
		return fmt.Errorf("operation K9 predecessor differs")
	}
	if old, err := works.Get(before.ID); err == nil && old.GetState() == pb.WorkState_WORK_STATE_RUNNING {
		if works.Cancel(before.ID) != nil {
			return fmt.Errorf("operation owner cannot be invalidated")
		}
	}
	generation, _, refusal := works.Start(before.ID, "host-compute")
	if refusal != nil || generation != after.Generation || key != fmt.Sprintf("operation-claim:%s:%d", before.ID, generation) {
		return fmt.Errorf("operation generation differs")
	}
	started := before
	started.Generation = generation
	actual, _ := platform.CanonicalDigest(started)
	frozen, _ := platform.CanonicalDigest(after)
	var work pb.Work
	if actual != frozen || protojson.Unmarshal(savedWork, &work) != nil {
		return fmt.Errorf("invalid operation claim transition")
	}
	check, _ := works.Get(before.ID)
	if !proto.Equal(check, &work) {
		return fmt.Errorf("invalid operation claim work")
	}
	return nil
}

func FinishWork(works *kernel.Works, id string, generation uint32, failed bool) (prior, work json.RawMessage, err error) {
	old, _ := works.Get(id)
	prior, _ = protojson.Marshal(old)
	if refusal := works.Finish(id, generation, failed); refusal != nil {
		return nil, nil, refusal
	}
	finished, _ := works.Get(id)
	work, _ = protojson.Marshal(finished)
	return prior, work, nil
}

func CancelWork(works *kernel.Works, id string) (prior, work json.RawMessage, err error) {
	old, refusal := works.Get(id)
	if refusal != nil {
		return nil, nil, nil
	}
	prior, _ = protojson.Marshal(old)
	if old.GetState() == pb.WorkState_WORK_STATE_RUNNING {
		if refusal := works.Cancel(id); refusal != nil {
			return prior, nil, refusal
		}
	}
	current, _ := works.Get(id)
	work, _ = protojson.Marshal(current)
	return prior, work, nil
}

func RestoreWork(works *kernel.Works, raw json.RawMessage) error {
	work := &pb.Work{}
	if err := protojson.Unmarshal(raw, work); err != nil {
		return err
	}
	all := works.All()
	for i, prior := range all {
		if prior.GetWorkId() == work.GetWorkId() {
			all[i] = work
		}
	}
	works.Restore(all)
	return nil
}

type Artifacts struct {
	Tenant string
	Files  platform.FileStore
}

func (a Artifacts) Key(digest string) string { return a.Tenant + "/artifacts/wasm/" + digest }

func (a Artifacts) Exists(ctx context.Context, digest string) bool {
	return a.Files.Exists(ctx, a.Key(digest))
}

func (a Artifacts) Put(ctx context.Context, digest string, module []byte) error {
	return a.Files.Put(ctx, a.Key(digest), module, "application/wasm")
}

func (a Artifacts) Stored(ctx context.Context, digest string) bool {
	r, n, err := a.Files.Get(ctx, a.Key(digest))
	if err != nil {
		return false
	}
	r.Close()
	return n > 0 && n <= 16<<20
}

func (a Artifacts) Read(ctx context.Context, digest string) ([]byte, error) {
	r, n, err := a.Files.Get(ctx, a.Key(digest))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if n <= 0 || n > 16<<20 {
		return nil, fmt.Errorf("module exceeds its byte bound")
	}
	raw, err := io.ReadAll(io.LimitReader(r, (16<<20)+1))
	hash := sha256.Sum256(raw)
	if err != nil || len(raw) > 16<<20 || hex.EncodeToString(hash[:]) != digest {
		return nil, fmt.Errorf("module digest differs from its retained bytes")
	}
	return raw, nil
}

func (a Artifacts) Build(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, error) {
	result, module, err := EnvironmentCompiler().Compile(ctx, q)
	if err == nil {
		err = ValidateWasm(ctx, module, q.ABI)
		if err == nil {
			hash := sha256.Sum256(module)
			result.Module = hex.EncodeToString(hash[:])
			err = a.Put(ctx, result.Module, module)
		}
	}
	if err != nil {
		result.Diagnostics = err.Error()
	}
	return result, err
}

func PlanCompilation(tenant string, c platform.Caller, r *pb.ChangeRecord, key, target string, q platform.CodeBuildRequest, queued int, pending []platform.Effect) (platform.OperationCall, platform.Effect, *kernel.Error) {
	if c.App != "build" || c.Automation || r.GetSubmission().GetAuthority() != c.App || key == "" || len(q.Source) == 0 || len(q.Source) > 256<<10 || q.Language != "go" && q.Language != "tinygo" || q.Input.Check() != nil || q.Output.Check() != nil {
		return platform.OperationCall{}, platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Compilation needs a bounded Go/TinyGo code asset")
	}
	for _, x := range pending {
		if x.Endpoint == operationEndpoint {
			queued++
		}
	}
	if queued >= 256 {
		return platform.OperationCall{}, platform.Effect{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Operation queue is at capacity")
	}
	hash, _ := platform.CanonicalDigest(q)
	id := fmt.Sprintf("%s:build:operation:%s:%s", tenant, key, operationEndpoint)
	call := platform.OperationCall{ID: id, InputHash: hash, Definition: hash}
	body, _ := json.Marshal(OperationBinding{Build: &q, Call: call, Member: c.ID})
	at := r.GetRecordedTime().AsTime()
	return call, platform.Effect{ID: id, Endpoint: operationEndpoint, Event: "build/operation", App: "build", Key: key, Target: target, At: at, Due: at, State: "pending", Body: string(body)}, nil
}

func ReadableBy(tenant string, c platform.Caller, id string, lookup func(string) (platform.Effect, bool), member func(string) (platform.Member, bool), authorize func(platform.Member, string, OperationBinding) *kernel.Error) (platform.OperationResult, *kernel.Error) {
	x, found := lookup(id)
	if !found || x.Endpoint != operationEndpoint {
		return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation call was not found")
	}
	var b OperationBinding
	if json.Unmarshal([]byte(x.Body), &b) != nil {
		return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation input is unavailable")
	}
	if c.Tenant != tenant || !c.Automation && c.ID != b.Member || c.Automation && c.App != x.App && c.App != "flow" {
		return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation is not readable")
	}
	m, ok := member(b.Member)
	if !ok {
		return platform.OperationResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation permission was revoked")
	}
	if refusal := authorize(m, x.App, b); refusal != nil {
		return platform.OperationResult{}, refusal
	}
	return ResultOf(x), nil
}

func CheckCancel(tenant string, c platform.Caller, r *pb.ChangeRecord, id string, lookup func(string) (platform.Effect, bool)) (bool, *kernel.Error) {
	if r == nil || c.Tenant != tenant || r.GetSubmission().GetAuthority() != c.App {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Cancellation needs its accepted owner decision")
	}
	x, ok := lookup(id)
	if !ok || x.Endpoint != operationEndpoint {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Operation call was not found")
	}
	if platform.EffectSettled(x.State) {
		return false, nil
	}
	var b OperationBinding
	_ = json.Unmarshal([]byte(x.Body), &b)
	if c.Automation {
		target := r.GetSubmission().GetTarget()
		if c.App != "flow" || x.Target != target.GetType()+"/"+target.GetId() {
			return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Flow can cancel only its own operation")
		}
	} else if c.ID != b.Member {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Operation belongs to another member")
	}
	return true, nil
}

func RulesRevoked(member platform.Member, rules platform.RuleTablePlan, query func(platform.AssetRef, int) (platform.NamedQuery, bool), entity func(string) (platform.EntityInfo, bool), authz platform.AuthorizationEngine) string {
	for _, set := range rules.Reads.Collections {
		q, ok := query(platform.AssetRef{App: set.App, Kind: platform.AssetQuery, Name: set.Query}, set.Version)
		if !ok || q.Object != set.Object {
			return "Rule collection permission was revoked"
		}
		info, _ := entity(set.Object)
		if set.Policy == "complete" && info.Scope.LevelFor(member.RolesIn(info.App), authz) != platform.ScopeTenant {
			return "Complete collection permission was revoked"
		}
	}
	return ""
}
