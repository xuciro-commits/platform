package control

import (
	"platformserver/platform"
	"strconv"
	"time"
)

type HostTenantView struct {
	ID            string    `json:"id"`
	Quarantined   bool      `json:"quarantined,omitempty"`
	Fault         string    `json:"fault,omitempty"`
	Lifecycle     string    `json:"lifecycle,omitempty"`
	Apps          []string  `json:"apps"`
	Members       int       `json:"members"`
	Audit         int       `json:"audit"`
	ActiveRelease string    `json:"activeRelease,omitempty"`
	Candidates    int       `json:"candidates"`
	Support       int       `json:"support"`
	FailedWork    int       `json:"failedWork"`
	Connectors    int       `json:"connectors"`
	Started       time.Time `json:"started,omitempty"`
}

type HostOverview struct {
	Now     time.Time        `json:"now"`
	Tenants []HostTenantView `json:"tenants"`
	Admins  int              `json:"admins"`
}

type LifecycleRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

func (r LifecycleRequest) Prepare(tenant, subject string) (HostAction, error) {
	if err := ValidLifecycleAction(r.Action); err != nil {
		return HostAction{}, err
	}
	return LifecycleAction(tenant, r.Action, r.Reason), nil
}

type SupportAnswer struct {
	Read   string `json:"read"`
	Answer any    `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

type SupportOpenRequest struct {
	Member  string `json:"member"`
	Reason  string `json:"reason"`
	Minutes int    `json:"minutes"`
}

func (r SupportOpenRequest) Prepare(tenant, subject string, now time.Time) (HostAction, string, error) {
	if err := ValidSupportSession(r.Member, r.Reason, subject, r.Minutes, now); err != nil {
		return HostAction{}, "", err
	}
	action := SupportOpenAction(r)
	id, err := SupportGrantID(tenant, subject, action, now)
	return action, id, err
}

type MigrationRequest struct {
	From        string   `json:"from"`
	Types       []string `json:"types"`
	SourceGrant string   `json:"sourceGrant"`
	TargetGrant string   `json:"targetGrant"`
}

func (r MigrationRequest) Complete() bool { return r.SourceGrant != "" && r.TargetGrant != "" }

func ActiveSupport(grants []platform.SupportGrant, now time.Time) int {
	n := 0
	for _, g := range grants {
		if g.Expires.After(now) {
			n++
		}
	}
	return n
}

type HostAction struct {
	Name    string
	Request any
	Audit   platform.AuditEntry
}

func LifecycleAction(tenant, action, reason string) HostAction {
	return HostAction{"lifecycle", struct{ Action, Reason string }{action, reason}, platform.AuditEntry{Action: "host.lifecycle." + action, Target: tenant + ":" + reason}}
}

func SupportOpenAction(r SupportOpenRequest) HostAction {
	return HostAction{"support.open", struct {
		Member, Reason string
		Minutes        int
	}{r.Member, r.Reason, r.Minutes}, platform.AuditEntry{Action: "host.support.open", Target: r.Member + ":" + r.Reason}}
}

func SupportUseAction(id string) HostAction {
	return HostAction{"support.use", struct{ ID string }{id}, platform.AuditEntry{Action: "host.support.use", Target: id}}
}

func MigrationAction(result platform.MigrationResult) HostAction {
	return HostAction{"migration", result, platform.AuditEntry{Action: "host.migration", Target: result.Key}}
}

func SupportGrantID(tenant, subject string, action HostAction, now time.Time) (string, error) {
	hash, err := platform.CanonicalDigest([]any{tenant, subject, action.Request, now.UTC()})
	if err != nil {
		return "", err
	}
	return "support:" + strconv.FormatInt(now.UTC().UnixNano(), 36) + ":" + hash[:12], nil
}

func RecordMigration(image *platform.HostControlImage, result platform.MigrationResult, member string, now time.Time) {
	image.Migrations = append(image.Migrations, platform.MigrationManifest{At: now.UTC(), From: result.From, To: result.To, Key: result.Key, Member: member, Types: result.Types})
}
