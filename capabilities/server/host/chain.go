package host

import (
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	controlowner "platformserver/services/security/control"
)

func (t *Tenant) chainRead(m platform.Member, ref string, now time.Time) (automation.Chain, *kernel.Error) {
	return automation.ChainReader{
		Record: func(typ, id string) (any, *kernel.Error) {
			view, err := t.recordOf(m, typ, id, now)
			return view.Record, err
		},
		Find: func(typ string, query platform.Query) []any {
			page, err := t.objectOwner(t.Projection).Records(m, typ, query, now)
			if err != nil {
				return nil
			}
			return page.Records
		},
		Effects: func() []platform.Effect {
			if m.Roles[controlowner.PlatformApp] != controlowner.Admin {
				return nil
			}
			return t.Effects(now)
		}}.Read(ref)
}
