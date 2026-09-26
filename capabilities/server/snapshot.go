package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	Apps       map[string]json.RawMessage `json:"apps"`
	Records    map[string][]recordState   `json:"records"`
	Audit      []AuditEntry               `json:"audit"`
	Deliveries []Delivery                 `json:"deliveries"`
	Acted      int                        `json:"acted"`
	Bindings   map[string]string          `json:"bindings"` // protocol → provider app
	Works      json.RawMessage            `json:"works"`
	Queues     map[string][]string        `json:"queues"` // subscriber → task IDs, head first
	Failed     []string                   `json:"failed"`
	Tasks      []taskState                `json:"tasks"` // every delivery task, by ID
	Jobs       []Task                     `json:"jobs"`
	Connectors json.RawMessage            `json:"connectors"`
	Marks      []kernel.ConnectorMark     `json:"marks"`
	LastError  map[string]ConnectorError  `json:"lastError"`
	Notices    []platform.Notification    `json:"notices"`
	NoticeSeq  int                        `json:"noticeSeq"`
	Settings   map[string]string          `json:"settings"`
	Endpoints  []*Endpoint                `json:"endpoints"`
	Outbound   []effectState              `json:"outbound"`
	Sequences  map[string]int             `json:"sequences,omitempty"`
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
			raw, err := json.Marshal(r.value.Interface())
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
	t.auditMu.Lock()
	s.Audit = slices.Clone(t.audit)
	t.auditMu.Unlock()
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	s.Deliveries, s.Acted, s.Failed = slices.Clone(t.deliveries), t.acted, []string{}
	for protocol, b := range t.bindings {
		s.Bindings[protocol] = b.provider.Manifest().ID
	}
	var err error
	if s.Works, err = platform.Protos(t.works.All()); err != nil {
		return tenantState{}, nil, 0, err
	}
	tasks := map[string]*Task{}
	for app, queue := range t.queues {
		for _, x := range queue {
			s.Queues[app], tasks[x.ID] = append(s.Queues[app], x.ID), x
		}
	}
	for _, x := range t.failed {
		s.Failed, tasks[x.ID] = append(s.Failed, x.ID), x
	}
	for _, id := range slices.Sorted(maps.Keys(tasks)) {
		x := tasks[id]
		state := taskState{Task: *x, Since: x.since}
		if x.event != nil {
			state.EventApp, state.Hops, state.Changed = x.event.App, x.event.hops, x.event.Changed
			if state.Event, err = platform.Protos([]*pb.ChangeRecord{x.event.Record}); err != nil {
				return tenantState{}, nil, 0, err
			}
		}
		s.Tasks = append(s.Tasks, state)
	}
	for _, x := range t.jobs {
		s.Jobs = append(s.Jobs, *x)
	}
	descriptors, marks := t.connectors.State()
	if s.Connectors, err = platform.Protos(descriptors); err != nil {
		return tenantState{}, nil, 0, err
	}
	s.Marks, s.LastError = marks, maps.Clone(t.lastError)
	s.Notices, s.NoticeSeq, s.Settings, s.Endpoints = slices.Clone(t.notices), t.noticeSeq, maps.Clone(t.settings), t.endpoints
	t.seqMu.Lock()
	s.Sequences = maps.Clone(t.sequences)
	t.seqMu.Unlock()
	for _, x := range t.outbound {
		s.Outbound = append(s.Outbound, effectState{Effect: x.Effect, Since: x.since})
	}
	return s, rows, position(), nil
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
	t.records.mu.Lock()
	for typ, rows := range s.Records {
		et := t.records.types[typ]
		if et == nil {
			t.records.mu.Unlock()
			return fmt.Errorf("tenant %s: no entity type %s", t.ID, typ)
		}
		et.rows = map[string]*row{}
		for _, r := range rows {
			v := reflect.New(et.info.Go).Elem()
			if err := json.Unmarshal(r.Value, v.Addr().Interface()); err != nil {
				t.records.mu.Unlock()
				return err
			}
			et.rows[recordOf(v).ID] = &row{value: v, history: r.History}
		}
	}
	t.records.mu.Unlock()
	t.auditMu.Lock()
	t.audit = s.Audit
	t.auditMu.Unlock()
	for protocol, provider := range s.Bindings {
		if !t.rebind(protocol, provider) {
			return fmt.Errorf("tenant %s: %s is not a provider of %s", t.ID, provider, protocol)
		}
	}
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
	t.deliveries, t.acted = s.Deliveries, s.Acted
	t.works.Restore(works)
	tasks := map[string]*Task{}
	for _, x := range s.Tasks {
		task := x.Task
		task.since = x.Since
		if x.Event != nil {
			records, err := platform.Unprotos[*pb.ChangeRecord](x.Event)
			if err != nil || len(records) != 1 {
				return fmt.Errorf("tenant %s: task %s: bad event", t.ID, x.ID)
			}
			task.event = &caused{Event: platform.Event{App: x.EventApp, Record: records[0], Changed: x.Changed}, hops: x.Hops}
		}
		tasks[x.ID] = &task
	}
	t.queues, t.failed = map[string][]*Task{}, nil
	for app, ids := range s.Queues {
		for _, id := range ids {
			t.queues[app] = append(t.queues[app], tasks[id])
		}
	}
	for _, id := range s.Failed {
		t.failed = append(t.failed, tasks[id])
	}
	for _, saved := range s.Jobs { // the jobs come from the manifests; their runs from the snapshot
		if i := slices.IndexFunc(t.jobs, func(x *Task) bool { return x.ID == saved.ID }); i >= 0 {
			job := t.jobs[i].job
			*t.jobs[i] = saved
			t.jobs[i].job = job
		}
	}
	t.connectors.Restore(descriptors, s.Marks)
	t.descriptors = map[string]*pb.ConnectorDescriptor{}
	for _, d := range descriptors {
		if d.GetTenantId() == t.ID {
			t.descriptors[d.GetConnectorId()] = d
		}
	}
	t.lastError = s.LastError
	if t.lastError == nil {
		t.lastError = map[string]ConnectorError{}
	}
	t.notices, t.noticeSeq, t.settings, t.endpoints = s.Notices, s.NoticeSeq, s.Settings, s.Endpoints
	if t.settings == nil {
		t.settings = map[string]string{}
	}
	t.seqMu.Lock()
	t.sequences = s.Sequences
	if t.sequences == nil {
		t.sequences = map[string]int{}
	}
	t.seqMu.Unlock()
	t.outbound = nil
	for _, x := range s.Outbound {
		t.outbound = append(t.outbound, &effect{Effect: x.Effect, since: x.Since})
	}
	return nil
}
