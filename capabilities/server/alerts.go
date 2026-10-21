package platformserver

import (
	"encoding/json"
	"strings"
	"time"

	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

// raiseAlerts tells people about a record that came to match an alert rule.
// It runs after every accepted decision on a builder object;
// notices are keyed, so a replay or a second look tells nobody twice.
func (t *Tenant) raiseAlerts(e platform.Event, now time.Time) {
	s := e.Record.GetSubmission()
	typ := s.GetTarget().GetType()
	if e.App != construction.ID || !strings.HasPrefix(typ, construction.ID+".") || typ == construction.AlertType {
		return
	}
	c := t.automation(construction.ID, false)
	owner, ok := t.app(construction.ID).(host.AlertDefinitions)
	if !ok {
		return
	}
	rules, err := owner.AlertRules(typ)
	if err != nil {
		return
	}
	if len(rules) == 0 {
		return
	}
	info, ok := t.entity(typ)
	if !ok {
		return
	}
	held, ok := t.held(typ + "/" + s.GetTarget().GetId())
	if !ok {
		return
	}
	record := map[string]any{}
	raw, _ := json.Marshal(held)
	json.Unmarshal(raw, &record)
	for _, rule := range rules {
		f, _ := info.Field(rule.Field)
		if !rule.Matches(record, f.Type) {
			continue
		}
		key := "alert:" + rule.ID + ":" + s.GetTarget().GetId()
		if rule.Every {
			key += ":" + e.Record.GetChangeId()
		}
		role := rule.Role
		if role == "" {
			role = construction.Builder
		}
		title := rule.Title
		if display, ok := record[info.Display].(string); ok && display != "" && info.Display != "id" {
			title += ": " + display
		}
		c.Notify(platform.Notification{Title: title, Body: rule.Message, Ref: typ + "/" + s.GetTarget().GetId(), Key: key}, now, platform.Recipient{AppRole: role})
	}
}
