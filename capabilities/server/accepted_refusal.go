package platformserver

import (
	"encoding/json"
	resultformat "platformserver/internal/acceptance/result"
)

func resultRequestHash(raw []byte) (string, error) {
	identity, err := acceptedIdentity(raw)
	return identity.Hash, err
}

func acceptedIdentity(raw []byte) (resultIdentity, error) {
	var identity resultIdentity
	var envelope struct{ Kind string }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return identity, err
	}
	identity.Scope = "submission"
	if envelope.Kind == "host-result" {
		r, e := decodeAcceptedHostControl(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "host"
		return identity, e
	}
	if envelope.Kind == "composite-result" {
		r, e := decodeAcceptedComposite(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "composite"
		return identity, e
	}
	if envelope.Kind == "usage-result" {
		r, e := resultformat.DecodeUsage(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "usage"
		return identity, e
	}
	if envelope.Kind == "agent-result" {
		r, e := decodeAcceptedAgent(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "agent"
		return identity, e
	}
	if envelope.Kind == "operation-claim" {
		result, err := decodeOperationClaim(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	if envelope.Kind == "refusal" {
		result, sub, err := resultformat.DecodeRefusal(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "record-batch" {
		result, receipt, err := resultformat.DecodeBatch(raw)
		if err != nil {
			return identity, err
		}
		sub, err := resultformat.BatchSubmission(result, receipt)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "work-result" {
		result, err := decodeAcceptedWork(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "work"
		return identity, err
	}
	if envelope.Kind == "input-result" {
		result, err := resultformat.DecodeInput(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "input"
		return identity, err
	}
	if envelope.Kind == "effect-result" {
		result, err := resultformat.DecodeEffect(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, PlatformApp, result.Key, result.RequestHash
		identity.Scope = "effect"
		return identity, err
	}
	if envelope.Kind == "release-result" {
		result, err := resultformat.DecodeRelease(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	result, receipt, err := resultformat.DecodeRecord(raw)
	identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, receipt.GetSubmission().GetIdempotencyKey(), result.RequestHash
	return identity, err
}
