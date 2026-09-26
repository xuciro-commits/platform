package mes

import (
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"production"
)

// languages translate the app's titles and descriptions (ADR-0023).
//
//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

// Settings of the plant (ADR-0013), set by administrators in Settings.
const (
	SettingNotifyDowntime = "notify-downtime"
	SettingReasonMinutes  = "reason-reminder-minutes"
	JobReasons            = "downtime-reasons"
)

// Snapshot and Restore: the plant's facts (gateway batches), the
// identities and redirects of downtime events, the derived downtime and the
// decisions; orders and SFCs are the host's records (ADR-0019 D6).
type plantState struct {
	Facts     json.RawMessage       `json:"facts"`
	Identity  kernel.IdentityState  `json:"identity"`
	Downtime  map[string][]Downtime `json:"downtime"`
	NextEvent int                   `json:"nextEvent"`
}

func (p *Plant) Snapshot() (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	facts, err := platform.SnapshotFacts(p.facts, p.tenant)
	if err != nil {
		return nil, err
	}
	return p.ledger.SnapshotWith(plantState{Facts: facts, Identity: p.identity.State(), Downtime: p.downtime, NextEvent: p.nextEvent})
}

func (p *Plant) Restore(raw json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var s plantState
	if err := p.ledger.RestoreWith(raw, &s); err != nil {
		return err
	}
	p.identity.Restore(s.Identity)
	p.downtime, p.nextEvent = s.Downtime, s.NextEvent
	if p.downtime == nil {
		p.downtime = map[string][]Downtime{}
	}
	return platform.RestoreFacts(p.facts, p.tenant, s.Facts)
}

// Manifest declares the plant as the "mes" app (ADR-0010): its actions, its
// reads, its connector input (batches, journaled; the host keeps the
// connectors), its settings, its scheduled job (ADR-0013), its flow (ADR-0020),
// and the protocol it reaches an ERP through (ADR-0024).
func (p *Plant) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: "mes", Title: "Plant operations", Version: "1", Actions: p.ledger.Catalog, Flows: []platform.Flow{p.confirmation()}, Agents: []platform.Agent{p.fixer(), p.planner()},
		Reads: []string{"master", "planned-orders", "downtime"}, Entities: p.entities,
		Consumes: []platform.Consumption{{Protocol: production.ID, Optional: true}},
		Inputs:   map[string]bool{"states": true},
		Jobs:     []platform.Job{{Name: JobReasons, Title: "Remind supervisors of downtime without a reason", Every: 5 * time.Minute}},
		Emits: []platform.EffectKind{{Name: EffectLeadTime, Title: "Lead time from a supplier",
			Description: "A question to a supplier's agent about how soon it can deliver a product; bind it to the supplier's A2A endpoint."}},
		Settings: []platform.Setting{
			{Name: SettingNotifyDowntime, Title: "Tell supervisors about new downtime", Type: "boolean", Default: "true",
				Description: "Each downtime that starts notifies the supervisors of its line."},
			{Name: SettingReasonMinutes, Title: "Remind about missing reasons after (minutes)", Type: "integer", Default: "15",
				Description: "Downtime still without a reason after this long reminds the line's supervisors once; 0 turns reminders off."},
		}}
}

// Run reminds the supervisors of each line about downtime still without a reason.
func (p *Plant) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	minutes, _ := strconv.Atoi(c.Setting(SettingReasonMinutes))
	if minutes <= 0 {
		return nil
	}
	for _, d := range p.Downtime() {
		if d.Reason == "" && !d.Start.After(now.Add(-time.Duration(minutes)*time.Minute)) {
			c.Notify(platform.Notification{Title: "Downtime without a reason on " + d.Resource,
				Body: fmt.Sprintf("Started %s UTC, still no reason after %d minutes.", d.Start.UTC().Format("15:04"), minutes),
				Ref:  DowntimeType + "/" + d.ID, Key: "reason:" + d.ID}, now, p.supervisorsOf(d.Resource))
		}
	}
	return nil
}

func (p *Plant) Read(c platform.Caller, name string) (any, *kernel.Error) {
	switch name {
	case "master":
		return p.Master(), nil
	case "planned-orders": // the orders of production.orders/1's providers
		return planned(c), nil
	}
	return p.Downtime(), nil
}

func (p *Plant) Input(c platform.Caller, name string, body []byte, now time.Time) (any, *kernel.Error) {
	switch name {
	case "states":
		var b StateBatch
		if json.Unmarshal(body, &b) != nil {
			return nil, invalid
		}
		return p.DeliverStates(c, b, now)
	}
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}
