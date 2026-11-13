package host

import (
	"cmp"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

func (t *Tenant) resourceActionActive(name string) bool {
	target := ""
	if owner := t.App(t.owner["action:"+name]); owner != nil {
		action, _ := owner.Manifest().Actions.Action(name)
		target = action.Target
	}
	return !build.ResourceActionRetired(t.Projection, name, target)
}

func (t *Tenant) ResourceRole(m platform.Member, project string) string {
	access, _ := t.App("platform").(platform.ProjectAccess)
	return build.ProjectRoleOf(m, access, project)
}

func (t *Tenant) ResourcePermission(store *records.Store, m platform.Member, typ, id, kind string, held bool) (bool, bool) {
	if !build.ProjectDelegated(m) || id == "" {
		return true, false
	}
	if !held {
		store.Lock()
		defer store.Unlock()
	}
	return build.StoreResourceRows(store, func(project string) string { return t.ResourceRole(m, project) }).Permission(m, typ, id, kind)
}

func (t *Tenant) resourceDirectory(m platform.Member) build.ResourceDirectory {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return build.StoreDirectory(t.Projection, func(project string) string { return t.ResourceRole(m, project) })
}

func (t *Tenant) initializeResources(now time.Time) error {
	owner := t.App(build.ID)
	if owner == nil {
		return nil
	}
	t.Lane.Lock()
	defer t.Lane.Unlock()
	if refusal := t.recordReadRefusal(); refusal != nil {
		return refusal
	}
	return build.StoreBootstrap(t.ID, t.definitions, t.Projection, func(s *pb.Submission) error {
		if refusal := t.submitAutomated(owner, t.automation(build.ID, false), s, now); refusal != nil {
			return refusal
		}
		return nil
	}).Initialize()
}

func (h *Host) routesResources(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/markings/{id}/resources", Summary: "Resources carrying a marking, without their data", Answer: []build.ResourceEntry{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) ([]build.ResourceEntry, *kernel.Error) {
		caller := t.caller(m, t.App("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(caller, "GET /v1/markings/{id}/resources", "read", []string{"admin", platform.Auditor}, true)); refusal != nil {
			return nil, refusal
		}
		marked := t.markingsIn(t.Projection, false).Marked(r.PathValue("id"))
		t.Projection.Lock()
		resources := build.StoreResources(t.Projection)
		t.Projection.Unlock()
		return build.MarkedResources(marked, resources, func(project string) string { return t.ResourceRole(m, project) }), nil
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/branches/{id}/reviewers", Summary: "Eligible non-author editors and the project approval requirement", Answer: build.BranchReviewers{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		branch, refusal := t.ReadBranch(m, r.PathValue("id"), h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		projects, required, err := t.App(build.ID).(*build.Build).BranchReviewRequirement(t.branchCaller(m), branch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		WriteJSON(w, http.StatusOK, build.ReviewerHooks{Holding: t.identity.Holding(build.ID, build.Builder), Member: t.Member, Role: t.ResourceRole,
			Name: func(id string) string {
				profile, _ := t.App("platform").(platform.IdentityProfile)
				if profile == nil {
					return id
				}
				return cmp.Or(profile.Account(id).DisplayName, id)
			}}.Eligible(branch, m.ID, projects, required))
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/working-state/{branch}", Summary: "All current builder's private resource edits on this branch", Answer: build.WorkingStateList{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		list, refusal, err := build.ReadWorkingStates(r.PathValue("branch"), func(query platform.Query) ([]any, *kernel.Error) {
			page, refusal := t.recordsRead(m, build.WorkingStateType, query, h.Now())
			return page.Records, refusal
		})
		if err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		transport.Answer(w, list, refusal)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/working-state/{branch}/{type}/{draft}", Summary: "Only the current builder's private working state", Answer: build.WorkingStateView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		id := build.WorkingStateID(m.ID, r.PathValue("branch"), r.PathValue("type"), r.PathValue("draft"))
		raw, refusal := t.readRecordFrom(t.Projection, t.branchCaller(m), build.WorkingStateType, id, h.Now())
		view, refusal, err := build.ReadWorkingState(id, raw, refusal)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		transport.Answer(w, view, refusal)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/resources", Summary: "Visible resource identities and their project and folder locations", Answer: build.ResourceDirectory{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.resourceDirectory(m))
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/resources/{rid}", Summary: "Resolve one stable resource identity", Answer: build.ResourceEntry{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (build.ResourceEntry, *kernel.Error) {
		entries := t.resourceDirectory(m).Resources
		index := slices.IndexFunc(entries, func(entry build.ResourceEntry) bool { return entry.ID == r.PathValue("rid") })
		if index < 0 {
			return build.ResourceEntry{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The resource is unavailable")
		}
		return entries[index], nil
	}))
}

func (h hostView) ManagedDefinition(ref platform.AssetRef) bool {
	store := h.t.Projection
	if h.decision != nil {
		store = h.decision.Records
	}
	return build.ManagedDefinition(store, h.t.ID, ref)
}
