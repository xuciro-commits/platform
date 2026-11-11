package kernel

import (
	"encoding/json"
	"fmt"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type UsageApply struct {
	Usage func() (json.RawMessage, error)
	Meter func(platform.AIUsage)
}

func (u UsageApply) Predecessor() (string, error) {
	if u.Usage == nil {
		return "", fmt.Errorf("usage needs an AI meter")
	}
	raw, err := u.Usage()
	if err != nil {
		return "", err
	}
	return platform.CanonicalDigest(raw)
}

func (s *Sequence) ApplyUsageResult(raw []byte, h UsageApply) (acceptedformat.Usage, error) {
	r, err := acceptedformat.DecodeUsage(raw)
	if err != nil {
		return r, err
	}
	before, err := h.Predecessor()
	if err != nil || r.Tenant != s.ID || before != r.Before {
		return r, fmt.Errorf("model usage predecessor differs: %v", err)
	}
	h.Meter(r.Usage)
	return r, nil
}

type AgentApply struct {
	Usage     UsageApply
	Ledger    *platform.Ledger
	Batch     BatchApply
	Published func(acceptedformat.Batch)
}

func (s *Sequence) ApplyAgentResult(raw []byte, h AgentApply) (acceptedformat.RuntimeAgent, error) {
	r, err := acceptedformat.DecodeAgent(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != s.ID || h.Ledger == nil {
		return r, fmt.Errorf("agent result belongs to another tenant")
	}
	if r.UsageBefore != "" {
		before, err := h.Usage.Predecessor()
		if err != nil || before != r.UsageBefore {
			return r, fmt.Errorf("agent usage predecessor differs: %v", err)
		}
	}
	batch, _, _ := acceptedformat.DecodeBatch(r.Changes)
	prepared, err := s.PrepareBatch(h.Ledger, r.Changes, h.Batch)
	applied := false
	if err == nil && prepared != nil {
		applied, err = s.PromoteBatch(prepared, h.Batch)
	}
	if err != nil || !applied {
		return r, fmt.Errorf("agent result application: %v (applied=%t)", err, applied)
	}
	if r.UsageBefore != "" {
		h.Usage.Meter(r.Step.Usage)
	}
	h.Published(batch)
	return r, nil
}
