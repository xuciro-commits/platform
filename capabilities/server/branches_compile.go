package platformserver

import (
	"context"
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

func (t *Tenant) BranchCompile(m platform.Member, id, typ, draft string, now time.Time) (BranchDetail, *kernel.Error) {
	if typ != build.CodeType {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Only a code function compiles")
	}
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen {
		return BranchDetail{}, branchRefuse("The branch is {state}; only an open branch takes a compile", branch.State)
	}
	request, expected, spec, refusal := t.branchCodeSource(m, branch, draft, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	result, err := t.buildCode(ctx, request)
	cancel()
	if err != nil || result.Module == "" {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The build failed: {diagnostics}", result.Diagnostics)
	}
	if result.SourceHash != expected.SourceHash || result.BuildHash != expected.BuildHash {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The compiler answered for different source or schemas")
	}
	artifact := build.CodeArtifact{Module: result.Module, SourceHash: result.SourceHash, BuildHash: result.BuildHash, Toolchain: result.Toolchain}
	branch, refusal = t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen {
		return BranchDetail{}, branchRefuse("The branch is {state}; only an open branch takes a compile", branch.State)
	}
	_, latest, spec, refusal := t.branchCodeSource(m, branch, draft, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if latest.SourceHash != expected.SourceHash || latest.BuildHash != expected.BuildHash {
		return BranchDetail{}, branchRefuse("The code changed while it was compiling; compile it again")
	}
	var saved map[string]json.RawMessage
	_ = json.Unmarshal(spec, &saved)
	saved[codeBuildKey], _ = json.Marshal(artifact)
	raw, _ := json.Marshal(saved)
	t.mu.Lock()
	current, refusal := t.mainDraft(m, typ, draft, now)
	t.mu.Unlock()
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.appendBranchSave(m, branch, current, typ, draft, "update", raw, now)
}

func (t *Tenant) branchCodeSource(m platform.Member, branch build.Branch, draft string, now time.Time) (platform.CodeBuildRequest, build.CodeArtifact, json.RawMessage, *kernel.Error) {
	none := platform.CodeBuildRequest{}
	t.mu.Lock()
	current, refusal := t.mainDraft(m, build.CodeType, draft, now)
	t.mu.Unlock()
	if refusal != nil {
		return none, build.CodeArtifact{}, nil, refusal
	}
	spec, _ := effectiveSpec(branch, build.CodeType, draft)
	if _, touched := branch.Latest(build.CodeType, draft); !touched {
		spec = current.Spec
	}
	if spec == nil {
		return none, build.CodeArtifact{}, nil, branchRefuse("{resource} does not exist", build.CodeType+"/"+draft)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(spec, &fields) != nil {
		return none, build.CodeArtifact{}, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The code spec is unreadable")
	}
	delete(fields, codeBuildKey)
	authored, _ := json.Marshal(fields)
	request, expected, bad := build.CodeBuildOf(authored)
	if bad != nil {
		return none, build.CodeArtifact{}, nil, bad
	}
	return request, expected, authored, nil
}
