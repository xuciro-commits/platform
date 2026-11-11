package host

import (
	"context"
	"encoding/json"
	"platformserver/services/resources/merge"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) BranchCompile(m platform.Member, id, typ, draft string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	if typ != build.CodeType {
		return branches.BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Only a code function compiles")
	}
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen {
		return branches.BranchDetail{}, branchRefuse("The branch is {state}; only an open branch takes a compile", branch.State)
	}
	request, expected, spec, refusal := t.branchCodeSource(m, branch, draft, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	result, err := t.buildCode(ctx, request)
	cancel()
	if err != nil || result.Module == "" {
		return branches.BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The build failed: {diagnostics}", result.Diagnostics)
	}
	if result.SourceHash != expected.SourceHash || result.BuildHash != expected.BuildHash {
		return branches.BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The compiler answered for different source or schemas")
	}
	artifact := build.CodeArtifact{Module: result.Module, SourceHash: result.SourceHash, BuildHash: result.BuildHash, Toolchain: result.Toolchain}
	branch, refusal = t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen {
		return branches.BranchDetail{}, branchRefuse("The branch is {state}; only an open branch takes a compile", branch.State)
	}
	_, latest, spec, refusal := t.branchCodeSource(m, branch, draft, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	if latest.SourceHash != expected.SourceHash || latest.BuildHash != expected.BuildHash {
		return branches.BranchDetail{}, branchRefuse("The code changed while it was compiling; compile it again")
	}
	var saved map[string]json.RawMessage
	_ = json.Unmarshal(spec, &saved)
	saved[branches.CodeBuildKey], _ = json.Marshal(artifact)
	raw, _ := json.Marshal(saved)
	type basis struct {
		draft   branches.MainDraft
		project string
	}
	base, refusal := recordRead(t, context.Background(), func(t *Tenant) (basis, *kernel.Error) {
		current, refusal := t.mainDraftOf(m, typ, draft, now)
		return basis{draft: current, project: t.resourceProject(typ, draft)}, refusal
	})
	current := base.draft
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	project := base.project
	if latestChange, found := branch.Latest(typ, draft); found && latestChange.Project != "" {
		project = latestChange.Project
	}
	return t.appendBranchSave(m, branch, current, typ, draft, "update", project, raw, now)
}

func (t *Tenant) branchCodeSource(m platform.Member, branch build.Branch, draft string, now time.Time) (platform.CodeBuildRequest, build.CodeArtifact, json.RawMessage, *kernel.Error) {
	none := platform.CodeBuildRequest{}
	current, refusal := recordRead(t, context.Background(), func(t *Tenant) (branches.MainDraft, *kernel.Error) {
		return t.mainDraftOf(m, build.CodeType, draft, now)
	})
	if refusal != nil {
		return none, build.CodeArtifact{}, nil, refusal
	}
	spec, _ := branches.EffectiveSpec(branch, build.CodeType, draft)
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
	delete(fields, branches.CodeBuildKey)
	authored, _ := json.Marshal(fields)
	request, expected, bad := build.CodeBuildOf(authored)
	if bad != nil {
		return none, build.CodeArtifact{}, nil, bad
	}
	return request, expected, authored, nil
}
