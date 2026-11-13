package kernel

import (
	"time"

	"platformserver/platform"
)

func (b *NoticeBoard) Deliver(app string, n platform.Notification, now time.Time, members []string, delivery map[string]platform.Reach, mail func(platform.Notification, platform.Reach)) []string {
	var out []string
	for _, m := range members {
		to, known := delivery[m]
		if !known {
			to = platform.Reach{InApp: true}
		}
		x := n
		x.App, x.At, x.Read = app, now, !to.InApp
		x, posted := b.Post(x, m)
		if !posted {
			continue
		}
		mail(x, to)
		out = append(out, m)
	}
	return out
}

func (d *NoticeDraft) Deliver(app string, n platform.Notification, now time.Time, members []string, console platform.IdentityProfile, mail func(platform.Notification, platform.Reach) []platform.Effect) []string {
	var out []string
	for _, member := range members {
		var reach platform.Reach
		if console != nil {
			reach = console.Reach(member)
		}
		n.Member, n.App, n.At = member, app, now
		x, added := d.Add(n, console != nil && !reach.InApp)
		if !added {
			continue
		}
		out = append(out, member)
		if console != nil {
			d.Mail(mail(x, reach)...)
		}
	}
	return out
}
