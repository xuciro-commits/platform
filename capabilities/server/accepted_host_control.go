package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// The host console owns lifecycle, bounded support authority and migration
// manifests. These images contain no ordinary tenant records or execution state.
type hostControlImage struct {
	Lifecycle  string                        `json:"lifecycle"`
	Support    []SupportGrant                `json:"support"`
	Migrations []MigrationManifest           `json:"migrations"`
	Requests   map[string]hostControlReceipt `json:"requests"`
}
type hostControlReceipt struct {
	RequestHash string       `json:"requestHash"`
	Answer      SupportGrant `json:"answer,omitempty"`
}
type acceptedHostControl struct {
	Authorizations []authorizationEvidence `json:"authorizations,omitempty"`
	Version        int                     `json:"version"`
	Kind           string                  `json:"kind"`
	Tenant         string                  `json:"tenant"`
	App            string                  `json:"app"`
	Member         string                  `json:"member"`
	Key            string                  `json:"key"`
	At             time.Time               `json:"at"`
	Action         string                  `json:"action"`
	Request        json.RawMessage         `json:"request"`
	RequestHash    string                  `json:"requestHash"`
	Digest         string                  `json:"digest"`
	Before         string                  `json:"before"`
	AuditBefore    string                  `json:"auditBefore"`
	Image          hostControlImage        `json:"image"`
	Audit          AuditEntry              `json:"audit"`
	Answer         SupportGrant            `json:"answer,omitempty"`
}

func (h *hostControl) image() (hostControlImage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Encode the complete owned shape to isolate nested migration row diagnostics.
	raw, err := json.Marshal(hostControlImage{h.state(), h.support, h.migrations, h.requests})
	var image hostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &image)
	}
	return image, err
}
func validateHostImage(tenant string, image hostControlImage) error {
	if !slices.Contains([]string{"", "open", "suspend", "decommission", "suspended", "decommissioned"}, image.Lifecycle) {
		return fmt.Errorf("invalid host lifecycle")
	}
	ids := map[string]bool{}
	for _, grant := range image.Support {
		if grant.ID == "" || ids[grant.ID] || grant.Tenant != tenant || grant.Member == "" || grant.Reason == "" || grant.Opened == "" || grant.Expires.IsZero() || grant.Uses < 0 {
			return fmt.Errorf("invalid host support grant")
		}
		ids[grant.ID] = true
	}
	for _, m := range image.Migrations {
		if m.At.IsZero() || m.Key == "" || m.From == "" || m.To != tenant || m.From == m.To || m.Member == "" {
			return fmt.Errorf("invalid host migration manifest")
		}
		for _, part := range m.Types {
			if part.Type == "" || part.Rows < 0 || part.Written < 0 || part.Written > part.Rows {
				return fmt.Errorf("invalid host migration counts")
			}
		}
	}
	for key, receipt := range image.Requests {
		tag, _, found := strings.Cut(strings.TrimPrefix(key, "host:"), ":")
		if !strings.HasPrefix(key, "host:") || !found || !slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration"}, tag) || len(receipt.RequestHash) != 64 || !strings.HasSuffix(key, ":"+receipt.RequestHash) {
			return fmt.Errorf("invalid host receipt")
		}
		if tag == "support.open" || tag == "support.use" {
			i := slices.IndexFunc(image.Support, func(g SupportGrant) bool { return g.ID == receipt.Answer.ID })
			if i < 0 {
				return fmt.Errorf("host receipt names no support grant")
			}
			current := image.Support[i]
			if receipt.Answer.Uses < 0 || receipt.Answer.Uses > current.Uses {
				return fmt.Errorf("host receipt has an invalid support count")
			}
			current.Uses = receipt.Answer.Uses
			if current != receipt.Answer {
				return fmt.Errorf("host receipt rewrites its support authority")
			}
		} else if receipt.Answer != (SupportGrant{}) {
			return fmt.Errorf("host receipt has an unrelated answer")
		}
	}
	return nil
}
func hostRequestHash(r acceptedHostControl) (string, error) {
	return canonicalDigest([]any{r.Tenant, r.Member, r.Action, r.Request, r.At})
}
func hostResultDigest(r acceptedHostControl) (string, error) {
	r.Digest = ""
	return canonicalDigest(r)
}
func decodeAcceptedHostControl(raw []byte) (acceptedHostControl, error) {
	var r acceptedHostControl
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return r, fmt.Errorf("host result is empty or too large")
	}
	if err := decodeAcceptedJSON(raw, &r, "host result"); err != nil {
		return r, err
	}
	if err := validateAuthorizationEvidence(r.Tenant, r.Authorizations); err != nil {
		return r, err
	}
	hash, hashErr := hostRequestHash(r)
	digest, digestErr := hostResultDigest(r)
	if hashErr != nil || digestErr != nil || r.Version != 1 || r.Kind != "host-result" || r.Tenant == "" || r.App != PlatformApp || r.Member == "" || r.At.IsZero() ||
		!slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration"}, r.Action) || r.Key != "host:"+r.Action+":"+hash || r.RequestHash != hash || r.Digest != digest || r.Before == "" || r.AuditBefore == "" ||
		r.Audit.Member != r.Member || r.Audit.App != PlatformApp || !r.Audit.At.Equal(r.At) || r.Audit.Action == "" {
		return r, fmt.Errorf("host result identity differs")
	}
	if err := validateHostImage(r.Tenant, r.Image); err != nil {
		return r, err
	}
	receipt, ok := r.Image.Requests[r.Key]
	if !ok || receipt.RequestHash != hash || receipt.Answer != r.Answer {
		return r, fmt.Errorf("host result has no matching receipt")
	}
	return r, nil
}

// acceptHostControl holds Tenant.mu from private image capture through append
// and application. The accepted clock is part of identity because these APIs
// predate caller supplied keys; repeating that same request returns its receipt.
func (t *Tenant) acceptHostControl(action, member string, request any, now time.Time, audit AuditEntry, decide func(*hostControlImage) (SupportGrant, error), evidence ...authorizationEvidence) (SupportGrant, error) {
	r := acceptedHostControl{Version: 1, Kind: "host-result", Tenant: t.ID, App: PlatformApp, Member: member, Action: action, At: now.UTC(), Request: platform.Raw(request)}
	r.Authorizations = slices.Clone(evidence)
	var err error
	r.RequestHash, err = hostRequestHash(r)
	if err != nil {
		return SupportGrant{}, err
	}
	r.Key = "host:" + action + ":" + r.RequestHash
	image, err := t.console.image()
	if err != nil {
		return SupportGrant{}, err
	}
	if prior, ok := image.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash {
			return SupportGrant{}, fmt.Errorf("host request key changed")
		}
		return prior.Answer, nil
	}
	r.Before, err = canonicalDigest(image)
	if err != nil {
		return SupportGrant{}, err
	}
	r.AuditBefore, err = canonicalDigest(t.audit.all())
	if err != nil {
		return SupportGrant{}, err
	}
	r.Answer, err = decide(&image)
	if err != nil {
		return SupportGrant{}, err
	}
	if image.Requests == nil {
		image.Requests = map[string]hostControlReceipt{}
	}
	image.Requests[r.Key] = hostControlReceipt{r.RequestHash, r.Answer}
	r.Image = image
	audit.At, audit.Member, audit.App = r.At, member, PlatformApp
	r.Audit = audit
	r.Digest, err = hostResultDigest(r)
	if err != nil {
		return SupportGrant{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return SupportGrant{}, err
	}
	if _, err = decodeAcceptedHostControl(raw); err != nil {
		return SupportGrant{}, err
	}
	principal, _ := json.Marshal(platform.Member{ID: member, Tenant: t.ID})
	committed, err := t.acceptResult(Entry{App: PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return SupportGrant{}, err
	}
	saved, err := decodeAcceptedHostControl(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed host result differs: %v", err))
		return SupportGrant{}, fmt.Errorf("committed host result differs: %v", err)
	}
	if _, err = t.applyAcceptedHostControl(committed); err != nil {
		t.quarantine(err)
		return SupportGrant{}, err
	}
	t.changed()
	return saved.Answer, nil
}
func (t *Tenant) applyAcceptedHostControl(raw []byte) (acceptedHostControl, error) {
	r, err := decodeAcceptedHostControl(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != t.ID {
		return r, fmt.Errorf("host result belongs to another tenant")
	}
	current, err := t.console.image()
	if err != nil {
		return r, err
	}
	if prior, ok := current.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash || prior.Answer != r.Answer {
			return r, fmt.Errorf("host result receipt differs")
		}
		return r, nil
	}
	before, err := canonicalDigest(current)
	auditBefore, auditErr := canonicalDigest(t.audit.all())
	if err != nil || auditErr != nil || before != r.Before || auditBefore != r.AuditBefore {
		return r, fmt.Errorf("host result predecessor differs")
	}
	if err := validateHostTransition(current, r); err != nil {
		return r, err
	}
	t.console.mu.Lock()
	t.console.setState(r.Image.Lifecycle)
	t.console.support, t.console.migrations, t.console.requests = r.Image.Support, r.Image.Migrations, maps.Clone(r.Image.Requests)
	t.console.mu.Unlock()
	t.audit.remember(r.Audit)
	return r, nil
}

// A saved image may only perform the one requested host-owned transition. This
// checks frozen bytes and predecessors; it does not rerun current authorization.
func validateHostTransition(current hostControlImage, r acceptedHostControl) error {
	raw, err := json.Marshal(current)
	var expected hostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &expected)
	}
	if err != nil {
		return err
	}
	var answer SupportGrant
	audit := AuditEntry{At: r.At, Member: r.Member, App: PlatformApp}
	switch r.Action {
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
		i := slices.IndexFunc(expected.Support, func(g SupportGrant) bool { return g.ID == body.ID })
		if i < 0 || !expected.Support[i].Expires.After(r.At) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host support use predecessor differs")
		}
		expected.Support[i].Uses++
		answer = expected.Support[i]
		audit.Action, audit.Target = "host.support.use", body.ID
	case "migration":
		var body MigrationResult
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		expected.Migrations = append(expected.Migrations, MigrationManifest{At: r.At, From: body.From, To: body.To, Key: body.Key, Member: r.Member, Types: body.Types})
		audit.Action, audit.Target = "host.migration", body.Key
	}
	if expected.Requests == nil {
		expected.Requests = map[string]hostControlReceipt{}
	}
	expected.Requests[r.Key] = hostControlReceipt{r.RequestHash, answer}
	want, err := canonicalDigest(expected)
	got, gotErr := canonicalDigest(r.Image)
	wantAudit, auditErr := canonicalDigest(audit)
	gotAudit, gotAuditErr := canonicalDigest(r.Audit)
	if err != nil || gotErr != nil || auditErr != nil || gotAuditErr != nil || want != got || answer != r.Answer || wantAudit != gotAudit {
		return fmt.Errorf("host result changes unrelated state or audit")
	}
	return nil
}
