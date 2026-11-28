package host

import (
	"encoding/json"
	"time"

	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/objects"
)

func (h hostView) AgentSuspended(member string) bool { return h.t.suspended(member) }
func (h hostView) EventHops() int                    { return max(0, h.t.hops-1) }

func (h hostView) PermittedActions(m platform.Member) []string {
	catalog := h.t.Catalog(m)
	out := make([]string, 0, len(catalog))
	for _, a := range catalog {
		out = append(out, a.Schema)
	}
	return out
}

func (h hostView) LoadDelegationRecords(member platform.Member, object string, domain json.RawMessage, now time.Time) []map[string]any {
	member.AIContext = true
	caller := platform.NewCaller(runtime{t: h.t}, member, "", false, false)
	page, _, err := h.t.loadObjectSet(h.t.Projection, caller, platform.ObjectSet{Object: object, Domain: domain}, objects.Window{Sort: []string{"id"}, Limit: 50, Bounded: true}, now)
	if err != nil {
		return nil
	}
	raw, _ := json.Marshal(page)
	var decoded struct {
		Records []map[string]any `json:"records"`
	}
	if json.Unmarshal(raw, &decoded) != nil {
		return nil
	}
	return decoded.Records
}

var _ runtimeprotocol.AgentHost = hostView{}

func (t *Tenant) suspended(member string) bool {
	v, ok := t.held(runtimeprotocol.SwitchType + "/" + member)
	return ok && v.(runtimeprotocol.Switch).Suspended
}

func (t *Tenant) SweepDelegations(now time.Time) {
	if t.Agents != nil && !t.businessStopped() && !t.quarantined() {
		t.Agents.SweepDelegations(now, t.Submit)
	}
}

func (t *Tenant) applyAcceptedAgent(raw []byte) (acceptedformat.RuntimeAgent, error) {
	h := runtimekernel.AgentApply{Usage: t.usage(), Batch: t.batchApply(), Published: t.publishAcceptedBatch}
	if t.Agents != nil {
		h.Ledger = t.Agents.Authority()
	}
	return t.ApplyAgentResult(raw, h)
}
