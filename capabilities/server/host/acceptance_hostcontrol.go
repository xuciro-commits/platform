package host

import (
	"encoding/json"
	"fmt"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
	"time"
)

func (t *Tenant) acceptHostControl(action, member string, request any, now time.Time, audit platform.AuditEntry, decide func(*platform.HostControlImage) (platform.SupportGrant, error), evidence ...acceptedformat.AuthorizationEvidence) (platform.SupportGrant, error) {
	image, err := t.console.Image()
	if err != nil {
		return platform.SupportGrant{}, err
	}
	r, raw, prior, err := runtimekernel.PrepareHostControl(runtimekernel.HostRequest{Tenant: t.ID, Action: action, Member: member, Request: request, At: now, Audit: audit, Evidence: evidence}, image, t.audit.All(), decide)
	if err != nil {
		return platform.SupportGrant{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	principal, _ := json.Marshal(platform.Member{ID: member, Tenant: t.ID})
	committed, err := t.acceptResult(Entry{App: controlowner.PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return platform.SupportGrant{}, err
	}
	saved, err := acceptedformat.DecodeHostControl(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed host result differs: %v", err))
		return platform.SupportGrant{}, fmt.Errorf("committed host result differs: %v", err)
	}
	if _, err = t.applyAcceptedHostControl(committed); err != nil {
		t.quarantine(err)
		return platform.SupportGrant{}, err
	}
	t.acceptedResultApplied()
	t.changed()
	return saved.Answer, nil
}

func (t *Tenant) applyAcceptedHostControl(raw []byte) (acceptedformat.HostControl, error) {
	current, err := t.console.Image()
	if err != nil {
		return acceptedformat.HostControl{}, err
	}
	r, fresh, err := runtimekernel.CheckHostControl(raw, t.ID, current, t.audit.All())
	if err != nil || !fresh {
		return r, err
	}
	t.console.Restore(r.Image)
	t.audit.Remember(r.Audit)
	return r, nil
}
