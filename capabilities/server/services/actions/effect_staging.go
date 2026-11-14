package actions

import (
	"time"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type EffectStaging struct {
	App            string
	Admin          string
	Console        bool
	At             time.Time
	Caller         func(app string) platform.Caller
	Answer         func() error
	Quarantined    func() bool
	Authorizations func() []acceptedformat.AuthorizationEvidence
	Reset          func()
	Handoff        func()
	Result         func() error
}

func (s EffectStaging) Stage(result *acceptedformat.Effect, before acceptedformat.EffectState, after platform.Effect) error {
	if after.State == "failed" && s.Console {
		s.Caller(s.App).Notify(platform.Notification{Title: "Effect to " + after.Endpoint + " failed: " + after.Event,
			Body: after.Error + ". Retry it in Settings → Automation once the cause is fixed.",
			Key:  "failed:" + after.ID}, s.At, platform.Recipient{AppRole: s.Admin})
	}
	if (platform.EffectSettled(after.State) || after.State == "unknown") && before.State != "discarded" {
		if err := s.Answer(); err != nil {
			if s.Quarantined() {
				return err
			}
			result.CallbackError = err.Error()
			result.Authorizations = s.Authorizations()
			s.Reset()
		}
	}
	if before.Endpoint == acceptedformat.ModelEndpoint && (after.State == "rejected" || after.State == "failed" || after.State == "unknown") {
		s.Handoff()
	}
	return s.Result()
}
