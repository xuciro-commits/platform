package journal

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type File struct {
	dir      string
	identify Identify
	file     *os.File

	mu          sync.Mutex
	locks       map[string]*sync.Mutex
	entries     map[string][]Entry
	next        map[string]int64
	results     map[string]map[string]string
	writeFault  error
	vectors     map[string]map[string][]float32
	transcripts []Transcript
}

type fileLine struct {
	Tenant string `json:"tenant"`
	Seq    int64  `json:"seq"`
	Entry  Entry  `json:"entry"`
}

type vectorLine struct {
	Tenant string `json:"tenant"`
	Model  string `json:"model"`
	Hash   string `json:"hash"`
	Vector []byte `json:"vector,omitempty"`
}

type transcriptLine struct {
	Tenant string `json:"tenant"`
	Transcript
}

func OpenFile(dir string, identify Identify) (*File, error) {
	for _, sub := range []string{"", "snapshots", "derived"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, fmt.Errorf("journal: %w", err)
		}
	}
	j := &File{dir: dir, identify: identify, locks: map[string]*sync.Mutex{}, entries: map[string][]Entry{}, next: map[string]int64{},
		results: map[string]map[string]string{}, vectors: map[string]map[string][]float32{}}
	if err := j.loadEntries(); err != nil {
		return nil, err
	}
	if err := j.loadVectors(); err != nil {
		return nil, err
	}
	if err := j.loadTranscripts(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(j.entriesPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	j.file = file
	return j, nil
}

func (j *File) entriesPath() string { return filepath.Join(j.dir, "journal.jsonl") }

func (j *File) loadEntries() error {
	file, err := os.Open(j.entriesPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 1<<20), 64<<20)
	number := 0
	for lines.Scan() {
		number++
		if len(lines.Bytes()) == 0 {
			continue
		}
		var line fileLine
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			return fmt.Errorf("%w: %s line %d: %v", ErrTenantJournal, j.entriesPath(), number, err)
		}
		last := int64(len(j.entries[line.Tenant]))
		if line.Seq != last+1 {
			return fmt.Errorf("%w: tenant %s expected entry %d, found %d", ErrTenantJournal, line.Tenant, last+1, line.Seq)
		}
		line.Entry.At = line.Entry.At.UTC()
		j.entries[line.Tenant] = append(j.entries[line.Tenant], line.Entry)
		j.next[line.Tenant] = line.Seq + 1
		if line.Entry.Kind == "accepted-result" {
			identity, err := j.identify(line.Entry.Body)
			if err != nil {
				return fmt.Errorf("%w: tenant %s entry %d: %v", ErrTenantJournal, line.Tenant, line.Seq, err)
			}
			j.index(line.Tenant, identity.App, identity.Scope, identity.Key, identity.Hash)
		}
	}
	if err := lines.Err(); err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	return nil
}

func (j *File) index(tenant, app, scope, key, hash string) {
	if j.results[tenant] == nil {
		j.results[tenant] = map[string]string{}
	}
	j.results[tenant][app+"|"+scope+"|"+key] = hash
}

func (j *File) loadVectors() error {
	file, err := os.Open(filepath.Join(j.dir, "derived", "vectors.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for lines.Scan() {
		var line vectorLine
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			return fmt.Errorf("journal: vectors: %v", err)
		}
		j.putVector(line.Tenant, line.Model, line.Hash, DecodeVector(line.Vector))
	}
	return lines.Err()
}

func (j *File) loadTranscripts() error {
	file, err := os.Open(filepath.Join(j.dir, "derived", "transcripts.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for lines.Scan() {
		var line transcriptLine
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			return fmt.Errorf("journal: transcripts: %v", err)
		}
		line.Transcript.Tenant = line.Tenant
		j.transcripts = append(j.transcripts, line.Transcript)
	}
	return lines.Err()
}

func (j *File) lock(tenant string) *sync.Mutex {
	j.mu.Lock()
	defer j.mu.Unlock()
	lock := j.locks[tenant]
	if lock == nil {
		lock = &sync.Mutex{}
		j.locks[tenant] = lock
	}
	return lock
}

func (j *File) Entries(_ context.Context, tenant string, after int64) ([]Entry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	all := j.entries[tenant]
	if after > int64(len(all)) {
		return nil, fmt.Errorf("%w: tenant %s expected entry %d, the journal ends at %d", ErrTenantJournal, tenant, after+1, len(all))
	}
	out := append([]Entry{}, all[after:]...)
	j.next[tenant] = int64(len(all)) + 1
	return out, nil
}

func (j *File) Position(tenant string) int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.next[tenant] - 1
}

func (j *File) Append(_ context.Context, tenant string, e Entry) error {
	lock := j.lock(tenant)
	lock.Lock()
	defer lock.Unlock()
	return j.appendLocked(tenant, e)
}

func (j *File) appendLocked(tenant string, e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.writeFault != nil {
		return j.writeFault
	}
	seq, read := j.next[tenant]
	if !read {
		return fmt.Errorf("journal: append to %s before reading its entries", tenant)
	}
	line := fileLine{Tenant: tenant, Seq: seq, Entry: e}
	raw, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	if n, err := j.file.Write(append(raw, '\n')); err != nil {
		if n > 0 {
			j.writeFault = fmt.Errorf("%w: partial journal append: %v", ErrCommitUnknown, err)
			return j.writeFault
		}
		return fmt.Errorf("journal: %w", err)
	}
	if err := j.file.Sync(); err != nil {
		j.writeFault = fmt.Errorf("%w: sync journal append: %v", ErrCommitUnknown, err)
		return j.writeFault
	}
	var stored fileLine
	if err := json.Unmarshal(raw, &stored); err != nil {
		return fmt.Errorf("%w: decode committed entry: %v", ErrCommitUnknown, err)
	}
	stored.Entry.At = stored.Entry.At.UTC()
	j.entries[tenant] = append(j.entries[tenant], stored.Entry)
	j.next[tenant] = seq + 1
	if stored.Entry.Kind == "accepted-result" {
		if identity, err := j.identify(stored.Entry.Body); err == nil {
			j.index(tenant, identity.App, identity.Scope, identity.Key, identity.Hash)
		}
	}
	return nil
}

func (j *File) AppendAccepted(_ context.Context, tenant string, e Entry, key, hash string) ([]byte, error) {
	identity, err := j.identify(e.Body)
	if err != nil || e.Kind != "accepted-result" || identity.Tenant != tenant || identity.App != e.App || identity.Key != key {
		return nil, fmt.Errorf("journal: accepted input identity differs: %v", err)
	}
	if identity.Hash != hash {
		return nil, ErrAcceptedConflict
	}
	lock := j.lock(tenant)
	lock.Lock()
	defer lock.Unlock()
	j.mu.Lock()
	priorHash, known := j.results[tenant][identity.App+"|"+identity.Scope+"|"+key]
	found := j.findAccepted(tenant, identity.App, identity.Scope, key)
	j.mu.Unlock()
	if known {
		if priorHash != hash {
			return nil, ErrAcceptedConflict
		}
		return found, nil
	}
	if err := j.appendLocked(tenant, e); err != nil {
		return nil, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.findAccepted(tenant, identity.App, identity.Scope, key), nil
}

func (j *File) findAccepted(tenant, app, scope, key string) []byte {
	for i := len(j.entries[tenant]) - 1; i >= 0; i-- {
		e := j.entries[tenant][i]
		if e.Kind != "accepted-result" || e.App != app {
			continue
		}
		identity, err := j.identify(e.Body)
		if err == nil && identity.App == app && identity.Scope == scope && identity.Key == key {
			return e.Body
		}
	}
	return nil
}

func (j *File) SaveSnapshot(_ context.Context, tenant string, seq int64, code string, state []byte) error {
	packed, err := packSnapshot(state)
	if err != nil {
		return err
	}
	name := snapshotName(tenant, seq, code)
	if err := WriteFileAtomic(filepath.Join(j.dir, "snapshots", name), packed); err != nil {
		return err
	}
	return j.keepTwoSnapshots(tenant)
}

func (j *File) RepairSnapshot(_ context.Context, tenant string, seq int64, code string, state []byte) error {
	packed, err := packSnapshot(state)
	if err != nil {
		return err
	}
	for _, name := range j.snapshotsOf(tenant) {
		if err := os.Remove(filepath.Join(j.dir, "snapshots", name)); err != nil {
			return fmt.Errorf("journal: %w", err)
		}
	}
	return WriteFileAtomic(filepath.Join(j.dir, "snapshots", snapshotName(tenant, seq, code)), packed)
}

func (j *File) Snapshot(_ context.Context, tenant, code string) (int64, []byte, bool, error) {
	names := j.snapshotsOf(tenant)
	for i := len(names) - 1; i >= 0; i-- {
		_, snapshotSeq, fileCode, ok := parseSnapshotName(names[i])
		if !ok || fileCode != code {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(j.dir, "snapshots", names[i]))
		if err != nil {
			return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", ErrTenantSnapshot, tenant, snapshotSeq, err)
		}
		state, err := unpackSnapshot(raw)
		if err != nil {
			return 0, nil, false, fmt.Errorf("%w: tenant %s at %d: %v", ErrTenantSnapshot, tenant, snapshotSeq, err)
		}
		return snapshotSeq, state, true, nil
	}
	return 0, nil, false, nil
}

func (j *File) snapshotsOf(tenant string) []string {
	entries, err := os.ReadDir(filepath.Join(j.dir, "snapshots"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if name, _, _, ok := parseSnapshotName(e.Name()); ok && name == tenant && !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(a, b int) bool { return snapshotOrder(names[a]) < snapshotOrder(names[b]) })
	return names
}

func (j *File) keepTwoSnapshots(tenant string) error {
	names := j.snapshotsOf(tenant)
	for len(names) > 2 {
		if err := os.Remove(filepath.Join(j.dir, "snapshots", names[0])); err != nil {
			return fmt.Errorf("journal: %w", err)
		}
		names = names[1:]
	}
	return nil
}

func saveName(tenant string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, tenant)
	return strings.Trim(filepath.Base(name), ".")
}

func snapshotName(tenant string, seq int64, code string) string {
	return fmt.Sprintf("%s.%020d.%s.gz", saveName(tenant), seq, code)
}

func parseSnapshotName(name string) (tenant string, seq int64, code string, ok bool) {
	if !strings.HasSuffix(name, ".gz") {
		return "", 0, "", false
	}
	parts := strings.Split(strings.TrimSuffix(name, ".gz"), ".")
	if len(parts) != 3 {
		return "", 0, "", false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &seq); err != nil {
		return "", 0, "", false
	}
	return parts[0], seq, parts[2], true
}

func snapshotOrder(name string) int64 {
	_, seq, _, _ := parseSnapshotName(name)
	return seq
}

func WriteFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("journal: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	return os.Rename(tmp, path)
}

func unpackSnapshot(packed []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func (j *File) Close() {
	if j.file != nil {
		j.file.Sync()
		j.file.Close()
	}
}

func (j *File) putVector(tenant, model, hash string, vector []float32) {
	key := tenant + "|" + model
	if j.vectors[key] == nil {
		j.vectors[key] = map[string][]float32{}
	}
	j.vectors[key][hash] = vector
}

func (j *File) Vectors(tenant, model string, hashes []string) map[string][]float32 {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string][]float32{}
	for _, hash := range hashes {
		if v, ok := j.vectors[tenant+"|"+model][hash]; ok {
			out[hash] = v
		}
	}
	return out
}

func (j *File) SaveVectors(tenant, model string, vectors map[string][]float32) {
	path := filepath.Join(j.dir, "derived", "vectors.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Printf("save vectors: %v", err)
		return
	}
	defer file.Close()
	j.mu.Lock()
	defer j.mu.Unlock()
	encoder := json.NewEncoder(file)
	for hash, vector := range vectors {
		j.putVector(tenant, model, hash, vector)
		if err := encoder.Encode(vectorLine{Tenant: tenant, Model: model, Hash: hash, Vector: EncodeVector(vector)}); err != nil {
			log.Printf("save vectors: %v", err)
			return
		}
	}
	file.Sync()
}

func (j *File) SaveTranscript(x Transcript) {
	path := filepath.Join(j.dir, "derived", "transcripts.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Printf("transcript: %v", err)
		return
	}
	defer file.Close()
	raw, err := json.Marshal(transcriptLine{Tenant: x.Tenant, Transcript: x})
	if err != nil {
		log.Printf("transcript: %v", err)
		return
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		log.Printf("transcript: %v", err)
		return
	}
	file.Sync()
	j.mu.Lock()
	j.transcripts = append(j.transcripts, x)
	j.mu.Unlock()
}

func (j *File) Transcripts(tenant, run string, limit int) []Transcript {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := []Transcript{}
	for i := len(j.transcripts) - 1; i >= 0 && len(out) < limit; i-- {
		x := j.transcripts[i]
		if x.Tenant == tenant && (run == "" || x.Run == run) {
			out = append(out, x)
		}
	}
	return out
}

func (j *File) PurgeTranscripts(tenant string, before time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	kept := make([]Transcript, 0, len(j.transcripts))
	for _, x := range j.transcripts {
		if x.Tenant == tenant && x.At.Before(before) {
			continue
		}
		kept = append(kept, x)
	}
	j.transcripts = kept
	var out []byte
	for _, x := range kept {
		raw, err := json.Marshal(transcriptLine{Tenant: x.Tenant, Transcript: x})
		if err != nil {
			log.Printf("purge transcripts: %v", err)
			return
		}
		out = append(append(out, raw...), '\n')
	}
	path := filepath.Join(j.dir, "derived", "transcripts.jsonl")
	if len(kept) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("purge transcripts: %v", err)
		}
		return
	}
	if err := WriteFileAtomic(path, out); err != nil {
		log.Printf("purge transcripts: %v", err)
	}
}
