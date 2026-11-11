package host

import (
	"fmt"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
	"slices"
	"strings"
	"time"
)

func (t *Tenant) validateAcceptedIntents(intents []platform.Effect) error {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	ids := map[string]bool{}
	for _, x := range intents {
		if err := platform.CheckApplicationBindings(x.Applications); err != nil {
			return err
		}
		kind, ok := strings.CutPrefix(x.Event, x.App+"/")
		id := fmt.Sprintf("%s:%s:%s:%s:%s", t.ID, x.App, kind, x.Key, x.Endpoint)
		if kind == "model" && x.Endpoint == acceptedformat.ModelEndpoint {
			id = fmt.Sprintf("%s:%s:model:%s", t.ID, x.App, x.Key)
		}
		if !ok || kind == "" || x.App == "" || t.app(x.App) == nil || x.Key == "" || x.Endpoint == "" ||
			x.ID != id ||
			ids[x.ID] || x.State != "pending" && x.State != "held" || x.At.IsZero() || !x.Due.Equal(x.At) ||
			x.Attempts != 0 || !x.Last.IsZero() || x.Error != "" || x.Digest != "" ||
			x.State == "held" && x.Agent == "" || slices.ContainsFunc(t.outbound, func(e *effect) bool { return e.ID == x.ID }) {
			return fmt.Errorf("accepted result has an invalid or duplicate outbound intent")
		}
		ids[x.ID] = true
	}
	return nil
}

func (t *Tenant) installAcceptedIntents(intents []platform.Effect) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, planned := range intents {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
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
	console, _ := d.tenant.app(controlowner.PlatformApp).(platform.IdentityProfile)
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
	defer t.opsMu.Unlock()
	t.acted += n.Seq - n.Base
	for _, planned := range n.Effects {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}
