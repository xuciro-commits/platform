package host

import (
	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	aiowner "platformserver/services/ai"

	"platformserver/platform"
	"platformserver/services/actions"
	"strings"
	"time"
)

func (h hostView) TrainingEvidence(event platform.Event) platform.AITrainingEvidence {
	return h.t.training().Evidence(event)
}

func (h hostView) TrainingReadable(member platform.Member, candidate platform.AITrainingCandidate, now time.Time) string {
	return h.t.training().Readable(platform.NewCaller(runtime{t: h.t}, member, platform.AIApp, false, false), candidate, now)
}

func (t *Tenant) training() aiowner.TrainingWorld {
	var models []platform.Effect
	for _, x := range t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.ModelEndpoint }) {
		models = append(models, x.Effect)
	}
	return aiowner.TrainingWorld{Tenant: t.ID, Models: models, App: t.App, Entity: t.Entity, Provider: t.router().Provider,
		Live:   t.LiveApps,
		Caller: func(app string) platform.Caller { return t.automation(app, false) },
		Image: func(ref, change string) (json.RawMessage, bool) {
			typ, id, ok := strings.Cut(ref, "/")
			if !ok {
				return nil, false
			}
			return records.LockedImage(t.Projection, typ, id, change)
		}}
}

var _ platform.AITrainingHost = hostView{}
