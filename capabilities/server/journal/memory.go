package journal

import (
	"slices"
	"sync"
	"time"
)

// Memory is the derived store of a host without a journal — development,
// tests, the lightweight profile before its first append: vectors and the
// latest transcripts per tenant, in memory only.
type Memory struct {
	mu          sync.Mutex
	vectors     map[string][]float32 // "<tenant>/<model>/<hash>"
	transcripts map[string][]Transcript
}

// transcriptsKept bounds what one tenant holds in memory.
const transcriptsKept = 500

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

// Transcripts are a run's calls, newest first (every call's, for no run).
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
	m.transcripts[tenant] = slices.DeleteFunc(m.transcripts[tenant], func(x Transcript) bool { return x.At.Before(before) })
}
