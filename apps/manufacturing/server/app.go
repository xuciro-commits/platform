package mes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Settings of the plant (ADR-0013), set by administrators in Settings.
const (
	SettingNotifyDowntime = "notify-downtime"
	SettingReasonMinutes  = "reason-reminder-minutes"
	JobReasons            = "downtime-reasons"
)

// Snapshot and Restore: the plant's facts (ERP claims, gateway batches), the
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
// reads, its connector inputs (batches and pages, both journaled; the host keeps
// the connectors), its settings, its scheduled job (ADR-0013) and its flow (ADR-0020).
func (p *Plant) Manifest() platform.Manifest {
	return platform.Manifest{ID: "mes", Title: "Plant operations", Version: "1", Actions: p.ledger.Catalog, Flows: []platform.Flow{p.confirmation()}, Agents: []platform.Agent{p.fixer(), p.planner()},
		Reads: []string{"master", "planned-orders", "downtime"}, Entities: p.entities,
		Inputs: map[string]bool{"states": true, "planned-orders": true},
		Jobs:   []platform.Job{{Name: JobReasons, Title: "Remind supervisors of downtime without a reason", Every: 5 * time.Minute}},
		Emits: []platform.EffectKind{{Name: EffectConfirmation, Title: "Order confirmation to the ERP", Irreversible: true,
			Description: "When the last SFC of an order ends, its yield and scrap are confirmed to the ERP (SAP production order confirmation); the ERP answers with its confirmation number."},
			{Name: EffectLeadTime, Title: "Lead time from a supplier",
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

func (p *Plant) Read(_ platform.Caller, name string) (any, *kernel.Error) {
	switch name {
	case "master":
		return p.Master(), nil
	case "planned-orders":
		return p.Planned(), nil
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
	case "planned-orders":
		var page PlannedPage
		if json.Unmarshal(body, &page) != nil {
			return nil, invalid
		}
		return nil, p.DeliverPlanned(c, page, now)
	}
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}
