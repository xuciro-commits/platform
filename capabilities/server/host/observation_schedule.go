package host

import records "platformserver/kernel/records"

import (
	"context"
	"encoding/json"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	controlowner "platformserver/services/security/control"

	"strings"
	"time"

	"platformserver/platform"
)

func (t *Tenant) EvaluateObservations(at time.Time) {
	if t.businessStopped() || !t.OwnerActive(controlowner.BuildApp) {
		return
	}
	type scheduled struct {
		ref        platform.AssetRef
		version    string
		definition platform.ObservationDefinition
		publisher  string
		scopes     []string
	}
	sources, _ := coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]scheduled, *kernel.Error) {
		var sources []scheduled
		definitions, err := t.ownedAssetDefinitions(t.definitions)
		if err != nil {
			return nil, nil
		}
		for _, definition := range definitions {
			if definition.Observation == nil || !definition.Observation.Scheduled || definition.OwnerRecord == "" || !t.observationSourceActive(definition.OwnerRecord) {
				continue
			}
			typ, id, valid := strings.Cut(definition.OwnerRecord, "/")
			if !valid {
				continue
			}
			publisher := ""
			var scopes []string
			func() {
				t.Projection.Lock()
				defer t.Projection.Unlock()
				owner := t.Projection.Type(typ)
				row := owner.Rows().Get(id)
				if row != nil {
					publisher = records.BusinessField(owner.Info(), row.Value, "publisher")
					var value struct {
						Scopes string `json:"publisherScopes"`
					}
					if json.Unmarshal(platform.Raw(row.Value.Interface()), &value) == nil {
						if value.Scopes != "" && json.Unmarshal([]byte(value.Scopes), &scopes) != nil {
							publisher = ""
						}
					}
				}
			}()
			if publisher != "" {
				sources = append(sources, scheduled{definition.Ref, definition.Version, *definition.Observation, publisher, scopes})
			}
		}
		return sources, nil
	})
	for _, source := range sources {
		member, found := t.Member(source.publisher)
		if !found {
			continue
		}
		member.Scopes = source.scopes
		seconds := int64(source.definition.WindowSeconds)
		until := time.Unix(at.Unix()/seconds*seconds, 0).UTC()
		for offset := 0; ; {
			page, refusal := t.Records(context.Background(), member, source.definition.Object.Name, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, at)
			if refusal != nil {
				break
			}
			for _, record := range page.Records {
				var row struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(platform.Raw(record), &row) != nil || row.ID == "" {
					continue
				}
				identity, err := platform.CanonicalDigest([]any{source.ref, source.version, row.ID, until})
				if err != nil {
					continue
				}
				slot := source.ref.String() + "/" + row.ID
				prior, started := t.schedules.Windows.Begin(slot, identity)
				if !started {
					continue
				}
				_, problem := t.EvaluateObservation(context.Background(), member, source.ref, dataowner.ObservationEvaluate{Record: row.ID, Until: until, Key: "scheduled-" + identity}, at)
				if problem == nil {
					t.schedules.Windows.End(slot, identity)
				} else {
					t.schedules.Windows.End(slot, prior)
				}
			}
			offset += len(page.Records)
			if len(page.Records) == 0 || offset >= page.Total {
				break
			}
		}
	}
}
