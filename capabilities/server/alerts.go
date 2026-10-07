package platformserver

import (
	"encoding/json"
	"strings"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

// raiseAlerts tells people about a record that came to match an alert rule
// (ADR-0077). It runs after every accepted decision on a builder object;
// notices are keyed, so a replay or a second look tells nobody twice.
func (t *Tenant) raiseAlerts(e platform.Event, now time.Time) {
	s := e.Record.GetSubmission()
	typ := s.GetTarget().GetType()
	if e.App != build.ID || !strings.HasPrefix(typ, build.ID+".") || typ == build.AlertType {
		return
	}
	c := t.automation(build.ID, false)
	domain, _ := json.Marshal([]any{[]any{"object", "=", typ}, []any{"active", "=", true}})
	rules, _, _ := platform.Find[build.Alert](c, platform.Query{Domain: domain, Limit: 100})
	if len(rules) == 0 {
		return
	}
	info, ok := t.entity(typ)
	if !ok {
		return
	}
	held, ok := t.Held(typ + "/" + s.GetTarget().GetId())
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
			role = build.Builder
		}
		title := rule.Title
		if display, ok := record[info.Display].(string); ok && display != "" && info.Display != "id" {
			title += ": " + display
		}
		c.Notify(platform.Notification{Title: title, Body: rule.Message, Ref: typ + "/" + s.GetTarget().GetId(), Key: key}, now, platform.Recipient{AppRole: role})
	}
}
