package host

import (
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	transport "platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/merge"
	"time"
)

const branchBodyMessage = "The request must be one valid bounded JSON object"

func branchByID[R any](now func() time.Time, f func(*Tenant, platform.Member, string, time.Time) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) {
	return transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (R, *kernel.Error) {
		return f(t, m, r.PathValue("id"), now())
	})
}

func branchByIDBody[B, R any](now func() time.Time, f func(*Tenant, platform.Member, string, B, time.Time) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) {
	return transport.Bounded(build.BranchMaxSpec+4096, branchBodyMessage, func(r *http.Request, m platform.Member, t *Tenant, body B) (R, *kernel.Error) {
		return f(t, m, r.PathValue("id"), body, now())
	})
}

func (h *Host) routesBranches(rt *routes) {
	branch := func(route transport.Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant)) {
		rt.authorized(route, build.ID, []string{build.Builder}, f)
	}
	branch(transport.Route{Pattern: "GET /v1/branches", Summary: "Builder-only list of named branches with the resources each changes", Answer: []branches.BranchDetail{}}, transport.Answering(func(_ *http.Request, m platform.Member, t *Tenant) ([]branches.BranchDetail, *kernel.Error) {
		return t.ListBranches(m, h.Now())
	}))
	branch(transport.Route{Pattern: "POST /v1/branches", Summary: "Open a named branch: a metadata overlay whose saves append to a change set; instance data is never part of it", Body: branches.BranchCreateRequest{}, Answer: branches.BranchDetail{}}, transport.Bounded(build.BranchMaxSpec+4096, branchBodyMessage, func(_ *http.Request, m platform.Member, t *Tenant, request branches.BranchCreateRequest) (branches.BranchDetail, *kernel.Error) {
		return t.CreateBranch(m, request, h.Now())
	}))
	branch(transport.Route{Pattern: "GET /v1/branches/{id}", Summary: "A branch with its resources, proposal and merge record", Answer: branches.BranchDetail{}}, branchByID(h.Now, (*Tenant).BranchDetail))
	branch(transport.Route{Pattern: "GET /v1/branches/{id}/changes", Summary: "The branch change set in save order; specs=true adds each saved spec and base", Query: []transport.Param{{Name: "specs", Description: "true: include base and spec bodies"}}, Answer: []branches.BranchChangeEntry{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) ([]branches.BranchChangeEntry, *kernel.Error) {
		return t.BranchChanges(m, r.PathValue("id"), r.URL.Query().Get("specs") == "true", h.Now())
	}))
	branch(transport.Route{Pattern: "GET /v1/branches/{id}/diff", Summary: "Per resource, the JSON path changes of the branch and of main since the branch's base, their conflicts, and the three-way merged spec", Answer: branches.BranchDiff{}}, branchByID(h.Now, (*Tenant).BranchDiff))
	branch(transport.Route{Pattern: "GET /v1/branches/{id}/resources/{type}/{draft}", Summary: "One resource's base, current main and branch spec", Answer: branches.BranchResourceView{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (branches.BranchResourceView, *kernel.Error) {
		return t.BranchResource(m, r.PathValue("id"), r.PathValue("type"), r.PathValue("draft"), h.Now())
	}))
	branch(transport.Route{Pattern: "PUT /v1/branches/{id}/resources/{type}/{draft}", Summary: "Save a resource's full authored spec into the branch (appends to the change set); op delete drops it", Body: branches.BranchSaveRequest{}, Answer: branches.BranchDetail{}}, transport.Bounded(build.BranchMaxSpec+4096, branchBodyMessage, func(r *http.Request, m platform.Member, t *Tenant, request branches.BranchSaveRequest) (branches.BranchDetail, *kernel.Error) {
		return t.SaveBranchResource(m, r.PathValue("id"), r.PathValue("type"), r.PathValue("draft"), request, h.Now())
	}))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/resources/{type}/{draft}/compile", Summary: "Compile a code function on the branch with the isolated controlled compiler; the content-addressed artifact reference is saved into the branch's code spec, never into main, and merges with the code resource without recompiling", Answer: branches.BranchDetail{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (branches.BranchDetail, *kernel.Error) {
		return t.BranchCompile(m, r.PathValue("id"), r.PathValue("type"), r.PathValue("draft"), h.Now())
	}))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/checks", Summary: "Dry-run staged check, recording nothing: three-way conflicts, then the merged resources applied in a staged world, the whole definition graph validated there, and optionally a candidate simulation", Body: BranchCheckRequest{}, Answer: BranchChecks{}}, branchByIDBody(h.Now, (*Tenant).BranchChecks))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/recheck", Summary: "Run the staged check again against current main and record it on the branch (open or proposed); the answer carries the recorded check and whether main has moved since", Body: BranchCheckRequest{}, Answer: branches.BranchDetail{}}, branchByIDBody(h.Now, (*Tenant).RecheckBranch))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/rebase", Summary: "Move the branch's base to current main, three-way merging every changed resource, then record a staged check of the result; unresolved conflicts answer 409 with the conflicts", Body: branches.BranchResolveRequest{}, Answer: branches.BranchDetail{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request branches.BranchResolveRequest
		if transport.DecodeBodyLimit(r, &request, build.BranchMaxSpec+4096) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, branchBodyMessage))
			return
		}
		out, conflicts, refusal := t.RebaseBranch(m, r.PathValue("id"), request, h.Now())
		if refusal == nil && conflicts != nil {
			WriteJSON(w, http.StatusConflict, branches.BranchConflictError{Conflicts: conflicts})
			return
		}
		transport.Answer(w, out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/proposal", Summary: "Propose the branch: the staged check of the merged definition graph must pass and is recorded on the proposal; named reviewers must be builders", Body: BranchProposeRequest{}, Answer: branches.BranchDetail{}}, branchByIDBody(h.Now, (*Tenant).ProposeBranch))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/proposal/review", Summary: "A named reviewer approves or asks for changes", Body: branches.BranchReviewRequest{}, Answer: branches.BranchDetail{}}, branchByIDBody(h.Now, (*Tenant).ReviewBranch))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/proposal/withdraw", Summary: "Return a proposed branch to editing", Answer: branches.BranchDetail{}}, branchByID(h.Now, (*Tenant).WithdrawBranch))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/merge", Summary: "Merge an approved proposal whose last staged check passed against the current main base: one accepted result applies every resource and the merge record, then a sealed release candidate over the merged drafts is saved (not activated); a stale or failed check is refused and nothing is accepted", Body: BranchMergeRequest{}, Answer: BranchMergeResult{}}, branchByIDBody(h.Now, (*Tenant).MergeBranch))
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/close", Summary: "Abandon the branch", Answer: branches.BranchDetail{}}, branchByID(h.Now, (*Tenant).CloseBranch))
}
