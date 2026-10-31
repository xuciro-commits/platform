package platformserver

import trialowner "platformserver/studio/trials"

import transport "platformserver/kernel/transport"

import computeowner "platformserver/engine/compute"

import releaseowner "platformserver/studio/releases"

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	controlowner "platformserver/kernel/control"
	"platformserver/studio/build"

	"platformserver/platform"
)

const releaseCommandBytes = build.JointDraftLimit * 512

type ActionCheckRequest struct {
	Target PageModelRecord `json:"target"`
	Inputs json.RawMessage `json:"inputs,omitempty"`
}

type ActionConditionCheck struct {
	Allowed     bool   `json:"allowed"`
	NeedsInputs bool   `json:"needsInputs,omitempty"`
	Message     string `json:"message"`
}

type ActionCheckResult struct {
	Allowed       bool                    `json:"allowed"`
	Message       string                  `json:"message,omitempty"`
	Conditions    []ActionConditionCheck  `json:"conditions"`
	Issues        []platform.FieldIssue   `json:"issues,omitempty"`
	Authorization *platform.AuthzDecision `json:"authorization,omitempty"`
}

func decodeReleaseBody(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := decodeBodyLimit(r, into, releaseCommandBytes); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		return false
	}
	return true
}

func (h *Host) routesBuild(rt *routes) {
	h.routesConnectionKinds(rt)
	h.routesPackageDrafts(rt)
	rt.handle(transport.Route{Pattern: "POST /v1/actions/{schema}/check", Summary: "Check any known action's authorization, state and each submission condition for this member and target; conditions that read unsupplied inputs are marked needsInputs and not counted", Body: ActionCheckRequest{}, Answer: ActionCheckResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ActionCheckRequest
		if decodeBodyLimit(r, &request, 64<<10) != nil || request.Target.Object == "" || request.Target.ID == "" {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an action target and optional inputs"))
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		result, refusal := t.actionCheck(m, r.PathValue("schema"), request, h.Now(), t.Language(m, r))
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/build/drafts/actions", Summary: "Describe saved object actions for authoring without installing them or granting execution", Answer: []platform.CapabilityDescriptor{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		actions, err := t.DraftActionCapabilities(m)
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, actions)
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
		source, err := computeowner.GenerateComputeSDK(q.Input, q.Output, q.ABI)
		if err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error()))
			return
		}
		WriteJSON(w, http.StatusOK, platform.ComputeSDK{Source: string(source)})
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/build/process/check", Summary: "Validate a workflow draft with its owner compiler without executing it", Query: []transport.Param{{Name: "branch", Description: "a branch id: the draft is validated against that branch's staged definitions, where objects that exist only on the branch resolve"}}, Body: build.Process{}, Answer: ProcessDiagnostics{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var p build.Process
		raw, err := io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
		if err != nil || len(raw) > 128<<10 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if _, err = platform.DecodeValue(raw, 128<<10); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		world, refusal := t.draftWorldLocked(m, r.URL.Query().Get("branch"), h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		owner, ok := world.app(build.ID).(build.ConstructionDrafts)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		answer := ProcessDiagnostics{Valid: true, Issues: []ProcessDiagnostic{}}
		if refusal := owner.CheckProcess(platform.NewCaller(runtime{world}, m, build.ID, false, false), p); refusal != nil {
			answer.Valid = false
			message := refusal.Message
			node := ""
			parts := strings.SplitN(message, ": ", 2)
			if len(parts) == 2 {
				if strings.HasPrefix(parts[0], "step ") {
					node = strings.TrimPrefix(parts[0], "step ")
				}
				if strings.HasPrefix(parts[0], "Node ") {
					node = strings.TrimPrefix(parts[0], "Node ")
				}
			}
			answer.Issues = append(answer.Issues, ProcessDiagnostic{Node: node, Code: refusal.Code.String(), Message: message})
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/releases/candidates", Summary: "Builder and publisher saved release inventory from committed candidate bytes", Query: []transport.Param{{Name: "offset", Description: "Candidates to skip"}, {Name: "limit", Description: "Candidates in the page, 1 to 100 (default 20)"}, {Name: "application", Description: "Application name; filter candidates before pagination"}}, Answer: releaseowner.ReleasePage{}}, build.ID, []string{build.Builder, build.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := 0, 20
		for name, target := range map[string]*int{"offset": &offset, "limit": &limit} {
			if raw := r.URL.Query().Get(name); raw != "" {
				value, err := strconv.Atoi(raw)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				*target = value
			}
		}
		if offset < 0 || limit < 1 || limit > 100 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := t.SavedReleases(m, offset, limit, r.URL.Query().Get("application"))
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, h.withPromotions(t.ID, answer))
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
	rt.authorized(transport.Route{Pattern: "GET /v1/releases/drafts/working", Summary: "Builder-only working state on main: the drafts whose latest edit is the caller's and that differ from what is installed", Answer: releaseowner.ReleaseWorkingState{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.WorkingDrafts(m)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/drafts/referenced", Summary: "Builder-only list of the saved drafts a chosen draft depends on and that are not installed yet; on main an application closure keeps only the caller's working state", Body: releaseowner.ReleaseDraftsRequest{}, Answer: releaseowner.ReleaseDraftClosure{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
		if readErr != nil || len(body) > 4096 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var request releaseowner.ReleaseDraftsRequest
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.ID == "" {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "a draft kind and id are required"})
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := t.ReferencedDrafts(m, request.Kind, request.ID, request.Branch, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/preview", Summary: "Builder-only read-only comparison of up to 128 saved drafts with their installed development definitions", Body: releaseowner.ReleasePreviewRequest{}, Answer: releaseowner.ReleasePreview{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, releaseCommandBytes+1))
		if readErr != nil || len(body) > releaseCommandBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var request releaseowner.ReleasePreviewRequest
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || len(request.Drafts) == 0 || len(request.Drafts) > build.JointDraftLimit {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "between 1 and 128 drafts are required"})
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := t.PreviewReleaseDrafts(m, request.Drafts, request.Branch, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	draftsOf := func(request releaseowner.ReleaseSaveRequest) []build.JointDraftRef {
		if len(request.Drafts) > 0 {
			return request.Drafts
		}
		return []build.JointDraftRef{{Kind: request.Kind, ID: request.ID}}
	}
	rt.authorized(transport.Route{Pattern: "POST /v1/releases/candidates", Summary: "Persist exact, immutable bytes for a builder-reviewed candidate; does not activate it", Body: releaseowner.ReleaseSaveRequest{}, Answer: releaseowner.ReleaseSaved{}}, build.ID, []string{build.Builder, build.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request releaseowner.ReleaseSaveRequest
		if !decodeReleaseBody(w, r, &request) {
			return
		}
		if (request.ID == "" && len(request.Drafts) == 0) ||
			(request.ID != "" && len(request.Drafts) > 0) || len(request.Drafts) > build.JointDraftLimit ||
			request.CandidateID == "" || request.Key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.SaveReleaseCandidates(m, draftsOf(request), request.CandidateID, request.Key, h.Now())
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
		if !decodeReleaseBody(w, r, &request) {
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
		if !decodeReleaseBody(w, r, &request) {
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
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/pipelines/preview", Summary: "Try the current pipeline draft on authorized dataset samples without publishing or writing output", Body: PipelinePreviewRequest{}, Answer: PipelinePreview{}}, build.ID, []string{build.Builder, build.Integrator}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request PipelinePreviewRequest
		if err := decodeBodyLimit(r, &request, 1<<20); err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The pipeline trial must be a valid bounded JSON object"))
			return
		}
		out, err := t.PreviewPipeline(m, request, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
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
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/simulate/candidate", Summary: "Test an application draft closure or retained candidate with synthetic samples, controlled members and original executor traces",
		Body: trialowner.CandidateSimulationRequest{}, Answer: trialowner.CandidateSimulation{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request trialowner.CandidateSimulationRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The test plan must be a valid bounded JSON object"))
			return
		}
		out, err := t.SimulateCandidate(m, request)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/projects/{id}/access", Summary: "A project's members and roles; owners, builders and administrators also get the people they may add", Answer: platform.ProjectAccessView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		d, _ := t.app(controlowner.PlatformApp).(platform.ProjectAccess)
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
