package mes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

// Settings of the plant (ADR-0013), set by administrators in Settings.
const (
	SettingNotifyDowntime = "notify-downtime"
	SettingReasonMinutes  = "reason-reminder-minutes"
	JobReasons            = "downtime-reasons"
)

// Manifest declares the plant as the "mes" app (ADR-0010): its actions, its
// reads, its connector inputs (batches and pages, both journaled; the host keeps
// the connectors), its settings and its scheduled job (ADR-0013).
func (p *Plant) Manifest() platformserver.Manifest {
	return platformserver.Manifest{ID: "mes", Version: "1", Actions: p.ledger.Catalog,
		Reads:  []string{"master", "orders", "sfcs", "planned-orders", "downtime"},
		Inputs: map[string]bool{"states": true, "planned-orders": true},
		Jobs:   []platformserver.Job{{Name: JobReasons, Title: "Remind supervisors of downtime without a reason", Every: 5 * time.Minute}},
		Emits: []platformserver.EffectKind{{Name: EffectConfirmation, Title: "Order confirmation to the ERP",
			Description: "When the last SFC of an order ends, its yield and scrap are confirmed to the ERP (SAP production order confirmation); the ERP answers with its confirmation number."}},
		Settings: []platformserver.Setting{
			{Name: SettingNotifyDowntime, Title: "Tell supervisors about new downtime", Type: "boolean", Default: "true",
				Description: "Each downtime that starts notifies the supervisors of its line."},
			{Name: SettingReasonMinutes, Title: "Remind about missing reasons after (minutes)", Type: "integer", Default: "15",
				Description: "Downtime still without a reason after this long reminds the line's supervisors once; 0 turns reminders off."},
		}}
}

// Run reminds the supervisors of each line about downtime still without a reason.
func (p *Plant) Run(c platformserver.Caller, _ string, now time.Time) *kernel.Error {
	minutes, _ := strconv.Atoi(c.Setting(SettingReasonMinutes))
	if minutes <= 0 {
		return nil
	}
	for _, d := range p.Downtime() {
		if d.Reason == "" && !d.Start.After(now.Add(-time.Duration(minutes)*time.Minute)) {
			c.Notify(platformserver.Notification{Title: "Downtime without a reason on " + d.Resource,
				Body: fmt.Sprintf("Started %s UTC, still no reason after %d minutes.", d.Start.UTC().Format("15:04"), minutes),
				Ref:  DowntimeType + "/" + d.ID, Key: "reason:" + d.ID}, now, p.supervisorsOf(d.Resource))
		}
	}
	return nil
}

func (p *Plant) Read(_ platformserver.Caller, name string) (any, *kernel.Error) {
	switch name {
	case "master":
		return p.Master(), nil
	case "orders":
		return p.Orders(), nil
	case "sfcs":
		return p.SFCs(), nil
	case "planned-orders":
		return p.Planned(), nil
	}
	return p.Downtime(), nil
}

func (p *Plant) Input(c platformserver.Caller, name string, body []byte, now time.Time) (any, *kernel.Error) {
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
