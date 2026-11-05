package store

import (
	"slices"
	"sync"
	"time"

	"platformserver/platform"
)

type Memory struct {
	mu             sync.Mutex
	vectors        map[string][]float32
	transcripts    map[string][]Transcript
	observations   map[string][]platform.Observation
	observationIDs map[string]map[string]struct{}
}

const (
	transcriptsKept  = 500
	observationsKept = 20000
)

func (m *Memory) Vectors(tenant, model string, hashes []string) map[string][]float32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]float32{}
	for _, h := range hashes {
		if v := m.vectors[tenant+"/"+model+"/"+h]; v != nil {
			out[h] = v
		}
	}
	return out
}

func (m *Memory) SaveVectors(tenant, model string, vs map[string][]float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vectors == nil {
		m.vectors = map[string][]float32{}
	}
	for h, v := range vs {
		m.vectors[tenant+"/"+model+"/"+h] = v
	}
}

func (m *Memory) SaveTranscript(x Transcript) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.transcripts == nil {
		m.transcripts = map[string][]Transcript{}
	}
	kept := append(m.transcripts[x.Tenant], x)
	if len(kept) > transcriptsKept {
		kept = kept[len(kept)-transcriptsKept:]
	}
	m.transcripts[x.Tenant] = kept
}

func (m *Memory) Transcripts(tenant, run string, limit int) []Transcript {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Transcript{}
	for _, x := range slices.Backward(m.transcripts[tenant]) {
		if (run == "" || x.Run == run) && len(out) < limit {
			out = append(out, x)
		}
	}
	return out
}

func (m *Memory) PurgeTranscripts(tenant string, before time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.transcripts[tenant]) == 0 {
		return
	}
	m.transcripts[tenant] = slices.DeleteFunc(m.transcripts[tenant], func(x Transcript) bool { return x.At.Before(before) })
}

func (m *Memory) SaveObservations(events []platform.Observation) error {
	if len(events) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.observations == nil {
		m.observations = map[string][]platform.Observation{}
	}
	if m.observationIDs == nil {
		m.observationIDs = map[string]map[string]struct{}{}
	}
	touched := map[string]struct{}{}
	for _, event := range events {
		event = normalizeObservation(event)
		ids := m.observationIDs[event.Tenant]
		if ids == nil {
			ids = map[string]struct{}{}
			m.observationIDs[event.Tenant] = ids
		}
		if _, seen := ids[event.ID]; seen {
			continue
		}
		ids[event.ID] = struct{}{}
		m.observations[event.Tenant] = append(m.observations[event.Tenant], event)
		touched[event.Tenant] = struct{}{}
	}
	for tenant := range touched {
		kept := m.observations[tenant]
		sortObservationsOldest(kept)
		if len(kept) > observationsKept {
			excess := len(kept) - observationsKept
			for _, event := range kept[:excess] {
				delete(m.observationIDs[tenant], event.ID)
			}
			kept = kept[excess:]
		}
		if len(kept) == 0 {
			delete(m.observations, tenant)
			delete(m.observationIDs, tenant)
		} else {
			m.observations[tenant] = kept
		}
	}
	return nil
}

func (m *Memory) QueryObservations(query platform.ObservationQuery) (platform.ObservationPage, error) {
	m.mu.Lock()
	events := slices.Clone(m.observations[query.Tenant])
	var retainedFrom time.Time
	if len(events) > 0 {
		retainedFrom = events[0].At
	}
	m.mu.Unlock()
	return selectObservationPage(events, query, retainedFrom)
}

func (m *Memory) PurgeObservations(tenant string, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := m.observations[tenant]
	if len(events) == 0 {
		return 0, nil
	}
	kept := make([]platform.Observation, 0, len(events))
	for _, event := range events {
		if event.At.Before(before) {
			continue
		}
		kept = append(kept, event)
	}
	removed := len(events) - len(kept)
	if removed == 0 {
		return 0, nil
	}
	if len(kept) == 0 {
		delete(m.observations, tenant)
		delete(m.observationIDs, tenant)
		return removed, nil
	}
	ids := make(map[string]struct{}, len(kept))
	for _, event := range kept {
		ids[event.ID] = struct{}{}
	}
	m.observations[tenant] = kept
	m.observationIDs[tenant] = ids
	return removed, nil
}
