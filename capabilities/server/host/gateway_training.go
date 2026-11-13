package host

import (
	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"
	aiowner "platformserver/services/ai"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	"strings"
	"time"
)

func (h hostView) TrainingEvidence(event platform.Event) platform.AITrainingEvidence {
	out := platform.AITrainingEvidence{}
	images := map[string]json.RawMessage{}
	for _, ref := range event.Changed {
		if image, ok := h.trainingRecordAt(ref, event.Record.GetChangeId()); ok {
			images[ref] = image
		}
	}
	for _, app := range h.t.LiveApps() {
		if owner, ok := app.(platform.TrainingEvidenceOwner); ok {
			out.Candidates = append(out.Candidates, owner.TrainingCandidates(h.t.automation(app.Manifest().ID, false), event, images)...)
		}
	}
	return h.t.training().Evidence(out, event, images)
}

func (h hostView) TrainingReadable(member platform.Member, candidate platform.AITrainingCandidate, now time.Time) string {
	if candidate.Tenant != h.t.ID {
		return "The training candidate belongs to another tenant"
	}
	return h.t.training().Readable(platform.NewCaller(runtime{t: h.t}, member, platform.AIApp, false, false), candidate, now)
}

func (t *Tenant) training() aiowner.TrainingWorld {
	var models []platform.Effect
	for _, x := range t.outbound.Select(func(x actions.Queued) bool { return x.Endpoint == acceptedformat.ModelEndpoint }) {
		models = append(models, x.Effect)
	}
	return aiowner.TrainingWorld{Tenant: t.ID, Models: models, App: t.App, Entity: t.Entity, Provider: t.router().Provider}
}

var _ platform.AITrainingHost = hostView{}

func (h hostView) trainingRecordAt(ref, change string) (json.RawMessage, bool) {
	typ, id, ok := strings.Cut(ref, "/")
	if !ok {
		return nil, false
	}
	h.t.Projection.Lock()
	entity := h.t.Projection.Type(typ)
	var row *records.Row
	if entity != nil {
		row = entity.Rows().Get(id)
	}
	h.t.Projection.Unlock()
	if row == nil {
		return nil, false
	}
	return row.ImageAt(change)
}
