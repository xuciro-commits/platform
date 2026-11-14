package host

import (
	"encoding/json"
	"io"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	transport "platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	"strconv"
	"strings"
	"time"
)

func (h *Host) routesCore(rt *routes) {
	mux := rt.mux
	rt.handle(transport.Route{Pattern: "GET /v1/business-refusals", Summary: "Read the original record's durable refusal and correction evidence", Answer: platform.BusinessRefusalPage{}, Query: []transport.Param{{Name: "type", Description: "original record type"}, {Name: "id", Description: "original record ID"}, {Name: "key", Description: "optional original request key"}, {Name: "offset", Description: "first result"}, {Name: "limit", Description: "at most 200 results"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.BusinessRefusalPage, *kernel.Error) {
		q := r.URL.Query()
		offset, _ := strconv.Atoi(q.Get("offset"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		return t.BusinessRefusals(m, q.Get("type"), q.Get("id"), offset, limit, h.Now(), q.Get("key"))
	}))
	rt.handle(transport.Route{Pattern: "GET /v1/changes", Summary: "Server-sent events: changed for data changes, operations for queue-only progress (F-32)", Answer: transport.LiveQueryFrame{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if r.URL.Query().Has("watch") {
			t.liveFollow(m, mux).Serve(w, r)
			return
		}
		transport.FollowChanges(w, r, t.changeState)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/recovery/retry", Summary: "Retry recovery of this quarantined tenant from the durable journal after an operator repairs its cause", Answer: TenantHealth{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.handle(transport.Route{Pattern: "POST /v1/submissions", Summary: "Submit a decision: an action on a target, received in the kernel's order (K6) and journaled once accepted", Body: pb.Submission{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var sub *pb.Submission
		var record *pb.ChangeRecord
		var refusal *kernel.Error
		defer func() {
			t.observeSubmission(r, m, sub, record, refusal, h.Now())
		}()
		body, _ := io.ReadAll(r.Body)
		if sub, refusal = transport.DecodeSubmission(body, r.Header.Get("Platform-Correction-Of")); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		var issues []platform.FieldIssue
		record, refusal, issues = t.submitSchemaDiagnosed(m, sub, r.Header.Get(SchemaVersionHeader), h.Now())
		localized := t.localizedFieldIssues(m, issues, t.Language(m, r))
		refusal = t.i18n.Said(refusal, t.Language(m, r))
		Reply(w, record, refusal, localized...)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/authz/explain", Summary: "Why a member may or may not exercise a permission: their roles, the roles it names, the engine's verdict (administrators, auditors)", Answer: security.Explanation{}, Query: []transport.Param{{Name: "member", Description: "the member to ask about"}, {Name: "permission", Description: "an action's schema, or <app>:read:<name>"}}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, transport.Answering(func(r *http.Request, _ platform.Member, t *Tenant) (security.Explanation, *kernel.Error) {
		return t.Explain(r.URL.Query().Get("member"), r.URL.Query().Get("permission"))
	}))
	rt.authorized(transport.Route{Pattern: "POST /v1/authz/explain", Summary: "Explain the ordered owner authorization decision for a record, fields and source", Body: platform.AuthzRequest{}, Answer: platform.AuthzDecision{}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request platform.AuthzRequest
		if !readCapabilityBody(w, r, &request) {
			return
		}
		decision, err := t.ExplainOperation(request, h.Now())
		transport.Answer(w, decision, err)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/sessions", Summary: "The caller's sessions: the credentials the host has seen act as them, the current one marked", Answer: []platform.Session{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Sessions(m.ID, bearer(r), h.Now()))
	})
	rt.handle(transport.Route{Pattern: "POST /v1/sessions/end-others", Summary: "End every session of the caller but this one: the host refuses those credentials from now on", Answer: map[string]int{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, map[string]int{"ended": consoleOf(t).EndOtherSessions(m.ID, bearer(r), h.Now())})
	})
	rt.handle(transport.Route{Pattern: "GET /v1/tokens", Summary: "The caller's personal tokens, never their secrets", Answer: []platform.TokenView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Tokens(m.ID, h.Now()))
	})
	rt.handle(transport.Route{Pattern: "GET /v1/tokens/{id}/secret", Summary: "The secret of a token the caller just issued, once", Answer: map[string]string{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (map[string]string, *kernel.Error) {
		secret, ok := consoleOf(t).Minted(r.PathValue("id"), m.ID, time.Now())
		if !ok {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return map[string]string{"secret": secret}, nil
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/{read}", Summary: "A named read of an app, such as inbox, requests, notifications, views, settings, members or audit; the app decides who may read it"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		t.namedRead(w, r, m, t.owner["read:"+r.PathValue("read")], func(app string, inputs json.RawMessage) (any, *kernel.Error) {
			return t.read(m, r.PathValue("read"), inputs)
		})
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/apps/{app}/reads/{read}", Summary: "A named read selected by its original app; the same owner authorization and field filtering apply"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		t.namedRead(w, r, m, r.PathValue("app"), func(app string, inputs json.RawMessage) (any, *kernel.Error) {
			return t.readFromApp(m, app, r.PathValue("read"), inputs)
		})
	})
	rt.public(transport.Route{Pattern: "GET /v1/sign-in", Summary: "How the workspace signs in: the OpenID issuer and client, or development identities", Answer: transport.SignIn{}}, func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, transport.SignInOf(h.Issuer, h.Client, h.Development, h.loginIdentities, h.Mint))
	})
}

func (h *Host) loginIdentities() []platform.LoginIdentity {
	var out []platform.LoginIdentity
	for _, t := range h.currentTenants() {
		if t.identityOwner != nil {
			out = append(out, t.identityOwner.Identities()...)
		}
	}
	return out
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (t *Tenant) namedRead(w http.ResponseWriter, r *http.Request, m platform.Member, app string, read func(app string, inputs json.RawMessage) (any, *kernel.Error)) {
	name := r.PathValue("read")
	inputs, refusal := transport.NamedReadInputs(t.App(app), name, r.URL.Query())
	if refusal != nil {
		Reply(w, nil, refusal)
		return
	}
	out, refusal := read(app, inputs)
	if refusal == nil {
		out = t.i18n.ForRead(name, out, t.Language(m, r))
	}
	transport.Answer(w, out, refusal)
}
