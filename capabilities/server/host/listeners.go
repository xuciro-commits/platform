package host

import (
	"context"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) listenerInput(ctx context.Context, m platform.Member, path, secret, origin string, body []byte, now time.Time) (any, *kernel.Error) {
	connector, refusal := coordinatedRecordRead(t, ctx, func(t *Tenant) (string, *kernel.Error) {
		rows, _, refusal := platform.Find[dataowner.Listener](t.automation(build.ID, false), platform.Query{})
		if refusal != nil {
			return "", refusal
		}
		return t.DataOwner().AuthorizeListener(rows, path, secret, origin, t.CredentialValue)
	})
	if refusal != nil {
		return nil, refusal
	}
	return t.Input(m, connector, body, now)
}
