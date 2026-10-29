package platformserver

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	controlowner "platformserver/kernel/control"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (h *Host) routesCore(rt *routes) {
	mux := rt.mux
	rt.handle(transport.Route{Pattern: "GET /v1/business-refusals", Summary: "Read the original record's durable refusal and correction evidence", Answer: platform.BusinessRefusalPage{}, Query: []transport.Param{{Name: "type", Description: "original record type"}, {Name: "id", Description: "original record ID"}, {Name: "key", Description: "optional original request key"}, {Name: "offset", Description: "first result"}, {Name: "limit", Description: "at most 200 results"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, err := t.BusinessRefusals(m, r.URL.Query().Get("type"), r.URL.Query().Get("id"), offset, limit, h.Now(), r.URL.Query().Get("key"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/changes", Summary: "Server-sent events: changed for data changes, operations for queue-only progress (F-32)", Answer: LiveQueryFrame{}},
		func(w http.ResponseWriter, r *http.Request, _ platform.Member, t *Tenant) {
			if r.URL.Query().Has("watch") {
				followLiveQueries(w, r, t, mux)
				return
			}
			followChanges(w, r, t)
		})
	rt.authorized(transport.Route{Pattern: "POST /v1/recovery/retry", Summary: "Retry recovery of this quarantined tenant from the durable journal after an operator repairs its cause",
		Answer: TenantHealth{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
		body, _ := io.ReadAll(r.Body)
		sub := &pb.Submission{}
		if protojson.Unmarshal(body, sub) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		if original := r.Header.Get("Platform-Correction-Of"); original != "" {
			correlation := "correction:" + original
			if sub.CorrelationId != "" && sub.CorrelationId != correlation {
				Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The correction source differs from the submitted identity"))
				return
			}
			sub.CorrelationId = correlation
		}
		record, err, issues := t.submitSchemaDiagnosed(m, sub, r.Header.Get(SchemaVersionHeader), h.Now())
		localized := t.localizedFieldIssues(m, issues, t.Language(m, r))
		Reply(w, record, t.i18n.Said(err, t.Language(m, r)), localized...)
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/authz/explain", Summary: "Why a member may or may not exercise a permission: their roles, the roles it names, the engine's verdict (administrators, auditors)", Answer: Explanation{},
		Query: []transport.Param{{Name: "member", Description: "the member to ask about"}, {Name: "permission", Description: "an action's schema, or <app>:read:<name>"}}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.Explain(r.URL.Query().Get("member"), r.URL.Query().Get("permission"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/authz/explain", Summary: "Explain the ordered owner authorization decision for a record, fields and source", Body: platform.AuthzRequest{}, Answer: platform.AuthzDecision{}}, controlowner.PlatformApp, []string{controlowner.Admin, platform.Auditor}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request platform.AuthzRequest
		if !readCapabilityBody(w, r, &request) {
			return
		}
		decision, err := t.ExplainOperation(request, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, decision)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/sessions", Summary: "The caller's sessions: the credentials the host has seen act as them, the current one marked", Answer: []platform.Session{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Sessions(m.ID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), h.Now()))
	})
	rt.handle(transport.Route{Pattern: "POST /v1/sessions/end-others", Summary: "End every session of the caller but this one: the host refuses those credentials from now on", Answer: map[string]int{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, map[string]int{"ended": consoleOf(t).EndOtherSessions(m.ID, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), h.Now())})
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/tokens", Summary: "The caller's personal tokens, never their secrets", Answer: []platform.TokenView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, consoleOf(t).Tokens(m.ID, h.Now()))
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/tokens/{id}/secret", Summary: "The secret of a token the caller just issued, once", Answer: map[string]string{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		secret, ok := consoleOf(t).Minted(r.PathValue("id"), m.ID, time.Now())
		if !ok {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"secret": secret})
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/{read}", Summary: "A named read of an app, such as inbox, requests, notifications, views, settings, members or audit; the app decides who may read it"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		name := r.PathValue("read")
		inputs, err := namedReadInputs(t.owner["read:"+name], name, r.URL.Query())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		out, err := t.read(m, name, inputs)
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
	rt.recordRead(transport.Route{Pattern: "GET /v1/apps/{app}/reads/{read}", Summary: "A named read selected by its original app; the same owner authorization and field filtering apply"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		name := r.PathValue("read")
		inputs, err := namedReadInputs(t.app(r.PathValue("app")), name, r.URL.Query())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		out, err := t.readFromApp(m, r.PathValue("app"), name, inputs)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if declarationReads[name] {
			out = t.i18n.Translate(out, t.Language(m, r))
		}
		if messageReads[name] {
			out = t.i18n.TranslateMessages(out, t.Language(m, r))
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.public(transport.Route{Pattern: "GET /v1/sign-in", Summary: "How the workspace signs in: the OpenID issuer and client, or development identities", Answer: SignIn{}}, func(w http.ResponseWriter, _ *http.Request) {
		out := SignIn{}
		if h.Issuer != "" {
			out.Issuer, out.Client = h.Issuer, h.Client
		} else if h.Development {
			out.Identities = []platform.LoginIdentity{}
			for _, t := range h.currentTenants() {
				if d, ok := t.app(controlowner.PlatformApp).(platform.HostIdentities); ok {
					out.Identities = append(out.Identities, d.Identities()...)
				}
			}
			if h.Mint != nil {
				for i := range out.Identities {
					out.Identities[i].Token = h.Mint(out.Identities[i].Token)
				}
			}
		}
		WriteJSON(w, http.StatusOK, out)
	})
}

func namedReadInputs(app platform.App, name string, query url.Values) (json.RawMessage, *kernel.Error) {
	if app == nil {
		return nil, nil
	}
	fields, declared := app.Manifest().ReadInputs[name]
	if !declared {
		return nil, nil
	}
	values := map[string]any{}
	for _, field := range fields {
		if !query.Has(field.Name) {
			continue
		}
		text := query.Get(field.Name)
		var value any = text
		var err error
		switch field.Type {
		case "integer":
			value, err = strconv.ParseInt(text, 10, 64)
		case "number":
			value, err = strconv.ParseFloat(text, 64)
		case "boolean":
			value, err = strconv.ParseBool(text)
		}
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, fmt.Sprintf("Read input %s must be %s", field.Name, field.Type))
		}
		values[field.Name] = value
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Read inputs cannot be encoded as JSON")
	}
	return raw, nil
}
