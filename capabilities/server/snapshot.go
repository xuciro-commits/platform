package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A snapshot is a tenant's state at a journal position (ADR-0019 D6): what the
// host keeps (records and their history, owned work, connectors, notices,
// settings, endpoints and effects, bindings, the audit) and each app's own,
// through platform.Snapshotter. Restored into a tenant composed as at start-up,
// then given the entries after that position, it equals a full replay;
// CheckReplay holds every composition to that. A snapshot is only a shortcut:
// the journal stays the truth, and a snapshot of other code is never used.
type tenantState struct {
	Apps              map[string]json.RawMessage   `json:"apps"`
	Records           map[string][]recordState     `json:"records"`
	Audit             []AuditEntry                 `json:"audit"`
	Refusals          map[string]refusedResult     `json:"refusals,omitempty"`
	AcceptedAnswers   map[string]json.RawMessage   `json:"acceptedAnswers,omitempty"`
	AcceptedInputs    map[string]json.RawMessage   `json:"acceptedInputs,omitempty"`
	ReleaseCandidates map[string]json.RawMessage   `json:"releaseCandidates,omitempty"`
	ReleaseApplied    map[string]string            `json:"releaseApplied,omitempty"`
	ActiveRelease     string                       `json:"activeRelease,omitempty"`
	Deliveries        []Delivery                   `json:"deliveries"`
	Acted             int                          `json:"acted"`
	Bindings          map[string]string            `json:"bindings"` // protocol → provider app
	Works             json.RawMessage              `json:"works"`
	Queues            map[string][]string          `json:"queues"` // subscriber → task IDs, head first
	Failed            []string                     `json:"failed"`
	Tasks             []taskState                  `json:"tasks"` // every delivery task, by ID
	Jobs              []Task                       `json:"jobs"`
	Connectors        json.RawMessage              `json:"connectors"`
	Marks             []kernel.ConnectorMark       `json:"marks"`
	LastError         map[string]ConnectorError    `json:"lastError"`
	Notices           []platform.Notification      `json:"notices"`
	NoticeSeq         int                          `json:"noticeSeq"`
	Settings          map[string]string            `json:"settings"`
	Words             map[string]map[string]string `json:"words,omitempty"` // the tenant's translations (ADR-0083)
	Endpoints         []*Endpoint                  `json:"endpoints"`
	Outbound          []effectState                `json:"outbound"`
	// The host console's lifecycle and support sessions (ADR-0047 §6.5) travel
	// with the tenant: a restart keeps a suspension and its authorized sessions.
	HostLifecycle  string                           `json:"hostLifecycle,omitempty"`
	Support        []SupportGrant                   `json:"support,omitempty"`
	Sealed         map[string]SealedArtifact        `json:"sealed,omitempty"`
	ReleaseOrigins map[string]ReleaseOrigin         `json:"releaseOrigins,omitempty"`
	Migrations     []MigrationManifest              `json:"migrations,omitempty"`
	Composites     map[string]string                `json:"composites,omitempty"`
	Staged         map[string]platform.StagedResult `json:"staged,omitempty"`
	Sequences      map[string]int                   `json:"sequences,omitempty"`
}

type recordState struct {
	Value   json.RawMessage `json:"value"`
	History []RecordChange  `json:"history"`
}

type taskState struct {
	Task
	Since    int             `json:"since,omitempty"`
	EventApp string          `json:"eventApp,omitempty"` // the event it delivers
	Event    json.RawMessage `json:"event,omitempty"`
	Hops     int             `json:"hops,omitempty"`
	Changed  []string        `json:"changed,omitempty"` // the records its decision put
	Plan     *acceptedEvent  `json:"plan,omitempty"`    // accepted-result delivery routing
}

type effectState struct {
	platform.Effect
	Since int `json:"since,omitempty"`
}

// Snapshot saves the tenant's state between inputs; the caller's position
// function runs under the same lock, so the journal position matches the state.
// Decisions wait only while the state is captured: records never change once
// stored (a put stores a new row), so they are encoded after the lock.
func (t *Tenant) Snapshot(position func() int64) (json.RawMessage, int64, error) {
	s, rows, seq, err := t.capture(position)
	if err != nil {
		return nil, 0, err
	}
	for typ, list := range rows {
		saved := make([]recordState, len(list))
		for i, r := range list {
			raw, err := r.image()
			if err != nil {
				return nil, 0, err
			}
			saved[i] = recordState{Value: raw, History: r.history}
		}
		slices.SortFunc(saved, func(a, b recordState) int { return strings.Compare(string(a.Value), string(b.Value)) })
		s.Records[typ] = saved
	}
	raw, err := json.Marshal(s)
	return raw, seq, err
}

// capture takes everything but the records' encoding under the tenant's locks.
func (t *Tenant) capture(position func() int64) (tenantState, map[string][]*row, int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return tenantState{}, nil, 0, fmt.Errorf("tenant %s is quarantined", t.ID)
	}
	if len(t.events) > 0 {
		return tenantState{}, nil, 0, fmt.Errorf("tenant %s: events pending", t.ID)
	}
	s := tenantState{Apps: map[string]json.RawMessage{}, Records: map[string][]recordState{}, Bindings: map[string]string{}, Queues: map[string][]string{}}
	for _, a := range t.apps {
		sn, ok := a.(platform.Snapshotter)
		if !ok {
			return tenantState{}, nil, 0, fmt.Errorf("tenant %s: app %s cannot be snapshotted", t.ID, a.Manifest().ID)
		}
		raw, err := sn.Snapshot()
		if err != nil {
			return tenantState{}, nil, 0, fmt.Errorf("tenant %s: app %s: %v", t.ID, a.Manifest().ID, err)
		}
		s.Apps[a.Manifest().ID] = raw
	}
	rows := map[string][]*row{}
	t.records.mu.Lock()
	for typ, et := range t.records.types {
		rows[typ] = slices.Collect(maps.Values(et.rows))
	}
	t.records.mu.Unlock()
	t.audit.snapshot(&s)
	t.committed.snapshot(&s)
	t.releases.snapshot(&s)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	s.Acted, s.Failed = t.acted, []string{}
	for protocol, b := range t.bindings {
		s.Bindings[protocol] = b.provider.Manifest().ID
	}
	var err error
	if s.Works, err = platform.Protos(t.works.All()); err != nil {
		return tenantState{}, nil, 0, err
	}
	if err := t.work.snapshot(&s); err != nil {
		return tenantState{}, nil, 0, err
	}
	descriptors, marks := t.connectors.kernel.State()
	if s.Connectors, err = platform.Protos(descriptors); err != nil {
		return tenantState{}, nil, 0, err
	}
	s.Marks = marks
	t.connectors.snapshot(&s)
	s.Notices, s.NoticeSeq = t.notices.state()
	s.Settings, s.Endpoints, s.Words = t.settings.clone(), t.endpoints, t.words.clone()
	s.Sequences = t.sequences.clone()
	for _, x := range t.outbound {
		s.Outbound = append(s.Outbound, effectState{Effect: x.Effect, Since: x.since})
	}
	t.console.snapshot(&s)
	s.Staged = t.staged.snapshot()
	return s, rows, position(), nil
}

// restoreRecords restores the records of every type declared now, and hands
// back those of types nobody has declared yet: a tenant's own objects, which
// their definitions install (ADR-0034 D4).
func (t *Tenant) restoreRecords(saved map[string][]recordState) (map[string][]recordState, error) {
	held := map[string][]recordState{}
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	for typ, rows := range saved {
		et := t.records.types[typ]
		if et == nil {
			held[typ] = rows
			continue
		}
		et.rows = map[string]*row{}
		for _, r := range rows {
			v := reflect.New(et.info.Go).Elem()
			if err := json.Unmarshal(r.Value, v.Addr().Interface()); err != nil {
				return nil, err
			}
			emptyLists(v) // a snapshot of older code may hold null lists
			recovered := &row{value: v, history: r.History}
			recovered.retainOriginal(r.Value)
			et.rows[recordOf(v).ID] = recovered
		}
		t.records.generation++
	}
	return held, nil
}

// Restore loads a snapshot into a tenant composed as at start-up, before any
// entry: its apps and connectors are the same ones the snapshot was taken of.
func (t *Tenant) Restore(raw json.RawMessage) error {
	var s tenantState
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(s.Apps) != len(t.apps) {
		return fmt.Errorf("tenant %s: the snapshot has %d apps, the tenant %d", t.ID, len(s.Apps), len(t.apps))
	}
	for _, a := range t.apps {
		sn, ok := a.(platform.Snapshotter)
		raw, saved := s.Apps[a.Manifest().ID]
		if !ok || !saved {
			return fmt.Errorf("tenant %s: app %s has no snapshot", t.ID, a.Manifest().ID)
		}
		if err := sn.Restore(raw); err != nil {
			return fmt.Errorf("tenant %s: app %s: %v", t.ID, a.Manifest().ID, err)
		}
	}
	// First the records of the types code declares, then the definitions a
	// tenant authored — which install their own types — then the records of
	// those (ADR-0034 D4).
	held, err := t.restoreRecords(s.Records)
	if err != nil {
		return err
	}
	if err := t.restoreDefinitions(&s, held); err != nil {
		return err
	}
	if err := t.records.validateLinkConstraints(); err != nil {
		return err
	}
	t.audit.restore(&s)
	t.console.restore(&s)
	if s.Staged != nil {
		t.staged.restore(s.Staged)
	}
	if err := t.verifyCommitted(&s); err != nil {
		return err
	}
	t.releases.restore(&s)
	for protocol, provider := range s.Bindings {
		if !t.rebind(protocol, provider) {
			return fmt.Errorf("tenant %s: %s is not a provider of %s", t.ID, provider, protocol)
		}
	}
	return t.restoreOperations(&s)
}

// restoreDefinitions reinstalls the tenant-authored definitions when the
// snapshot's published assets or held records need them, then restores the
// records of the types they install (ADR-0034 D4).
func (t *Tenant) restoreDefinitions(s *tenantState, held map[string][]recordState) error {
	needsDefinitions := len(held) > 0
	for typ, kind := range map[string]platform.AssetKind{build.PropertyTypeType: platform.AssetPropertyType, build.LinkTypeType: platform.AssetLinkType, build.QueryType: platform.AssetQuery, build.ObjectType: platform.AssetObject, build.PageType: platform.AssetPage, build.AppType: platform.AssetApp, build.ProcessType: platform.AssetFlow, build.FunctionType: platform.AssetFunction, build.CodeType: platform.AssetCompute} {
		for _, row := range s.Records[typ] {
			var meta struct {
				Published string `json:"published"`
			}
			if json.Unmarshal(row.Value, &meta) != nil || meta.Published == "" {
				continue
			}
			var saved struct {
				Name    string `json:"name"`
				Version int    `json:"version"`
			}
			if json.Unmarshal([]byte(meta.Published), &saved) != nil {
				continue
			}
			name := saved.Name
			if kind == platform.AssetObject || kind == platform.AssetFlow {
				name = build.TypeOf(name)
			}
			if kind == platform.AssetFlow {
				var state struct {
					Name    string `json:"name"`
					Version int    `json:"version"`
				}
				json.Unmarshal([]byte(meta.Published), &state)
				owner, ok := t.procs.(interface {
					HasDefinition(string, string, int) bool
				})
				needsDefinitions = needsDefinitions || !ok || !owner.HasDefinition(build.ID, state.Name, state.Version)
				continue
			}

			ref := platform.AssetRef{App: build.ID, Kind: kind, Name: name}
			needsDefinitions = needsDefinitions || !slices.ContainsFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Source == "tenant" })
		}
	}
	if needsDefinitions {
		if err := t.reinstall(); err != nil {
			return err
		}
	}
	if len(held) > 0 {
		left, err := t.restoreRecords(held)
		if err != nil {
			return err
		}
		if len(left) > 0 {
			return fmt.Errorf("tenant %s: no entity type %s", t.ID, slices.Sorted(maps.Keys(left))[0])
		}
	}
	return nil
}

// verifyCommitted checks that every committed answer in the snapshot is one
// this tenant and its apps still recognise, before it is restored.
func (t *Tenant) verifyCommitted(s *tenantState) error {
	for key, result := range s.Refusals {
		raw, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("tenant %s: refusal snapshot %s: %w", t.ID, key, err)
		}
		decoded, sub, err := decodeRefusedResult(raw)
		if err != nil || decoded.Tenant != t.ID || key != decoded.App+"/"+sub.GetIdempotencyKey() {
			return fmt.Errorf("tenant %s: refusal snapshot %s is incompatible: %v", t.ID, key, err)
		}
	}
	t.committed.restore(s)
	for key, raw := range s.AcceptedAnswers {
		if strings.HasPrefix(key, "release:") {
			result, err := decodeAcceptedRelease(raw)
			if err != nil || result.Version != 3 || result.Tenant != t.ID || key != "release:"+result.Key || s.ReleaseApplied[result.Key] != result.Digest || !bytes.Equal(s.ReleaseCandidates[result.CandidateID], result.Bytes) {
				return fmt.Errorf("tenant %s: release answer snapshot %s is incompatible", t.ID, key)
			}
			continue
		}
		result, receipt, err := decodeAcceptedBatch(raw)
		sub, subErr := batchSubmission(result, receipt)
		if err != nil || subErr != nil || result.Tenant != t.ID ||
			len(result.Submission) == 0 || key != result.App+"/"+sub.GetIdempotencyKey() {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s is incompatible", t.ID, key)
		}
		owner, ok := t.app(receipt.GetSubmission().GetAuthority()).(platform.ResultApp)
		if !ok || !proto.Equal(owner.AcceptedLedger().AcceptedFor(t.ID, receipt.GetSubmission().GetIdempotencyKey()), receipt) {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s has no receipt", t.ID, key)
		}
		if _, refused := t.committed.refusals[key]; refused {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s reuses a refused key", t.ID, key)
		}
	}
	for key, raw := range s.AcceptedInputs {
		result, err := decodeAcceptedInput(raw)
		owner, ok := t.app(result.App).(platform.AcceptedInputApp)
		if err != nil || !ok || result.Tenant != t.ID ||
			key != result.App+"/"+result.Key || !slices.Contains(owner.AcceptedInputs(), result.Name) {
			return fmt.Errorf("tenant %s: accepted input snapshot %s is incompatible: %v", t.ID, key, err)
		}
	}
	for id, raw := range s.ReleaseCandidates {
		if _, err := platform.ReadCandidate(id, raw); err != nil {
			return fmt.Errorf("tenant %s: release snapshot %s is incompatible: %w", t.ID, id, err)
		}
	}
	if s.ActiveRelease != "" && s.ReleaseCandidates[s.ActiveRelease] == nil {
		return fmt.Errorf("tenant %s: active release snapshot has no saved candidate", t.ID)
	}
	return nil
}

// restoreOperations restores what the runner shares with reads: works, tasks,
// queues, jobs, connectors, notices, settings, endpoints, sequences, outbound.
func (t *Tenant) restoreOperations(s *tenantState) error {
	works, err := platform.Unprotos[*pb.Work](s.Works)
	if err != nil {
		return err
	}
	descriptors, err := platform.Unprotos[*pb.ConnectorDescriptor](s.Connectors)
	if err != nil {
		return err
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.acted = s.Acted
	t.works.Restore(works)
	if err := t.work.restore(t.ID, s); err != nil {
		return err
	}
	t.connectors.restore(t.ID, descriptors, s)
	t.notices.restore(s.Notices, s.NoticeSeq)
	t.settings.restore(s.Settings)
	t.words.restore(s.Words)
	t.i18n.reset()
	t.endpoints = s.Endpoints
	t.sequences.restore(s.Sequences)
	t.outbound = nil
	for _, x := range s.Outbound {
		t.outbound = append(t.outbound, &effect{Effect: x.Effect, since: x.Since})
	}
	return nil
}
