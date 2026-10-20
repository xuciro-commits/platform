package platformserver

import (
	"context"
	"encoding/json"
	"platformserver/internal/acceptance"
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
	if t.quarantined() {
		return
	}
	type scheduled struct {
		ref        platform.AssetRef
		version    string
		definition platform.ObservationDefinition
		publisher  string
		scopes     []string
	}
	var sources []scheduled
	t.mu.Lock()
	definitions, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		t.mu.Unlock()
		return
	}
	for _, definition := range definitions {
		if definition.Observation == nil || !definition.Observation.Scheduled || definition.OwnerRecord == "" || !t.observationSourceActive(definition.OwnerRecord) {
			continue
		}
		typ, id, valid := strings.Cut(definition.OwnerRecord, "/")
		if !valid {
			continue
		}
		t.records.mu.Lock()
		owner := t.records.types[typ]
		row := owner.rows.Get(id)
		publisher := ""
		var scopes []string
		if row != nil {
			publisher = businessField(owner.info, row.value, "publisher")
			var value struct {
				Scopes string `json:"publisherScopes"`
			}
			if json.Unmarshal(platform.Raw(row.value.Interface()), &value) == nil {
				if value.Scopes != "" && json.Unmarshal([]byte(value.Scopes), &scopes) != nil {
					publisher = ""
				}
			}
		}
		t.records.mu.Unlock()
		if publisher != "" {
			sources = append(sources, scheduled{definition.Ref, definition.Version, *definition.Observation, publisher, scopes})
		}
	}
	t.mu.Unlock()
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
				identity, err := acceptance.Digest([]any{source.ref, source.version, row.ID, until})
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
