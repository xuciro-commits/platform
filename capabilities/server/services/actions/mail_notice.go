package actions

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func PlanMailNotice(tenant string, n platform.Notification, to platform.Reach, endpoints func() []Endpoint, say func(lang, s string) string) []platform.Effect {
	var effects []platform.Effect
	address, lang := to.Address, to.Language
	if address == "" || !to.Mail {
		return effects
	}
	due := to.Due(n.At)
	for _, ep := range endpoints() {
		if ep.Kind != "email" || !slices.Contains(ep.Notifications, n.App) {
			continue
		}
		text := say(lang, n.Body)
		if n.Ref != "" {
			text += "\n\n" + say(lang, "About") + ": " + n.Ref
		}
		body, _ := json.Marshal(Letter{To: address, Subject: say(lang, n.Title), Text: strings.TrimSpace(text) + "\n\n— " + tenant + " · " + n.App})
		effects = append(effects, platform.Effect{ID: fmt.Sprintf("%s:notice:%s:%s", tenant, n.ID, ep.ID), Endpoint: ep.ID,
			Event: n.App + "/notification", Target: n.Ref, At: n.At, State: "pending", Due: due, Body: string(body)})
	}
	return effects
}
