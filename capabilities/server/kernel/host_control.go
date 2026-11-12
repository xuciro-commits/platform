package kernel

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

type HostRequest struct {
	Tenant, Action, Member string
	Request                any
	At                     time.Time
	Audit                  platform.AuditEntry
	Evidence               []acceptedformat.AuthorizationEvidence
}

func PrepareHostControl(q HostRequest, image platform.HostControlImage, audits []platform.AuditEntry, decide func(*platform.HostControlImage) (platform.SupportGrant, error)) (acceptedformat.HostControl, []byte, *platform.SupportGrant, error) {
	r := acceptedformat.HostControl{Version: 1, Kind: "host-result", Tenant: q.Tenant, App: acceptedformat.EffectApp, Member: q.Member, Action: q.Action, At: q.At.UTC(), Request: platform.Raw(q.Request)}
	r.Authorizations = slices.Clone(q.Evidence)
	var err error
	r.RequestHash, err = acceptedformat.HostRequestHash(r)
	if err != nil {
		return r, nil, nil, err
	}
	r.Key = "host:" + q.Action + ":" + r.RequestHash
	if prior, ok := image.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash {
			return r, nil, nil, fmt.Errorf("host request key changed")
		}
		return r, nil, &prior.Answer, nil
	}
	if r.Before, err = platform.CanonicalDigest(image); err != nil {
		return r, nil, nil, err
	}
	if r.AuditBefore, err = platform.CanonicalDigest(audits); err != nil {
		return r, nil, nil, err
	}
	if r.Answer, err = decide(&image); err != nil {
		return r, nil, nil, err
	}
	if image.Requests == nil {
		image.Requests = map[string]platform.HostControlReceipt{}
	}
	image.Requests[r.Key] = platform.HostControlReceipt{RequestHash: r.RequestHash, Answer: r.Answer}
	r.Image = image
	audit := q.Audit
	audit.At, audit.Member, audit.App = r.At, q.Member, acceptedformat.EffectApp
	r.Audit = audit
	if r.Digest, err = acceptedformat.HostResultDigest(r); err != nil {
		return r, nil, nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return r, nil, nil, err
	}
	if _, err = acceptedformat.DecodeHostControl(raw); err != nil {
		return r, nil, nil, err
	}
	return r, raw, nil, nil
}

func CheckHostControl(raw []byte, tenant string, current platform.HostControlImage, audits []platform.AuditEntry) (acceptedformat.HostControl, bool, error) {
	r, err := acceptedformat.DecodeHostControl(raw)
	if err != nil {
		return r, false, err
	}
	if r.Tenant != tenant {
		return r, false, fmt.Errorf("host result belongs to another tenant")
	}
	if prior, ok := current.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash || prior.Answer != r.Answer {
			return r, false, fmt.Errorf("host result receipt differs")
		}
		return r, false, nil
	}
	before, err := platform.CanonicalDigest(current)
	auditBefore, auditErr := platform.CanonicalDigest(audits)
	if err != nil || auditErr != nil || before != r.Before || auditBefore != r.AuditBefore {
		return r, false, fmt.Errorf("host result predecessor differs")
	}
	return r, true, hostTransition(current, r)
}

func hostTransition(current platform.HostControlImage, r acceptedformat.HostControl) error {
	raw, err := json.Marshal(current)
	var expected platform.HostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &expected)
	}
	if err != nil {
		return err
	}
	var answer platform.SupportGrant
	audit := platform.AuditEntry{At: r.At, Member: r.Member, App: acceptedformat.EffectApp}
	switch r.Action {
	case "composition":
		var body acceptedformat.CompositionRequest
		if err := acceptedformat.DecodeControl(r.Request, &body); err != nil {
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
		if err := acceptedformat.DecodeControl(r.Request, &body); err != nil {
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
		if err := acceptedformat.DecodeControl(r.Request, &body); err != nil {
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
		if err := acceptedformat.DecodeControl(r.Request, &body); err != nil {
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
		if err := acceptedformat.DecodeControl(r.Request, &body); err != nil {
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

type HostControlOwner struct {
	Image   func() (platform.HostControlImage, error)
	Restore func(platform.HostControlImage)
	Audit   *AuditLog
}

func (s *Sequence) ApplyHostControl(raw []byte, o HostControlOwner) (acceptedformat.HostControl, error) {
	current, err := o.Image()
	if err != nil {
		return acceptedformat.HostControl{}, err
	}
	r, fresh, err := CheckHostControl(raw, s.ID, current, o.Audit.All())
	if err != nil || !fresh {
		return r, err
	}
	o.Restore(r.Image)
	o.Audit.Remember(r.Audit)
	return r, nil
}

func (s *Sequence) AcceptHostControl(q HostRequest, o HostControlOwner, decide func(*platform.HostControlImage) (platform.SupportGrant, error), commit func(persistence.Entry, string, string) ([]byte, error), quarantine func(error)) (platform.SupportGrant, bool, error) {
	image, err := o.Image()
	if err != nil {
		return platform.SupportGrant{}, false, err
	}
	r, raw, prior, err := PrepareHostControl(q, image, o.Audit.All(), decide)
	if err != nil {
		return platform.SupportGrant{}, false, err
	}
	if prior != nil {
		return *prior, false, nil
	}
	principal, _ := json.Marshal(platform.Member{ID: q.Member, Tenant: s.ID})
	committed, err := commit(persistence.Entry{App: acceptedformat.EffectApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return platform.SupportGrant{}, false, err
	}
	saved, err := acceptedformat.DecodeHostControl(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		err = fmt.Errorf("committed host result differs: %v", err)
		quarantine(err)
		return platform.SupportGrant{}, false, err
	}
	if _, err = s.ApplyHostControl(committed, o); err != nil {
		quarantine(err)
		return platform.SupportGrant{}, false, err
	}
	return saved.Answer, true, nil
}
