package host

import (
	"encoding/json"
	"fmt"
	"platformserver/services/actions"
	"slices"
	"strings"

	"platformserver/platform"
)

func (t *Tenant) planMailNotice(n platform.Notification, to platform.Reach) []platform.Effect {
	var effects []platform.Effect
	address, lang := to.Address, to.Language
	if address == "" || !to.Mail {
		return effects
	}
	due := to.Due(n.At)
	for _, ep := range t.endpoints {
		if ep.Kind != "email" || !slices.Contains(ep.Notifications, n.App) {
			continue
		}
		text := t.i18n.Say(lang, n.Body)
		if n.Ref != "" {
			text += "\n\n" + t.i18n.Say(lang, "About") + ": " + n.Ref
		}
		body, _ := json.Marshal(actions.Letter{To: address, Subject: t.i18n.Say(lang, n.Title), Text: strings.TrimSpace(text) + "\n\n— " + t.ID + " · " + n.App})
		effects = append(effects, platform.Effect{ID: fmt.Sprintf("%s:notice:%s:%s", t.ID, n.ID, ep.ID), Endpoint: ep.ID,
			Event: n.App + "/notification", Target: n.Ref, At: n.At, State: "pending", Due: due, Body: string(body)})
	}
	return effects
}
