package branches

import (
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

type GraphPreview struct {
	Diagnostic string
	Candidate  string
	Added      []platform.AssetRef
	Removed    []platform.AssetRef
	Changed    []platform.AssetRef
}

func DecideChild[D any](draft D, tenant string, m platform.Member, item Action, key string, contained func(any) bool, step func(D, string, *pb.Submission, bool) StageChild) (out StageChild) {
	defer func() {
		if p := recover(); p != nil {
			if !contained(p) {
				panic(p)
			}
			out = StageChild{Refusal: platform.RefuseOutsideAcceptedBoundary()}
		}
	}()
	if item.Schema != "" {
		out = step(draft, item.Schema, item.Submission(tenant, m, key), false)
		if out.Refusal != nil {
			return out
		}
	}
	if item.Artifact != nil {
		attach := &pb.Submission{TenantId: tenant, PrincipalId: m.ID, Authority: build.ID, Target: &pb.EntityRef{Type: item.Plan.Type, Id: item.Plan.ID},
			Schema: &pb.SchemaRef{Name: build.SchemaCodeAttach, Version: 1}, Payload: item.Artifact}
		attach.IdempotencyKey = Key(key+":build", m.ID, build.SchemaCodeAttach, item.Artifact, nil)
		out = step(draft, build.SchemaCodeAttach, attach, true)
	}
	return out
}

func PreviewDrafts[C any](drafts []build.JointDraftRef, preview func([]build.JointDraftRef) (GraphPreview, C, error)) (GraphPreview, C, error) {
	reply, candidate, err := preview(drafts)
	if err == nil && reply.Diagnostic != "" {
		err = fmt.Errorf("%s", reply.Diagnostic)
	}
	return reply, candidate, err
}

func CheckGraph[C any](view func() (build.ConstructionDrafts, bool, error), preview func([]build.JointDraftRef) (GraphPreview, C, error), actions []Action) (BranchGraphCheck, []build.JointDraftRef, C) {
	graph := BranchGraphCheck{Messages: []string{}}
	var none C
	fail := func(format string, args ...any) (BranchGraphCheck, []build.JointDraftRef, C) {
		graph.Messages = append(graph.Messages, fmt.Sprintf(format, args...))
		return graph, nil, none
	}
	owner, ok, err := view()
	if err != nil {
		return fail("%s", err.Error())
	}
	if !ok {
		return fail("The staged definition graph has no builder")
	}
	drafts := DraftSet(owner, actions)
	if len(drafts) == 0 {
		graph.Passed = true
		return graph, nil, none
	}
	reply, candidate, err := PreviewDrafts(drafts, preview)
	if err != nil {
		return fail("The merged drafts do not form a valid release: %s", err)
	}
	graph.Passed, graph.Candidate = true, reply.Candidate
	graph.Added, graph.Removed, graph.Changed = reply.Added, reply.Removed, reply.Changed
	return graph, drafts, candidate
}

func MergeBefore[D any](id string, actions []Action, decide func(D, Action, string) StageChild) func(D) *kernel.Error {
	return func(draft D) *kernel.Error {
		for _, item := range actions {
			if item.Action == "none" {
				continue
			}
			child := decide(draft, item, "branch:"+id+":merge:"+item.Plan.Type+"/"+item.Plan.ID)
			if child.Refusal != nil {
				return platform.Refuse(child.Refusal.Code, "{resource} could not be applied: {message}", item.Plan.Type+"/"+item.Plan.ID, child.Refusal.Message)
			}
		}
		return nil
	}
}
