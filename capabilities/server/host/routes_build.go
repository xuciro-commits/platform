package host

import trialowner "platformserver/services/devops/trials"

import transport "platformserver/kernel/transport"

import "platformserver/services/functions/runtime"

import releaseowner "platformserver/services/devops/releases"

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	pageowner "platformserver/services/apps/pages"
	dataowner "platformserver/services/data"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"

	"platformserver/platform"
)

const releaseCommandBytes = build.JointDraftLimit * 512

func (h *Host) routesBuild(rt *routes) {
	h.routesConnectionKinds(rt)
	h.routesPackages(rt)
	rt.handle(transport.Route{Pattern: "POST /v1/actions/{schema}/check", Summary: "Check any known action's authorization, state and each submission condition for this member and target; conditions that read unsupplied inputs are marked needsInputs and not counted", Body: pageowner.ActionCheckRequest{}, Answer: pageowner.ActionCheckResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request pageowner.ActionCheckRequest
		if decodeBodyLimit(r, &request, 64<<10) != nil || request.Target.Object == "" || request.Target.ID == "" {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an action target and optional inputs"))
			return
		}
		result, refusal := recordRead(t, r.Context(), func(t *Tenant) (pageowner.ActionCheckResult, *kernel.Error) {
			return t.actionCheck(m, r.PathValue("schema"), request, h.Now(), t.Language(m, r))
		})
		transport.Answer(w, result, refusal)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/build/drafts/actions", Summary: "Describe saved object actions for authoring without installing them or granting execution", Answer: []platform.CapabilityDescriptor{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		actions, err := t.DraftActionCapabilities(m)
		transport.AnswerOr(w, http.StatusConflict, actions, err)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/composites", Summary: "Edit several of an application's assets as one unit: every edit is probed, and either all apply or none", Body: struct {
		Key   string                   `json:"key"`
		Edits []platform.CompositeEdit `json:"edits"`
	}{}, Answer: platform.CompositeAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		var request struct {
			Key   string                   `json:"key"`
			Edits []platform.CompositeEdit `json:"edits"`
		}
		if json.Unmarshal(body, &request) != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "a key and its edits are required"})
			return
		}
		answer, err := t.Composite(m, request.Key, request.Edits, h.Now())
		if err != nil {
			failure := map[string]any{"error": err.Error()}
			var refusal compositeRefusal
			if errors.As(err, &refusal) {
				failure["outcome"] = "refused"
			}
			WriteJSON(w, http.StatusConflict, failure)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/build/code/sdk", Summary: "Generate the Go/TinyGo input, output and command wrapper from the same bounded schema", Body: platform.ComputeSDKRequest{}, Answer: platform.ComputeSDK{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, _ *Tenant) {
		var q platform.ComputeSDKRequest
		if !readCapabilityBody(w, r, &q) {
			return
		}
		source, err := compute.GenerateComputeSDK(q.Input, q.Output, q.ABI)
		if err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error()))
			return
		}
		WriteJSON(w, http.StatusOK, platform.ComputeSDK{Source: string(source)})
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/build/process/check", Summary: "Validate a workflow draft with its owner compiler without executing it", Query: []transport.Param{{Name: "branch", Description: "a branch id: the draft is validated against that branch's staged definitions, where objects that exist only on the branch resolve"}}, Body: build.Process{}, Answer: ProcessDiagnostics{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var p build.Process
		if !transport.DecodeValueBody(w, r, &p, 128<<10) {
			return
		}
		answer, refusal := recordRead(t, r.Context(), func(t *Tenant) (ProcessDiagnostics, *kernel.Error) {
			world, refusal := t.draftWorldLocked(m, r.URL.Query().Get("branch"), h.Now())
			if refusal != nil {
				return ProcessDiagnostics{}, refusal
			}
			owner, ok := world.App(build.ID).(build.ConstructionDrafts)
			if !ok {
				return ProcessDiagnostics{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			answer := ProcessDiagnostics{Valid: true, Issues: []ProcessDiagnostic{}}
			if refusal := owner.CheckProcess(platform.NewCaller(runtime{t: world, scoped: true}, m, build.ID, false, false), p); refusal != nil {
				answer.Valid = false
				message := refusal.Message
				node := build.ProcessIssueNode(message)
				answer.Issues = append(answer.Issues, ProcessDiagnostic{Node: node, Code: refusal.Code.String(), Message: message})
			}
			return answer, nil
		})
		transport.Answer(w, answer, refusal)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/releases/candidates", Summary: "Builder and publisher saved release inventory from committed candidate bytes", Query: []transport.Param{{Name: "offset", Description: "Candidates to skip"}, {Name: "limit", Description: "Candidates in the page, 1 to 100 (default 20)"}, {Name: "application", Description: "Application name; filter candidates before pagination"}}, Answer: releaseowner.ReleasePage{}}, build.ID, []string{build.Builder, build.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := transport.PageBounds(r, 0, 20)
		if offset < 0 || limit < 1 || limit > 100 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := t.SavedReleases(m, offset, limit, r.URL.Query().Get("application"))
		if err == nil {
			answer = h.withPromotions(t.ID, answer)
		}
		transport.AnswerOr(w, http.StatusConflict, answer, err)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/releases/candidates/{id}", Summary: "Builder and publisher review of sealed definitions against actual running definitions", Answer: releaseowner.SavedReleaseReview{}}, build.ID, []string{build.Builder, build.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReviewSavedRelease(m, r.PathValue("id"))
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		answer.PromotedTo = h.promotionsOf(t.ID, answer.Preview.CandidateID)
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/releases/drafts/change", Summary: "Builder-only field-level difference between one draft and what is installed", Query: []transport.Param{{Name: "kind", Description: "Draft kind"}, {Name: "id", Description: "Draft id"}}, Answer: build.DraftChange{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.DraftChange(m, platform.AssetKind(r.URL.Query().Get("kind")), r.URL.Query().Get("id"))
		transport.AnswerOr(w, http.StatusBadRequest, answer, err)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/releases/drafts/restore", Summary: "Builder-only action and payload that discard one draft's working changes: an edit back to what is installed, or archiving a draft never installed", Query: []transport.Param{{Name: "kind", Description: "Draft kind"}, {Name: "id", Description: "Draft id"}}, Answer: releaseowner.ReleaseDraftRestore{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.DraftRestore(m, platform.AssetKind(r.URL.Query().Get("kind")), r.URL.Query().Get("id"), h.Now())
		transport.AnswerOr(w, http.StatusBadRequest, answer, err)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/releases/drafts/working", Summary: "Builder-only working state on main: the drafts whose latest edit is the caller's and that differ from what is installed", Answer: releaseowner.ReleaseWorkingState{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.WorkingDrafts(m)
		transport.AnswerOr(w, http.StatusBadRequest, answer, err)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/drafts/referenced", Summary: "Builder-only list of the saved drafts a chosen draft depends on and that are not installed yet; on main an application closure keeps only the caller's working state", Body: releaseowner.ReleaseDraftsRequest{}, Answer: releaseowner.ReleaseDraftClosure{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request releaseowner.ReleaseDraftsRequest
		if !transport.DecodeCommand(w, r, &request, 4096, func() bool { return request.ID != "" }, "a draft kind and id are required") {
			return
		}
		answer, err := t.ReferencedDrafts(m, request.Kind, request.ID, request.Branch, h.Now())
		transport.AnswerOr(w, http.StatusBadRequest, answer, err)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/preview", Summary: "Builder-only read-only comparison of up to 128 saved drafts with their installed development definitions", Body: releaseowner.ReleasePreviewRequest{}, Answer: releaseowner.ReleasePreview{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request releaseowner.ReleasePreviewRequest
		if !transport.DecodeCommand(w, r, &request, releaseCommandBytes, func() bool { return len(request.Drafts) > 0 && len(request.Drafts) <= build.JointDraftLimit }, "between 1 and 128 drafts are required") {
			return
		}
		answer, err := t.PreviewReleaseDrafts(m, request.Drafts, request.Branch, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/candidates", Summary: "Persist exact, immutable bytes for a builder-reviewed candidate; does not activate it", Body: releaseowner.ReleaseSaveRequest{}, Answer: releaseowner.ReleaseSaved{}}, build.ID, []string{build.Builder, build.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request releaseowner.ReleaseSaveRequest
		if !transport.DecodeLimited(w, r, &request, releaseCommandBytes) {
			return
		}
		if !request.Valid() {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.SaveReleaseCandidates(m, request.Refs(), request.CandidateID, request.Key, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if _, err := t.SealCandidate(id, h.Now()); err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": "saved, but not sealed: " + err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, releaseowner.ReleaseSaved{ID: id})
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/evaluations", Summary: "Run the original function or agent evaluator against a saved candidate", Body: ReleaseEvaluationRequest{}, Answer: ReleaseEvaluationStarted{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ReleaseEvaluationRequest
		if !transport.DecodeLimited(w, r, &request, releaseCommandBytes) {
			return
		}
		if request.CandidateID == "" || (request.PlanID == "") == (request.Agent == "") || request.Key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.EvaluateRelease(m, request, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, ReleaseEvaluationStarted{ID: id})
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/active", Summary: "Builder and publisher atomic installation and activation of a saved object/page/application closure", Body: releaseowner.ReleaseActivateRequest{}, Answer: releaseowner.ReleaseActive{}}, build.ID, []string{build.Builder, build.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request releaseowner.ReleaseActivateRequest
		if !transport.DecodeLimited(w, r, &request, releaseCommandBytes) {
			return
		}
		if request.CandidateID == "" || request.Key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.ActivateReleaseWithUpgrade(m, request.CandidateID, request.Key, request.UpgradeID, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, releaseowner.ReleaseActive{ID: id})
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/queries/{app}/{name}", Summary: "Run a declared query as the caller: its conditions, and the record it is run for",
		Query: []transport.Param{{Name: "for", Description: "the ID of the record it is run for, when it takes one"}, {Name: "version", Description: "An exact published source version"}, {Name: "search", Description: "Words to find within the declared conditions"}, {Name: "offset", Description: "Records to skip"}, {Name: "limit", Description: "Records in this bounded window"}}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := pageBounds(r, 0, 0)
		if offset < 0 || limit < 0 {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "offset and limit are non-negative"))
			return
		}
		page, err := t.queryWindowRead(m, r.PathValue("app"), r.PathValue("name"), r.URL.Query().Get("for"), r.URL.Query().Get("version"), platform.Query{Search: r.URL.Query().Get("search"), Offset: offset, Limit: limit}, h.Now())
		transport.Answer(w, page, err)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/pipelines/preview", Summary: "Try the current pipeline draft on authorized dataset samples without publishing or writing output", Body: dataowner.PipelinePreviewRequest{}, Answer: dataowner.PipelinePreview{}}, build.ID, []string{build.Builder, build.Integrator}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request dataowner.PipelinePreviewRequest
		if err := decodeBodyLimit(r, &request, 1<<20); err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The pipeline trial must be a valid bounded JSON object"))
			return
		}
		out, err := t.PreviewPipeline(m, request, h.Now())
		transport.Answer(w, out, err)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/simulate", Summary: "Try an authorized action as yourself in a private decision; only a builder may choose another member",
		Query: []transport.Param{{Name: "as", Description: "the member it is tried as; empty: yourself; another member requires a builder"}, {Name: "branch", Description: "a branch id: the action is tried in that branch's staged world, where resources that exist only on the branch can be tried; requires a builder"}}, Body: []byte{}, Answer: trialowner.Simulation{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s := &pb.Submission{}
		if protojson.Unmarshal(body, s) != nil || s.GetSchema() == nil || s.GetTarget() == nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "a submission with a schema and a target is required"})
			return
		}
		out, err := t.Simulate(m, r.URL.Query().Get("as"), r.URL.Query().Get("branch"), s, h.Now())
		transport.Answer(w, out, err)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/simulate/candidate", Summary: "Test an application draft closure or retained candidate with synthetic samples, controlled members and original executor traces",
		Body: trialowner.CandidateSimulationRequest{}, Answer: trialowner.CandidateSimulation{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request trialowner.CandidateSimulationRequest
		if decodeBodyLimit(r, &request, 1<<20) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The test plan must be a valid bounded JSON object"))
			return
		}
		out, err := t.SimulateCandidate(m, request)
		transport.Answer(w, out, err)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/projects/{id}/access", Summary: "A project's members and roles; owners, builders and administrators also get the people they may add", Answer: platform.ProjectAccessView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		d, _ := t.App(controlowner.PlatformApp).(platform.ProjectAccess)
		if d == nil || !m.Holds(build.ID, build.Builder) && !m.Holds(controlowner.PlatformApp, controlowner.Admin) {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED})
			return
		}
		WriteJSON(w, http.StatusOK, d.ProjectAccess(m, r.PathValue("id")))
	})
	rt.handle(transport.Route{Pattern: "GET /v1/releases/active", Summary: "The tenant's active release ID, readable by any member", Answer: releaseowner.ReleaseActive{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, releaseowner.ReleaseActive{ID: t.ActiveRelease()})
	})
}
