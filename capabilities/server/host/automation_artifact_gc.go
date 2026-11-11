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
				t.Projection.Lock()
				defer t.Projection.Unlock()
				entity := t.Projection.Type(platform.FlowInstanceType)
				if entity == nil {
					return fmt.Errorf("Flow artifact references have no installed record owner")
				}
				for _, row := range entity.Rows().All() {
					image, err := row.Image()
					if err != nil {
						return err
					}
					if err := visit(image); err != nil {
						return err
					}
					current, err := json.Marshal(row.Value.Interface())
					if err != nil {
						return err
					}
					if err := visit(current); err != nil {
						return err
					}
					for _, change := range row.RecordHistory() {
						for _, field := range change.Fields {
							if err := visit(field.Before); err != nil {
								return err
							}
							if err := visit(field.After); err != nil {
								return err
							}
						}
					}
				}
				return nil
			})
		})
	})
}
