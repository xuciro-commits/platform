package host

import (
	"context"
	"encoding/json"
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type legacySeedState interface {
	StateRecord(context.Context, string, string, string) (json.RawMessage, error)
}

func (t *Tenant) restoreLegacySeeds(ctx context.Context, state legacySeedState) error {
	for _, app := range t.apps {
		seeder, ok := app.(platform.LegacySeeder)
		if !ok {
			continue
		}
		for _, seed := range seeder.LegacySeeds() {
			image, err := state.StateRecord(ctx, t.ID, seed.Type, seed.ID)
			if err != nil {
				return fmt.Errorf("read legacy seed %s/%s: %w", seed.Type, seed.ID, err)
			}
			if len(image) == 0 {
				continue
			}
			t.records.Lock()
			entity := t.records.Type(seed.Type)
			exists := entity != nil && entity.Rows().Get(seed.ID) != nil
			t.records.Unlock()
			if exists {
				continue
			}
			if entity == nil {
				return fmt.Errorf("legacy seed type %s is not declared", seed.Type)
			}
			caller := platform.NewCaller(nil, platform.Member{ID: "seed"}, app.Manifest().ID, false, false)
			record := &pb.ChangeRecord{ChangeId: "seed", Submission: &pb.Submission{PrincipalId: "seed", Schema: &pb.SchemaRef{Name: "seed"}}}
			if refusal := t.records.Put(caller, record, seed.Value); refusal != nil {
				return fmt.Errorf("restore legacy seed %s/%s: %s", seed.Type, seed.ID, refusal.Message)
			}
		}
	}
	return nil
}
