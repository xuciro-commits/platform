package host

import (
	"context"
	"encoding/json"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/ai/gateway"
	"time"
)

func (h *Host) mcp(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
	gateway := aigateway.Tools{Host: t, Now: h.Now}
	gateway.Serve(w, r, m)
}
func (t *Tenant) ModelToolReads(m platform.Member) []string {
	if t.readOrigin == nil {
		values, _ := recordRead(t, context.Background(), func(t *Tenant) ([]string, *kernel.Error) { return t.ModelToolReads(m), nil })
		return values
	}
	var out []string
	for _, app := range t.LiveApps() {
		for _, read := range app.Manifest().Reads {
			if t.mayCallRead(m, read) {
				out = append(out, read)
			}
		}
	}
	return out
}
func (t *Tenant) ProposeModelTool(m platform.Member, proposal platform.ModelToolProposal, at time.Time) *kernel.Error {
	owner, refusal := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) { return t.AuthorityOf(proposal.Action.Target), nil })
	if refusal != nil {
		return refusal
	}
	raw := platform.Raw(map[string]any{"owner": owner, "action": proposal.Action.Schema, "target": proposal.Target, "payload": proposal.Payload, "rationale": "Proposed through MCP"})
	_, refusal = t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: platform.AgentApp, Target: &pb.EntityRef{Type: platform.RunType, Id: proposal.ID}, Schema: &pb.SchemaRef{Name: platform.SchemaToolPropose, Version: 1}, IdempotencyKey: proposal.Key, Payload: raw}, at)
	return refusal
}

func (t *Tenant) ModelToolSurface(m platform.Member) platform.AgentSurface {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (platform.AgentSurface, *kernel.Error) {
		return (hostView{t: t}).AgentSurface(m, ""), nil
	})
	return value
}

func (t *Tenant) AgentObjects(m platform.Member, op string, raw json.RawMessage, now time.Time) (platform.AgentObservation, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (platform.AgentObservation, *kernel.Error) {
		return (hostView{t: t}).AgentObjects(m, op, raw, now)
	})
}
