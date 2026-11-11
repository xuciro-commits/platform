package host

import (
	"context"
	"crypto/hmac"
	dataowner "platformserver/services/data"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) listenerInput(ctx context.Context, m platform.Member, path, secret, origin string, body []byte, now time.Time) (any, *kernel.Error) {
	connector, refusal := coordinatedRecordRead(t, ctx, func(t *Tenant) (string, *kernel.Error) {
		rows, _, refusal := platform.Find[dataowner.Listener](t.automation(build.ID, false), platform.Query{})
		if refusal != nil {
			return "", refusal
		}
		owner := t.DataOwner()
		var matches []dataowner.Listener
		for _, live := range rows {
			if !live.Enabled || live.State != "published" {
				continue
			}
			listener, err := owner.ExecutionListener(live)
			if err != nil {
				return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if listener.Path == path {
				matches = append(matches, listener)
			}
		}
		if len(matches) != 1 {
			return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		listener := matches[0]
		value, ok := t.credentialValue(listener.Secret, "listener/"+listener.ID, origin)
		if !ok || len(value) == 0 || secret == "" || !hmac.Equal(value, []byte(secret)) {
			return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return listener.Connector, nil
	})
	if refusal != nil {
		return nil, refusal
	}
	return t.Input(m, connector, body, now)
}
