package host

import (
	"encoding/json"
	"net/http"
	"platformserver/services/actions"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) published(agent string) bool {
	if t.agents == nil {
		return false
	}
	if _, ok := t.agents.AgentDeclaration(agent); !ok {
		return false
	}
	list := strings.Split(t.setting(t.automation(platform.AgentApp, false), platform.SettingPublished), ",")
	return slices.ContainsFunc(list, func(s string) bool { return strings.TrimSpace(s) == agent })
}

func (h *Host) serveA2A(w http.ResponseWriter, r *http.Request) {
	tenant, agent := r.PathValue("tenant"), r.PathValue("agent")
	r.Header.Set(TenantHeader, tenant)
	m, t, ok := h.member(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	WriteJSON(w, http.StatusOK, actions.A2AServe(r.Body, r.Header.Get("A2A-Version"), agent, m.ID, actions.A2AHooks{
		Published: t.published,
		Run: func(id string) (platform.AgentRunRecord, bool) {
			value, refusal := recordRead(t, r.Context(), func(t *Tenant) (pair[platform.AgentRunRecord, bool], *kernel.Error) {
				x, ok := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), id)
				return pair[platform.AgentRunRecord, bool]{x, ok}, nil
			})
			return value.first, value.second && refusal == nil
		},
		Decide: func(app, schema, typ, id, key string, payload any) error {
			raw, _ := json.Marshal(payload)
			if _, refusal := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: app, IdempotencyKey: key,
				Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, h.Now()); refusal != nil {
				return refusal
			}
			return nil
		},
	}))
}
