package platformserver

import (
	"encoding/json"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/aigateway"
	"platformserver/platform"
	"time"
)

func (h *Host) mcp(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
	gateway := aigateway.Tools{Host: t, Now: h.Now}
	gateway.Serve(w, r, m)
}
func (t *Tenant) ModelToolReads(m platform.Member) []string {
	var out []string
	for _, app := range t.liveApps() {
		for _, read := range app.Manifest().Reads {
			if t.mayCallRead(m, read) {
				out = append(out, read)
			}
		}
	}
	return out
}
func (t *Tenant) ProposeModelTool(m platform.Member, proposal platform.ModelToolProposal, at time.Time) *kernel.Error {
	raw := platform.Raw(map[string]any{"owner": t.authorityOf(proposal.Action.Target), "action": proposal.Action.Schema, "target": proposal.Target, "payload": proposal.Payload, "rationale": "Proposed through MCP"})
	_, refusal := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: platform.AgentApp, Target: &pb.EntityRef{Type: platform.RunType, Id: proposal.ID}, Schema: &pb.SchemaRef{Name: platform.SchemaToolPropose, Version: 1}, IdempotencyKey: proposal.Key, Payload: raw}, at)
	return refusal
}

func (t *Tenant) ModelToolSurface(m platform.Member) platform.AgentSurface {
	return (hostView{t: t}).AgentSurface(m, "")
}

func (t *Tenant) AgentObjects(m platform.Member, op string, raw json.RawMessage, now time.Time) (platform.AgentObservation, *kernel.Error) {
	return (hostView{t: t}).AgentObjects(m, op, raw, now)
}
