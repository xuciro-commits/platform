package host

import (
	"platformserver/platform"
	"platformserver/services/actions"
)

func (t *Tenant) planMailNotice(n platform.Notification, to platform.Reach) []platform.Effect {
	return actions.PlanMailNotice(t.ID, n, to, t.endpoints.All, func(lang, s string) string { return t.i18n.Say(lang, s) })
}
