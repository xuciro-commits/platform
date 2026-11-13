package aigateway

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"slices"
)

type UsageAcceptance struct {
	Tenant      string
	Prior       func() []platform.AIUsage
	Caller      func(platform.Member) platform.Caller
	Authorize   func(c platform.Caller, valid bool) (platform.AuthzDecision, *kernel.Error)
	Predecessor func() (string, error)
	Apply       func([]byte) error
	Quarantine  func(error)
	Applied     func()
}

func (a UsageAcceptance) Seal(m platform.Member, u platform.AIUsage) (acceptedformat.Usage, []byte, *kernel.Error) {
	unknown := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	if u.Call != "" {
		for _, prior := range a.Prior() {
			if prior.Call != u.Call {
				continue
			}
			x, _ := platform.CanonicalDigest(prior)
			y, _ := platform.CanonicalDigest(u)
			if x != y {
				return acceptedformat.Usage{}, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The model usage differs from its accepted reading")
			}
			return acceptedformat.Usage{}, nil, nil
		}
	}
	c := a.Caller(platform.Member{ID: "app:" + platform.AIApp, Tenant: m.Tenant})
	decision, refusal := a.Authorize(c, m.Tenant == a.Tenant && m.ID != "" && m.ID == u.Member && acceptedformat.ValidUsage(u))
	if refusal != nil {
		return acceptedformat.Usage{}, nil, refusal
	}
	cannot := platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	before, err := a.Predecessor()
	if err != nil {
		return acceptedformat.Usage{}, nil, cannot
	}
	r := acceptedformat.Usage{Version: 1, Kind: "usage-result", Tenant: a.Tenant, App: platform.AIApp, Member: m.ID, At: u.At.UTC(), Before: before, Usage: u}
	r.Authorizations = []acceptedformat.AuthorizationEvidence{{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Action: "usage:observe"}, Decision: decision, Roles: slices.Clone(c.RolesHere())}}
	if r.RequestHash, err = acceptedformat.UsageHash(before, u); err != nil {
		return acceptedformat.Usage{}, nil, unknown
	}
	r.Key = "usage:" + r.RequestHash
	if r.Digest, err = acceptedformat.DigestUsage(r); err != nil {
		return acceptedformat.Usage{}, nil, unknown
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return acceptedformat.Usage{}, nil, unknown
	}
	if _, err := acceptedformat.DecodeUsage(raw); err != nil {
		return acceptedformat.Usage{}, nil, cannot
	}
	return r, raw, nil
}

func (a UsageAcceptance) Finish(committed []byte, key, hash string) *kernel.Error {
	unknown := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	saved, err := acceptedformat.DecodeUsage(committed)
	if err != nil || saved.Key != key || saved.RequestHash != hash {
		a.Quarantine(fmt.Errorf("committed model usage differs: %v", err))
		return unknown
	}
	if err := a.Apply(committed); err != nil {
		a.Quarantine(err)
		return unknown
	}
	a.Applied()
	return nil
}
