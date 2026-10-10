package platformserver

import (
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// routesCore serves the submit pipeline, live changes, recovery, authz, sessions and named reads.
func (h *Host) routesCore(rt *routes) {
	mux := rt.mux
	rt.handle(Route{Pattern: "GET /v1/changes", Summary: "Server-sent events: changed for data changes, operations for queue-only progress (F-32)", Answer: LiveQueryFrame{}},
		func(w http.ResponseWriter, r *http.Request, _ platform.Member, t *Tenant) {
			if r.URL.Query().Has("watch") {
				followLiveQueries(w, r, t, mux)
				return
			}
			followChanges(w, r, t)
		})
	rt.handle(Route{Pattern: "POST /v1/recovery/retry", Summary: "Retry recovery of this quarantined tenant from the durable journal after an operator repairs its cause (ADR-0038)",
		Answer: TenantHealth{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if m.Roles[PlatformApp] != Admin {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if h.Recover == nil {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		if err := h.Recover(r.Context(), t.ID); err != nil {
			WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "health": t.Health(h.Now())})
			return
		}
		for _, current := range h.currentTenants() {
			if current.ID == t.ID {
				WriteJSON(w, http.StatusOK, current.Health(h.Now()))
				return
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	rt.handle(Route{Pattern: "POST /v1/submissions", Summary: "Submit a decision: an action on a target, received in the kernel's order (K6) and journaled once accepted", Body: pb.Submission{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		record, err, issues := t.submitDiagnosed(m, sub, h.Now())
		localized := append([]platform.FieldIssue(nil), issues...)
		for i := range localized {
			localized[i].Message = t.i18n.said(&kernel.Error{Message: localized[i].Message}, t.Language(m, r)).Message
		}
		Reply(w, record, t.i18n.said(err, t.Language(m, r)), localized...)
	})
	rt.handle(Route{Pattern: "GET /v1/authz/explain", Summary: "Why a member may or may not exercise a permission: their roles, the roles it names, the engine's verdict (administrators, auditors; ADR-0078)", Answer: Explanation{},
		Query: []Param{{"member", "the member to ask about"}, {"permission", "an action's schema, or <app>:read:<name>"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if !m.Holds(PlatformApp, Admin) && !m.Holds(PlatformApp, Auditor) {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED})
			return
		}
		out, err := t.Explain(r.URL.Query().Get("member"), r.URL.Query().Get("permission"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.handle(Route{Pattern: "GET /v1/sessions", Summary: "The caller's sessions: the credentials the host has seen act as them, the current one marked (ADR-0079 §5)", Answer: []Session{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Sessions(m.ID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), h.Now()))
	})
	rt.handle(Route{Pattern: "POST /v1/sessions/end-others", Summary: "End every session of the caller but this one: the host refuses those credentials from now on (ADR-0079 §5)", Answer: map[string]int{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, map[string]int{"ended": consoleOf(t).EndOtherSessions(m.ID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), h.Now())})
	})
	rt.handle(Route{Pattern: "GET /v1/tokens", Summary: "The caller's personal tokens, never their secrets (ADR-0079 §5)", Answer: []TokenView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Tokens(m.ID, h.Now()))
	})
	rt.handle(Route{Pattern: "GET /v1/tokens/{id}/secret", Summary: "The secret of a token the caller just issued, once (ADR-0079 §5)", Answer: map[string]string{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		secret, ok := consoleOf(t).Minted(r.PathValue("id"), m.ID, time.Now())
		if !ok {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"secret": secret})
	})
	rt.handle(Route{Pattern: "GET /v1/{read}", Summary: "A named read of an app, such as inbox, requests, notifications, views, settings, members or audit; the app decides who may read it"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.Read(m, r.PathValue("read"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if declarationReads[r.PathValue("read")] {
			out = t.i18n.Translate(out, t.Language(m, r))
		}
		if messageReads[r.PathValue("read")] {
			out = t.i18n.TranslateMessages(out, t.Language(m, r))
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.public(Route{Pattern: "GET /v1/sign-in", Summary: "How the workspace signs in: the OpenID issuer and client, or development identities", Answer: SignIn{}}, func(w http.ResponseWriter, _ *http.Request) {
		out := SignIn{}
		if h.Issuer != "" {
			out.Issuer, out.Client = h.Issuer, h.Client
		} else if h.Development {
			out.Identities = []Identity{}
			for _, t := range h.currentTenants() {
				if d := consoleOf(t); d != nil {
					out.Identities = append(out.Identities, d.Identities()...)
				}
			}
			if h.Mint != nil {
				for i := range out.Identities { // the lightweight host signs what it serves
					out.Identities[i].Token = h.Mint(out.Identities[i].Token)
				}
			}
		}
		WriteJSON(w, http.StatusOK, out)
	})
}
