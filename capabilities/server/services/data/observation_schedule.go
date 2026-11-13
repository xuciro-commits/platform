package data

import (
	"encoding/json"
	"strings"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

type ScheduledObservation struct {
	Ref        platform.AssetRef
	Version    string
	Definition platform.ObservationDefinition
	Publisher  string
	Scopes     []string
}

type ObservationSchedule struct {
	Member      func(id string) (platform.Member, bool)
	Definitions func() ([]platform.Definition, error)
	Active      func(record string) bool
	Publisher   func(record string) (string, []string, bool)
	Records     func(m platform.Member, object string, q platform.Query, at time.Time) ([]any, int, *kernel.Error)
	Windows     *ObservationWindows
	Evaluate    func(m platform.Member, ref platform.AssetRef, q ObservationEvaluate, at time.Time) *kernel.Error
}

func (s ObservationSchedule) RunDue(at time.Time) {
	definitions, err := s.Definitions()
	if err != nil {
		return
	}
	s.Run(ScheduledObservations(definitions, s.Active, s.Publisher), at)
}

func (s ObservationSchedule) Run(sources []ScheduledObservation, at time.Time) {
	for _, source := range sources {
		member, found := s.Member(source.Publisher)
		if !found {
			continue
		}
		member.Scopes = source.Scopes
		seconds := int64(source.Definition.WindowSeconds)
		until := time.Unix(at.Unix()/seconds*seconds, 0).UTC()
		for offset := 0; ; {
			page, total, refusal := s.Records(member, source.Definition.Object.Name, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, at)
			if refusal != nil {
				break
			}
			for _, record := range page {
				var row struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(platform.Raw(record), &row) != nil || row.ID == "" {
					continue
				}
				identity, err := platform.CanonicalDigest([]any{source.Ref, source.Version, row.ID, until})
				if err != nil {
					continue
				}
				slot := source.Ref.String() + "/" + row.ID
				prior, started := s.Windows.Begin(slot, identity)
				if !started {
					continue
				}
				if s.Evaluate(member, source.Ref, ObservationEvaluate{Record: row.ID, Until: until, Key: "scheduled-" + identity}, at) == nil {
					s.Windows.End(slot, identity)
				} else {
					s.Windows.End(slot, prior)
				}
			}
			offset += len(page)
			if len(page) == 0 || offset >= total {
				break
			}
		}
	}
}

func VisibleObservationEvidence(evidence *platform.ObservationDecisionEvidence, definition func() (platform.ObservationDefinition, bool), native func(name string, roles []string) bool, read func(typ, id string, fields []string) bool) *platform.ObservationDecisionEvidence {
	def, found := definition()
	if !found || !native(evidence.Definition.Ref.Name, def.Roles) {
		return nil
	}
	typ, id, valid := strings.Cut(evidence.Record, "/")
	if !valid || typ != def.Object.Name {
		return nil
	}
	fields := []string{def.DeviceField}
	if def.Decision != nil {
		fields = append(fields, ObservationDecisionFields(*def.Decision)...)
	}
	if !read(typ, id, fields) {
		return nil
	}
	copy := *evidence
	if evidence.Intervals != nil {
		intervals := evidence.Intervals.Clone()
		copy.Intervals = &intervals
	}
	return &copy
}

func ScheduledObservations(definitions []platform.Definition, active func(record string) bool, publisher func(record string) (string, []string, bool)) []ScheduledObservation {
	var sources []ScheduledObservation
	for _, definition := range definitions {
		if definition.Observation == nil || !definition.Observation.Scheduled || definition.OwnerRecord == "" || !active(definition.OwnerRecord) {
			continue
		}
		if name, scopes, _ := publisher(definition.OwnerRecord); name != "" {
			sources = append(sources, ScheduledObservation{Ref: definition.Ref, Version: definition.Version, Definition: *definition.Observation, Publisher: name, Scopes: scopes})
		}
	}
	return sources
}
