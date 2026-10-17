package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// A direct model call has no owned effect. Its immutable usage observation is
// accepted before changing the meter, using the same predecessor as effects.
type acceptedUsage struct {
	Authorizations []authorizationEvidence `json:"authorizations,omitempty"`
	Version        int                     `json:"version"`
	Kind           string                  `json:"kind"`
	Tenant         string                  `json:"tenant"`
	App            string                  `json:"app"`
	Member         string                  `json:"member"`
	Key            string                  `json:"key"`
	At             time.Time               `json:"at"`
	RequestHash    string                  `json:"requestHash"`
	Digest         string                  `json:"digest"`
	Before         string                  `json:"before"`
	Usage          platform.AIUsage        `json:"usage"`
}

func (t *Tenant) usagePredecessor() (string, error) {
	if t.ai == nil {
		return "", fmt.Errorf("usage needs an AI meter")
	}
	raw, err := t.ai.Snapshot()
	if err != nil {
		return "", err
	}
	return canonicalDigest(raw)
}

func validUsage(u platform.AIUsage) bool {
	return u.Member != "" && u.Model != "" && !u.At.IsZero() && u.Input >= 0 && u.Output >= 0 && u.Cost >= 0 && u.Millis >= 0
}

func usageHash(before string, u platform.AIUsage) (string, error) {
	return canonicalDigest([]any{before, u})
}
func digestUsage(r acceptedUsage) (string, error) { r.Digest = ""; return canonicalDigest(r) }
func decodeAcceptedUsage(raw []byte) (acceptedUsage, error) {
	var r acceptedUsage
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return r, fmt.Errorf("usage result is empty or too large")
	}
	if err := decodeAcceptedJSON(raw, &r, "usage result"); err != nil {
		return r, err
	}
	if err := validateAuthorizationEvidence(r.Tenant, r.Authorizations); err != nil {
		return r, err
	}

	hash, err := usageHash(r.Before, r.Usage)
	digest, digestErr := digestUsage(r)
	if err != nil || digestErr != nil || r.Version != 1 || r.Kind != "usage-result" || r.Tenant == "" || r.App != platform.AIApp || r.Before == "" ||
		r.Member != r.Usage.Member || !validUsage(r.Usage) || !r.At.Equal(r.Usage.At) || r.RequestHash != hash || r.Key != "usage:"+hash || r.Digest != digest {
		return r, fmt.Errorf("usage result has inconsistent identity, reading or digest")
	}
	return r, nil
}

func (t *Tenant) acceptUsage(m platform.Member, u platform.AIUsage) *kernel.Error {
	// Usage observes a completed call. Its principal is the meter owner; a
	// later revocation of the attributed caller cannot erase an incurred cost.
	c := platform.NewCaller(runtime{t}, platform.Member{ID: "app:" + platform.AIApp, Tenant: m.Tenant}, platform.AIApp, false, true)
	decision := t.authorizeNative(c, "usage:observe", "derive", nil, m.ID != "" && m.ID == u.Member && validUsage(u))
	if refusal := authorizationRefusal(decision); refusal != nil {
		return refusal
	}
	before, err := t.usagePredecessor()
	if err != nil || m.Tenant != t.ID || m.ID != u.Member || !validUsage(u) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	r := acceptedUsage{Version: 1, Kind: "usage-result", Tenant: t.ID, App: platform.AIApp, Member: m.ID, At: u.At.UTC(), Before: before, Usage: u}
	r.Authorizations = []authorizationEvidence{{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Action: "usage:observe"}, Decision: decision, Roles: slices.Clone(c.RolesHere())}}
	r.RequestHash, err = usageHash(before, u)
	if err != nil {
		return unknown()
	}
	r.Key = "usage:" + r.RequestHash
	r.Digest, err = digestUsage(r)
	if err != nil {
		return unknown()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return unknown()
	}
	if _, err := decodeAcceptedUsage(raw); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	principal, _ := json.Marshal(m)
	committed, err := t.acceptResult(Entry{App: platform.AIApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage was not accepted")
	}
	saved, err := decodeAcceptedUsage(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed model usage differs: %v", err))
		return unknown()
	}
	if _, err := t.applyAcceptedUsage(committed); err != nil {
		t.quarantine(err)
		return unknown()
	}
	t.operationsChanged(platform.AIApp)
	return nil
}

func (t *Tenant) applyAcceptedUsage(raw []byte) (acceptedUsage, error) {
	r, err := decodeAcceptedUsage(raw)
	if err != nil {
		return r, err
	}
	before, err := t.usagePredecessor()
	if err != nil || r.Tenant != t.ID || before != r.Before {
		return r, fmt.Errorf("model usage predecessor differs: %v", err)
	}
	t.ai.Meter(r.Usage)
	return r, nil
}
