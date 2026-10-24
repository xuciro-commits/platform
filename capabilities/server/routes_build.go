package platformserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
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
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
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
	rt.handle(Route{Pattern: "GET /v1/releases/active", Summary: "The tenant's active release ID, readable by any member", Answer: ReleaseActive{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, ReleaseActive{ID: t.ActiveRelease()})
	})
}
