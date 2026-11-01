package platformserver

import (
	"context"
	"crypto/hmac"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

func (t *Tenant) listenerInput(ctx context.Context, m platform.Member, path, secret, origin string, body []byte, now time.Time) (any, *kernel.Error) {
	return coordinatedRecordRead(t, ctx, func() (any, *kernel.Error) {
		if t.businessStopped() {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		rows, _, refusal := platform.Find[build.Listener](t.automation(build.ID, false), platform.Query{})
		if refusal != nil {
			return nil, refusal
		}
		owner := t.app(build.ID).(build.IntegrationDefinitions)
		var matches []build.Listener
		for _, live := range rows {
			if !live.Enabled || live.State != "published" {
				continue
			}
			listener, err := owner.ExecutionListener(live)
			if err != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if listener.Path == path {
				matches = append(matches, listener)
			}
		}
		if len(matches) != 1 {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		listener := matches[0]
		value, ok := t.credentialValue(listener.Secret, "listener/"+listener.ID, origin)
		if !ok || len(value) == 0 || secret == "" || !hmac.Equal(value, []byte(secret)) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return t.input(m, listener.Connector, body, now)
	})
}
