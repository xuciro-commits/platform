package host

import (
	"time"

	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	controlowner "platformserver/services/security/control"
)

func (t *Tenant) chainRead(m platform.Member, ref string, now time.Time) (automation.Chain, *platform.Error) {
	return automation.ChainReader{
		Record: func(typ, id string) (any, *platform.Error) {
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
