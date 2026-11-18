package automate

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"time"

	"platformserver/platform"
)

func Notify(c platform.Caller, run *platform.Run, step string, n protocol.NotificationEffect, object, display string, subject json.RawMessage, now time.Time) json.RawMessage {
	ref := ""
	title := n.Title
	key := "automation:" + run.ID + ":" + step
	if object != "" {
		var record map[string]any
		_ = json.Unmarshal(subject, &record)
		id, _ := record["id"].(string)
		ref = object + "/" + id
		if display != "" && display != "id" {
			if value, ok := record[display].(string); ok && value != "" {
				title += ": " + value
			}
		}

	}
	to := c.Notify(platform.Notification{Title: title, Body: n.Body, Ref: ref, Key: key}, now, platform.Recipient{AppRole: n.Role})
	return platform.Raw(map[string]any{"recipients": to})
}
