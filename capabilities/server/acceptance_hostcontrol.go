package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (t *Tenant) acceptHostControl(action, member string, request any, now time.Time, audit platform.AuditEntry, decide func(*platform.HostControlImage) (platform.SupportGrant, error), evidence ...acceptedformat.AuthorizationEvidence) (platform.SupportGrant, error) {
	r := acceptedformat.HostControl{Version: 1, Kind: "host-result", Tenant: t.ID, App: controlowner.PlatformApp, Member: member, Action: action, At: now.UTC(), Request: platform.Raw(request)}
	r.Authorizations = slices.Clone(evidence)
	var err error
	r.RequestHash, err = acceptedformat.HostRequestHash(r)
	if err != nil {
		return platform.SupportGrant{}, err
	}
	r.Key = "host:" + action + ":" + r.RequestHash
	image, err := t.console.Image()
	if err != nil {
		return platform.SupportGrant{}, err
	}
	if prior, ok := image.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash {
			return platform.SupportGrant{}, fmt.Errorf("host request key changed")
		}
		return prior.Answer, nil
	}
	r.Before, err = platform.CanonicalDigest(image)
	if err != nil {
		return platform.SupportGrant{}, err
	}
	r.AuditBefore, err = platform.CanonicalDigest(t.audit.All())
	if err != nil {
		return platform.SupportGrant{}, err
	}
	r.Answer, err = decide(&image)
	if err != nil {
		return platform.SupportGrant{}, err
	}
	if image.Requests == nil {
		image.Requests = map[string]platform.HostControlReceipt{}
	}
	image.Requests[r.Key] = platform.HostControlReceipt{RequestHash: r.RequestHash, Answer: r.Answer}
	r.Image = image
	audit.At, audit.Member, audit.App = r.At, member, controlowner.PlatformApp
	r.Audit = audit
	r.Digest, err = acceptedformat.HostResultDigest(r)
	if err != nil {
		return platform.SupportGrant{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return platform.SupportGrant{}, err
	}
	if _, err = acceptedformat.DecodeHostControl(raw); err != nil {
		return platform.SupportGrant{}, err
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
	r, err := acceptedformat.DecodeHostControl(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != t.ID {
		return r, fmt.Errorf("host result belongs to another tenant")
	}
	current, err := t.console.Image()
	if err != nil {
		return r, err
	}
	if prior, ok := current.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash || prior.Answer != r.Answer {
			return r, fmt.Errorf("host result receipt differs")
		}
		return r, nil
	}
	before, err := platform.CanonicalDigest(current)
	auditBefore, auditErr := platform.CanonicalDigest(t.audit.All())
	if err != nil || auditErr != nil || before != r.Before || auditBefore != r.AuditBefore {
		return r, fmt.Errorf("host result predecessor differs")
	}
	if err := validateHostTransition(current, r); err != nil {
		return r, err
	}
	t.console.Restore(r.Image)
	t.audit.Remember(r.Audit)
	return r, nil
}

func validateHostTransition(current platform.HostControlImage, r acceptedformat.HostControl) error {
	raw, err := json.Marshal(current)
	var expected platform.HostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &expected)
	}
	if err != nil {
		return err
	}
	var answer platform.SupportGrant
	audit := platform.AuditEntry{At: r.At, Member: r.Member, App: controlowner.PlatformApp}
	switch r.Action {
	case "composition":
		var body CompositionRequest
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if body.Reason == "" || body.At.IsZero() || !body.At.Equal(r.At) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("invalid runtime maintenance request")
		}
		expected.RuntimeSelection = maps.Clone(body.Selection)
		expected.DisabledOwners = slices.Clone(body.DisabledOwners)
		expected.Lifecycle = "suspend"
		audit.Action, audit.Target = "host.composition", r.Tenant+":"+body.Reason
	case "lifecycle":
		var body struct{ Action, Reason string }
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if !slices.Contains([]string{"suspend", "decommission", "resume", "open"}, body.Action) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host lifecycle request differs")
		}
		expected.Lifecycle = ""
		if body.Action == "suspend" || body.Action == "decommission" {
			expected.Lifecycle = body.Action
		}
		audit.Action, audit.Target = "host.lifecycle."+body.Action, r.Tenant+":"+body.Reason
	case "support.open":
		var body struct {
			Member, Reason string
			Minutes        int
		}
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if body.Member == "" || body.Reason == "" || body.Minutes < 1 || body.Minutes > 1440 || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host support request differs")
		}
		answer = r.Answer
		if answer.Tenant != r.Tenant || answer.Member != body.Member || answer.Reason != body.Reason || answer.Opened != strings.TrimPrefix(r.Member, "host:") || answer.Uses != 0 || !answer.Expires.Equal(r.At.Add(time.Duration(body.Minutes)*time.Minute)) {
			return fmt.Errorf("host support grant differs from its accepted request")
		}
		expected.Support = append(expected.Support, answer)
		audit.Action, audit.Target = "host.support.open", body.Member+":"+body.Reason
	case "support.use":
		var body struct{ ID string }
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		i := slices.IndexFunc(expected.Support, func(g platform.SupportGrant) bool { return g.ID == body.ID })
		if i < 0 || !expected.Support[i].Expires.After(r.At) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host support use predecessor differs")
		}
		expected.Support[i].Uses++
		answer = expected.Support[i]
		audit.Action, audit.Target = "host.support.use", body.ID
	case "migration":
		var body platform.MigrationResult
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		expected.Migrations = append(expected.Migrations, platform.MigrationManifest{At: r.At, From: body.From, To: body.To, Key: body.Key, Member: r.Member, Types: body.Types})
		audit.Action, audit.Target = "host.migration", body.Key
	}
	if expected.Requests == nil {
		expected.Requests = map[string]platform.HostControlReceipt{}
	}
	expected.Requests[r.Key] = platform.HostControlReceipt{RequestHash: r.RequestHash, Answer: answer}
	want, err := platform.CanonicalDigest(expected)
	got, gotErr := platform.CanonicalDigest(r.Image)
	wantAudit, auditErr := platform.CanonicalDigest(audit)
	gotAudit, gotAuditErr := platform.CanonicalDigest(r.Audit)
	if err != nil || gotErr != nil || auditErr != nil || gotAuditErr != nil || want != got || answer != r.Answer || wantAudit != gotAudit {
		return fmt.Errorf("host result changes unrelated state or audit")
	}
	return nil
}
