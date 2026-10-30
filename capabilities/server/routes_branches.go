package platformserver

import (
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	transport "platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/studio/build"
)

func (h *Host) routesBranches(rt *routes) {
	builder := []string{build.Builder}
	branch := func(route transport.Route, f func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) (any, *branchFailure)) {
		rt.authorized(route, build.ID, builder, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
			answer, failure := f(w, r, m, t)
			if failure != nil {
				if failure.Conflicts != nil {
					WriteJSON(w, http.StatusConflict, BranchConflictError{Conflicts: failure.Conflicts})
					return
				}
				Reply(w, nil, failure.Error)
				return
			}
			WriteJSON(w, http.StatusOK, answer)
		})
	}
	invalid := func(message string) *branchFailure {
		return &branchFailure{Error: platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)}
	}
	body := func(r *http.Request, into any) *branchFailure {
		if err := decodeBodyLimit(r, into, build.BranchMaxSpec+4096); err != nil {
			return invalid("The request must be one valid bounded JSON object")
		}
		return nil
	}
	failed := func(detail any, refusal *kernel.Error) (any, *branchFailure) {
		if refusal != nil {
			return nil, &branchFailure{Error: refusal}
		}
		return detail, nil
	}
	branch(transport.Route{Pattern: "GET /v1/branches", Summary: "Builder-only list of named branches with the resources each changes", Answer: []BranchDetail{}}, func(_ http.ResponseWriter, _ *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		out, refusal := t.ListBranches(m, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches", Summary: "Open a named branch: a metadata overlay whose saves append to a change set; instance data is never part of it", Body: BranchCreateRequest{}, Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchCreateRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, refusal := t.CreateBranch(m, request, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "GET /v1/branches/{id}", Summary: "A branch with its resources, proposal and merge record", Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		out, refusal := t.BranchDetail(m, r.PathValue("id"), h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "GET /v1/branches/{id}/changes", Summary: "The branch change set in save order; specs=true adds each saved spec and base", Query: []transport.Param{{Name: "specs", Description: "true: include base and spec bodies"}}, Answer: []BranchChangeEntry{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		out, refusal := t.BranchChanges(m, r.PathValue("id"), r.URL.Query().Get("specs") == "true", h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "GET /v1/branches/{id}/diff", Summary: "Per resource, the JSON path changes of the branch and of main since the branch's base, their conflicts, and the three-way merged spec", Answer: BranchDiff{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		out, refusal := t.BranchDiff(m, r.PathValue("id"), h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "GET /v1/branches/{id}/resources/{type}/{draft}", Summary: "One resource's base, current main and branch spec", Answer: BranchResourceView{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		out, refusal := t.BranchResource(m, r.PathValue("id"), r.PathValue("type"), r.PathValue("draft"), h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "PUT /v1/branches/{id}/resources/{type}/{draft}", Summary: "Save a resource's full authored spec into the branch (appends to the change set); op delete drops it", Body: BranchSaveRequest{}, Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchSaveRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, refusal := t.SaveBranchResource(m, r.PathValue("id"), r.PathValue("type"), r.PathValue("draft"), request, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/checks", Summary: "Dry-run staged check, recording nothing: three-way conflicts, then the merged resources applied in a staged world, the whole definition graph validated there, and optionally a candidate simulation", Body: BranchCheckRequest{}, Answer: BranchChecks{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchCheckRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, refusal := t.BranchChecks(m, r.PathValue("id"), request, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/recheck", Summary: "Run the staged check again against current main and record it on the branch (open or proposed); the answer carries the recorded check and whether main has moved since", Body: BranchCheckRequest{}, Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchCheckRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, refusal := t.RecheckBranch(m, r.PathValue("id"), request, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/rebase", Summary: "Move the branch's base to current main, three-way merging every changed resource, then record a staged check of the result; unresolved conflicts answer 409 with the conflicts", Body: BranchResolveRequest{}, Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchResolveRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, conflicts, refusal := t.RebaseBranch(m, r.PathValue("id"), request, h.Now())
		if refusal == nil && conflicts != nil {
			return nil, &branchFailure{Conflicts: conflicts}
		}
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/proposal", Summary: "Propose the branch: the staged check of the merged definition graph must pass and is recorded on the proposal; named reviewers must be builders", Body: BranchProposeRequest{}, Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchProposeRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, refusal := t.ProposeBranch(m, r.PathValue("id"), request, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/proposal/review", Summary: "A named reviewer approves or asks for changes", Body: BranchReviewRequest{}, Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchReviewRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, refusal := t.ReviewBranch(m, r.PathValue("id"), request, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/proposal/withdraw", Summary: "Return a proposed branch to editing", Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		out, refusal := t.WithdrawBranch(m, r.PathValue("id"), h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/merge", Summary: "Merge an approved proposal whose last staged check passed against the current main base: one accepted result applies every resource and the merge record, then a sealed release candidate over the merged drafts is saved (not activated); a stale or failed check is refused and nothing is accepted", Body: BranchMergeRequest{}, Answer: BranchMergeResult{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		var request BranchMergeRequest
		if failure := body(r, &request); failure != nil {
			return nil, failure
		}
		out, refusal := t.MergeBranch(m, r.PathValue("id"), request, h.Now())
		return failed(out, refusal)
	})
	branch(transport.Route{Pattern: "POST /v1/branches/{id}/close", Summary: "Abandon the branch", Answer: BranchDetail{}}, func(_ http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) (any, *branchFailure) {
		out, refusal := t.CloseBranch(m, r.PathValue("id"), h.Now())
		return failed(out, refusal)
	})
}

type branchFailure struct {
	Error     *kernel.Error
	Conflicts []BranchResourceDiff
}
