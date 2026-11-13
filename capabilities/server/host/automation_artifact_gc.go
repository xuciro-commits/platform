package host

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
)

func (t *Tenant) frames() automation.FrameStore {
	return automation.FrameStore{Tenant: t.ID, Files: t.files()}
}

func (t *Tenant) beginFlowArtifactPreparation() func() { return t.flowArtifacts.Preparing() }

func (t *Tenant) flowArtifactRecoveryComplete(started time.Time) {
	if started.IsZero() || t.quarantined() {
		return
	}
	t.flowArtifacts.Recovered(started)
}

func (t *Tenant) collectUnacceptedFlowArtifacts(ctx context.Context, now time.Time) error {
	return t.flowArtifacts.Collect(ctx, t.frames(), now, t.quarantined, func() (map[string]bool, error) {
		return readWithError(t, ctx, func(t *Tenant) (map[string]bool, error) {
			return t.frames().References(func(visit func(json.RawMessage) error) error {
				found, err := t.Projection.VisitImages(platform.FlowInstanceType, visit)
				if !found {
					return fmt.Errorf("Flow artifact references have no installed record owner")
				}
				return err
			})
		})
	})
}
