package automate

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"platformserver/platform"
)

type NotificationEffect struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Role  string `json:"role"`
}

func (n NotificationEffect) Check() error {
	if strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.Body) == "" || n.Role == "" {
		return fmt.Errorf("A notification needs a title, message and recipient role")
	}
	return nil
}

func Notify(c platform.Caller, run *platform.Run, step string, n NotificationEffect, object, display, legacy string, subject json.RawMessage, now time.Time) json.RawMessage {
	ref := ""
	title := n.Title
	key := "automation:" + run.ID + ":" + step
	if legacy != "" && !strings.HasPrefix(run.Key, "manual:") {
		key = "alert:" + legacy + ":" + run.Key
	}
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
