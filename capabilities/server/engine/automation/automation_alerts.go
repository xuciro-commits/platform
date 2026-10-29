package automation

import (
	"platformserver/platform"
	"time"
)

type AlertDefinition struct {
	ID   string
	Rule platform.AlertRule
}

func RaiseAlerts(c platform.Caller, rules []AlertDefinition, info platform.EntityInfo, record map[string]any, event platform.Event, now time.Time) {
	for _, rule := range rules {
		f, _ := info.Field(rule.Rule.Field)
		if !platform.Holds(record[rule.Rule.Field], f.Type, rule.Rule.Operator, rule.Rule.Value) {
			continue
		}
		key := "alert:" + rule.ID + ":" + event.Record.GetSubmission().GetTarget().GetId()
		if rule.Rule.Every {
			key += ":" + event.Record.GetChangeId()
		}
		role := rule.Rule.Role
		if role == "" {
			role = "builder"
		}
		title := rule.Rule.Title
		if display, ok := record[info.Display].(string); ok && display != "" && info.Display != "id" {
			title += ": " + display
		}
		c.Notify(platform.Notification{Title: title, Body: rule.Rule.Message, Ref: info.Type + "/" + event.Record.GetSubmission().GetTarget().GetId(), Key: key}, now, platform.Recipient{AppRole: role})
	}
}
