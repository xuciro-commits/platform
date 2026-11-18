package authoring

import (
	"encoding/json"
	"platformserver/kernel/protocol"

	"platformserver/services/resources/monitoring"
)

func MonitoringAlerts(processes []protocol.Process) []monitoring.MonitoringAlert {
	out := []monitoring.MonitoringAlert{}
	for _, process := range processes {
		var installed protocol.Process
		if process.Archived || process.Paused || json.Unmarshal([]byte(process.Published), &installed) != nil || installed.Kind != "automation" {
			continue
		}
		for _, step := range installed.Steps {
			if step.Notify != nil {
				out = append(out, monitoring.MonitoringAlert{ID: process.ID, Title: installed.Title, Object: installed.Object, Repeat: installed.Repeat, Message: step.Notify.Body, Role: step.Notify.Role})
				break
			}
		}
	}
	return out
}
