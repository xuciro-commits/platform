package host

import (
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"
)

func (t *Tenant) validateAcceptedIntents(intents []platform.Effect) error {
	return runtimekernel.CheckIntents(t.ID, intents, func(app string) bool { return t.App(app) != nil }, func(id string) bool {
		return t.outbound.Any(func(e actions.Queued) bool { return e.ID == id })
	})
}

func (t *Tenant) installAcceptedIntents(intents []platform.Effect) {
	t.outbound.Add(t.current(), intents...)
}

func (d *stagedDecision) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	apps := slices.Clone(d.tenant.apps)
	if i, ok := d.tenant.appIndex[controlowner.PlatformApp]; ok {
		apps[i] = d.stateApp(apps[i])
	}
	view := &Tenant{ownerSet: d.tenant.with(apps), directory: d.directoryView()}
	return view.recipients(c, now, to)
}

func (d *stagedDecision) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	console, _ := d.tenant.App(controlowner.PlatformApp).(platform.IdentityProfile)
	if private, ok := d.states[controlowner.PlatformApp].(platform.IdentityProfile); ok {
		console = private
	}
	var out []string
	for _, member := range d.recipients(c, now, to) {
		var reach platform.Reach
		if console != nil {
			reach = console.Reach(member)
		}
		n.Member, n.App, n.At = member, c.App, now
		x, added := d.notices.Add(n, console != nil && !reach.InApp)
		if !added {
			continue
		}
		out = append(out, member)
		if console != nil {
			router := d.routingTenant()
			router.opsMu.Lock()
			d.notices.Mail(router.planMailNotice(x, reach)...)
			router.opsMu.Unlock()
		}
	}
	return out
}

func (d *stagedDecision) Seen(c platform.Caller, keys ...string) { d.notices.Seen(c.App, keys) }

func (t *Tenant) applyAcceptedNotices(n *acceptedformat.Notices) {
	if n == nil {
		return
	}
	t.notices.Restore(slices.Clone(n.Notices), n.Seq)
	t.opsMu.Lock()
	t.acted += n.Seq - n.Base
	t.opsMu.Unlock()
	t.outbound.Add(t.current(), n.Effects...)
}
