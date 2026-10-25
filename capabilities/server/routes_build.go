package platformserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

const releaseCommandBytes = construction.JointDraftLimit * 512

// Release commands accept bounded metadata for the original application closure.
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

// routesBuild serves the builder: composites, code SDK, process checks, releases, queries and simulation.
func (h *Host) routesBuild(rt *routes) {
	h.routesProtocolConnections(rt)
	h.routesPackageDrafts(rt)
	rt.authorized(Route{Pattern: "GET /v1/build/drafts/actions", Summary: "Describe saved object actions for authoring without installing them or granting execution", Answer: []platform.CapabilityDescriptor{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		actions, err := t.DraftActionCapabilities(m)
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, actions)
	})
	rt.handle(Route{Pattern: "POST /v1/composites", Summary: "Edit several of an application's assets as one unit: every edit is probed, and either all apply or none", Body: struct {
		Key   string          `json:"key"`
		Edits []CompositeEdit `json:"edits"`
	}{}, Answer: CompositeAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		var request struct {
			Key   string          `json:"key"`
			Edits []CompositeEdit `json:"edits"`
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
	rt.authorized(Route{Pattern: "POST /v1/build/code/sdk", Summary: "Generate the Go/TinyGo input, output and command wrapper from the same bounded schema", Body: ComputeSDKRequest{}, Answer: ComputeSDK{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, _ *Tenant) {
		var q ComputeSDKRequest
		if !readCapabilityBody(w, r, &q) {
			return
		}
		source, err := GenerateComputeSDK(q.Input, q.Output, q.ABI)
		if err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error()))
			return
		}
		WriteJSON(w, http.StatusOK, ComputeSDK{Source: string(source)})
	})
	rt.authorized(Route{Pattern: "POST /v1/build/process/check", Summary: "Validate a workflow draft with its owner compiler without executing it", Body: construction.Process{}, Answer: ProcessDiagnostics{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var p construction.Process
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
		owner, ok := t.app(construction.ID).(host.ConstructionDrafts)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		answer := ProcessDiagnostics{Valid: true, Issues: []ProcessDiagnostic{}}
		if refusal := owner.CheckProcess(platform.NewCaller(runtime{t}, m, construction.ID, false, false), p); refusal != nil {
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
	rt.authorized(Route{Pattern: "GET /v1/releases/candidates", Summary: "Builder and publisher saved release inventory from committed candidate bytes", Query: []Param{{"offset", "Candidates to skip"}, {"limit", "Candidates in the page, 1 to 100 (default 20)"}, {"application", "Application name; filter candidates before pagination"}}, Answer: ReleasePage{}}, construction.ID, []string{construction.Builder, construction.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.authorized(Route{Pattern: "POST /v1/releases/migrations/review", Summary: "Review explicit migration steps between sealed releases without changing data", Body: platform.MigrationPlan{}, Answer: MigrationReview{}}, construction.ID, []string{construction.Builder, construction.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var plan platform.MigrationPlan
		if err := json.Unmarshal(body, &plan); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		answer, err := t.ReviewMigrationPlan(m, plan, Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(Route{Pattern: "GET /v1/releases/candidates/{id}", Summary: "Builder and publisher review of sealed definitions against actual running definitions", Answer: SavedReleaseReview{}}, construction.ID, []string{construction.Builder, construction.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReviewSavedRelease(m, r.PathValue("id"))
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		answer.PromotedTo = h.promotionsOf(t.ID, answer.Preview.CandidateID)
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(Route{Pattern: "GET /v1/releases/drafts/changes", Summary: "Read saved authoring changes against original retained publications without creating a candidate", Answer: ReleaseDraftChanges{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.SavedDraftChanges(m, r.URL.Query().Get("application"), r.URL.Query().Get("type"), r.URL.Query().Get("id"))
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(Route{Pattern: "POST /v1/releases/drafts/referenced", Summary: "Builder-only list of the saved record drafts a chosen draft depends on and that are not installed yet", Body: ReleaseDraftsRequest{}, Answer: ReleaseDraftClosure{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
		if readErr != nil || len(body) > 4096 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var request ReleaseDraftsRequest
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
		answer, err := t.ReferencedDrafts(m, request.Kind, request.ID)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.authorized(Route{Pattern: "POST /v1/releases/preview", Summary: "Builder-only read-only comparison of one saved draft with its installed development definition", Body: ReleasePreviewRequest{}, Answer: ReleasePreview{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, readErr := io.ReadAll(io.LimitReader(r.Body, releaseCommandBytes+1))
		if readErr != nil || len(body) > releaseCommandBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var request ReleasePreviewRequest
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil ||
			(request.ID == "" && len(request.Drafts) == 0) ||
			(request.ID != "" && len(request.Drafts) > 0) ||
			len(request.Drafts) > construction.JointDraftLimit {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "one draft kind and id, or up to 128 joint drafts, are required"})
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var answer ReleasePreview
		var err error
		if request.ApplicationDrafts {
			if request.Kind != platform.AssetApp || request.ID == "" || len(request.Drafts) > 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			answer, err = t.PreviewApplicationDrafts(m, request.ID)
		} else if len(request.Drafts) > 0 {
			answer, err = t.PreviewReleaseDrafts(m, request.Drafts)
		} else {
			answer, err = t.PreviewRelease(m, request.Kind, request.ID)
		}
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	// One request carries either a single draft or a joint selection.
	draftsOf := func(request ReleaseSaveRequest) []construction.JointDraftRef {
		if len(request.Drafts) > 0 {
			return request.Drafts
		}
		return []construction.JointDraftRef{{Kind: request.Kind, ID: request.ID}}
	}
	rt.authorized(Route{Pattern: "POST /v1/releases/candidates", Summary: "Persist exact, immutable bytes for a builder-reviewed candidate; does not activate it", Body: ReleaseSaveRequest{}, Answer: ReleaseSaved{}}, construction.ID, []string{construction.Builder, construction.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ReleaseSaveRequest
		if !decodeReleaseBody(w, r, &request) {
			return
		}
		if (request.ID == "" && len(request.Drafts) == 0) ||
			(request.ID != "" && len(request.Drafts) > 0) || len(request.Drafts) > construction.JointDraftLimit ||
			request.CandidateID == "" || request.Key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, err := t.SaveReleaseCandidates(m, draftsOf(request), request.CandidateID, request.Key, h.Now())
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		// A saved candidate is sealed at once: its exact bytes become an
		// addressable artifact in the file store, which promotion to another
		// environment reads. The save stands if sealing fails;
		// the next save or promotion seals the same bytes again.
		if _, err := t.SealCandidate(id, h.Now()); err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": "saved, but not sealed: " + err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, ReleaseSaved{ID: id})
	})
	rt.authorized(Route{Pattern: "POST /v1/releases/evaluations", Summary: "Run the original function or agent evaluator against a saved candidate", Body: ReleaseEvaluationRequest{}, Answer: ReleaseEvaluationStarted{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.authorized(Route{Pattern: "POST /v1/releases/active", Summary: "Builder and publisher atomic installation and activation of a saved object/page/application closure", Body: ReleaseActivateRequest{}, Answer: ReleaseActive{}}, construction.ID, []string{construction.Builder, construction.Publisher}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ReleaseActivateRequest
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
		WriteJSON(w, http.StatusOK, ReleaseActive{ID: id})
	})
	rt.recordRead(Route{Pattern: "GET /v1/queries/{app}/{name}", Summary: "Run a declared query as the caller: its conditions, and the record it is run for",
		Query: []Param{{"for", "the ID of the record it is run for, when it takes one"}, {"version", "An exact published source version"}, {"search", "Words to find within the declared conditions"}, {"offset", "Records to skip"}, {"limit", "Records in this bounded window"}}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.authorized(Route{Pattern: "POST /v1/pipelines/preview", Summary: "Try the current pipeline draft on authorized dataset samples without publishing or writing output", Body: PipelinePreviewRequest{}, Answer: PipelinePreview{}}, construction.ID, []string{construction.Builder, construction.Integrator}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.handle(Route{Pattern: "POST /v1/simulate", Summary: "Try an authorized action as yourself in a private decision; only a builder may choose another member",
		Query: []Param{{"as", "the member it is tried as; empty: yourself; another member requires a builder"}}, Body: []byte{}, Answer: Simulation{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s := &pb.Submission{}
		if protojson.Unmarshal(body, s) != nil || s.GetSchema() == nil || s.GetTarget() == nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "a submission with a schema and a target is required"})
			return
		}
		out, err := t.Simulate(m, r.URL.Query().Get("as"), s, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.authorized(Route{Pattern: "POST /v1/simulate/candidate", Summary: "Test an application draft closure or retained candidate with synthetic samples, controlled members and original executor traces",
		Body: CandidateSimulationRequest{}, Answer: CandidateSimulation{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request CandidateSimulationRequest
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
	rt.handle(Route{Pattern: "GET /v1/releases/active", Summary: "The tenant's active release ID, readable by any member", Answer: ReleaseActive{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, ReleaseActive{ID: t.ActiveRelease()})
	})
}
