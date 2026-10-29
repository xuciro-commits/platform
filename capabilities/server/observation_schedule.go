package platformserver

import records "platformserver/kernel/records"

import (
	"context"
	"encoding/json"
	"platformkernel/kernel"
	controlowner "platformserver/kernel/control"

	"strings"
	"time"

	"platformserver/platform"
)

type observationWindowAttempt struct {
	Key  string
	Busy bool
}

// EvaluateObservations shares the existing outside source loop. One completed
// source window produces at most an ordinary business decision. Unmatched
// windows, source points and this disposable scheduling memory are not logged.
func (t *Tenant) EvaluateObservations(at time.Time) {
	if t.businessStopped() || !t.ownerActive(controlowner.BuildApp) {
		return
	}
	type scheduled struct {
		ref        platform.AssetRef
		version    string
		definition platform.ObservationDefinition
		publisher  string
		scopes     []string
	}
	sources, _ := coordinatedRecordRead(t, context.Background(), func() ([]scheduled, *kernel.Error) {
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
				t.records.Lock()
				defer t.records.Unlock()
				owner := t.records.Type(typ)
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
			page, refusal := t.Records(member, source.definition.Object.Name, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, at)
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
				t.mu.Lock()
				prior := t.observationWindows[slot]
				if prior.Busy || prior.Key == identity {
					t.mu.Unlock()
					continue
				}
				if t.observationWindows == nil {
					t.observationWindows = map[string]observationWindowAttempt{}
				}
				t.observationWindows[slot] = observationWindowAttempt{Key: prior.Key, Busy: true}
				t.mu.Unlock()
				_, problem := t.EvaluateObservation(context.Background(), member, source.ref, ObservationEvaluate{Record: row.ID, Until: until, Key: "scheduled-" + identity}, at)
				t.mu.Lock()
				if problem == nil {
					t.observationWindows[slot] = observationWindowAttempt{Key: identity}
				} else {
					t.observationWindows[slot] = observationWindowAttempt{Key: prior.Key}
				}
				t.mu.Unlock()
			}
			offset += len(page.Records)
			if len(page.Records) == 0 || offset >= page.Total {
				break
			}
		}
	}
}
