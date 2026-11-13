package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

type TenantImage[Job, Endpoint any] struct {
	Runtime             *SQLCheckpoint                          `json:"runtime,omitempty"`
	Checkpoint          *SnapshotCheckpoint                     `json:"checkpoint,omitempty"`
	DecisionDefinitions map[string]*platform.DecisionDefinition `json:"decisionDefinitions,omitempty"`
	SubmissionSchemas   map[string]platform.SchemaVersion       `json:"submissionSchemas,omitempty"`
	Apps                map[string]json.RawMessage              `json:"apps"`
	Records             map[string][]records.StoredState        `json:"records"`
	Facts               json.RawMessage                         `json:"facts,omitempty"`
	Identities          *kernel.IdentityState                   `json:"identities,omitempty"`
	Audit               []platform.AuditEntry                   `json:"audit"`
	Refusals            map[string]acceptedformat.Refusal       `json:"refusals,omitempty"`
	AcceptedAnswers     map[string]json.RawMessage              `json:"acceptedAnswers,omitempty"`
	AcceptedInputs      map[string]json.RawMessage              `json:"acceptedInputs,omitempty"`
	ReleaseCandidates   map[string]json.RawMessage              `json:"releaseCandidates,omitempty"`
	ReleaseRequests     map[string]string                       `json:"releaseRequests,omitempty"`
	ReleaseApplied      map[string]string                       `json:"releaseApplied,omitempty"`
	ActiveRelease       string                                  `json:"activeRelease,omitempty"`
	Deliveries          []platform.Delivery                     `json:"deliveries"`
	Acted               int                                     `json:"acted"`
	Bindings            map[string]string                       `json:"bindings"`
	Works               json.RawMessage                         `json:"works"`
	Queues              map[string][]string                     `json:"queues"`
	Failed              []string                                `json:"failed"`
	Tasks               []platform.ScheduledTaskState           `json:"tasks"`
	Jobs                []Job                                   `json:"jobs"`
	Connectors          json.RawMessage                         `json:"connectors"`
	Marks               []kernel.ConnectorMark                  `json:"marks"`
	LastError           map[string]ConnectorError               `json:"lastError"`
	Notices             []platform.Notification                 `json:"notices"`
	NoticeSeq           int                                     `json:"noticeSeq"`
	Settings            map[string]string                       `json:"settings"`
	Words               map[string]map[string]string            `json:"words,omitempty"`
	Endpoints           []*Endpoint                             `json:"endpoints"`
	Outbound            []acceptedformat.EffectState            `json:"outbound"`
	HostRequests        map[string]platform.HostControlReceipt  `json:"hostRequests,omitempty"`
	HostLifecycle       string                                  `json:"hostLifecycle,omitempty"`
	RuntimeSelection    map[string]string                       `json:"runtimeSelection,omitempty"`
	DisabledOwners      []string                                `json:"disabledOwners,omitempty"`
	Support             []platform.SupportGrant                 `json:"support,omitempty"`
	Sealed              map[string]platform.SealedArtifact      `json:"sealed,omitempty"`
	ReleaseOrigins      map[string]platform.ReleaseOrigin       `json:"releaseOrigins,omitempty"`
	Migrations          []platform.MigrationManifest            `json:"migrations,omitempty"`
	CompositeAnswers    map[string]json.RawMessage              `json:"compositeAnswers,omitempty"`
	Composites          map[string]string                       `json:"composites,omitempty"`
	Staged              map[string]platform.StagedResult        `json:"staged,omitempty"`
	Sequences           map[string]int                          `json:"sequences,omitempty"`
}

type TenantSnapshots[Job, Endpoint any] struct {
	Compatibility func() (SnapshotCompatibility, error)
	Build         func() string
	Apps          []platform.App
	AppOf         func(string) platform.App
	Inputs        func(string) platform.AcceptedInputApp
	Quarantined   func() bool
	Pending       func() bool
	Scopes        func() bool
	Detach        func()
	Invalidate    func()
	CheckRecord   func(any) error
	Capture       func(image *TenantImage[Job, Endpoint]) (func(), error)
	Definitions   func(image *TenantImage[Job, Endpoint], held map[string][]records.StoredState) error
	Services      func(image *TenantImage[Job, Endpoint]) error
	Operations    func(image *TenantImage[Job, Endpoint]) error
}

func (c SnapshotCompatibility) StorageKey(code string, raw []byte) (string, error) {
	runtime, err := ReadCheckpoint(raw)
	if err != nil {
		return "", err
	}
	if runtime != nil {
		return c.RuntimeKey(code)
	}
	return c.Key(code)
}

func (h TenantSnapshots[Job, Endpoint]) Snapshot(q *Sequence, position func() int64) (json.RawMessage, int64, error) {
	compatibility, err := h.Compatibility()
	if err != nil {
		return nil, 0, err
	}
	var read persistence.StateReadView
	var ownsRead bool
	defer func() {
		if read != nil && ownsRead {
			_ = read.Close()
		}
	}()
	s, rows, seq, err := h.capture(q, position, true, func() (func(), error) {
		if q.Projection.Current().View() != nil {
			read = q.Projection.Current().View()
			if h.Scopes() {
				ownsRead = true
				return h.Detach, nil
			}
			return nil, nil
		}
		views, ok := q.Projection.Current().Backend().(persistence.StateReadViews)
		if !ok {
			return nil, nil
		}
		var err error
		read, err = views.BeginStateRead(context.Background(), q.ID, q.Projection.Current().Applied())
		if err != nil {
			return nil, err
		}
		ownsRead = true
		q.Projection.Current().Bind(read)
		return h.Detach, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if compatibility.Host != "" {
		s.Checkpoint = &SnapshotCheckpoint{SnapshotCompatibility: compatibility, Tenant: q.ID, Position: seq, Build: h.Build()}
	}
	for typ, list := range rows {
		saved := make([]records.StoredState, len(list))
		for i, r := range list {
			raw, err := r.Image()
			if err != nil {
				return nil, 0, err
			}
			head, err := r.HistoryDigest()
			if err != nil {
				return nil, 0, err
			}
			saved[i] = records.StoredState{HistoryHead: head, Value: raw, Format: r.Format, History: r.RecordHistory(), Projection: records.CloneProjection(r.Projection), Extraction: r.Extraction}
		}
		slices.SortFunc(saved, func(a, b records.StoredState) int { return strings.Compare(string(a.Value), string(b.Value)) })
		s.Records[typ] = saved
	}
	raw, err := json.Marshal(s)
	return raw, seq, err
}

func (h TenantSnapshots[Job, Endpoint]) SnapshotRuntime(q *Sequence, ctx context.Context, j persistence.Journals) (json.RawMessage, int64, error) {
	views, ok := j.(persistence.StateReadViews)
	if !ok || q.Projection.Current().Backend() == nil {
		return h.Snapshot(q, func() int64 { return j.Position(q.ID) })
	}
	compatibility, err := h.Compatibility()
	if err != nil {
		return nil, 0, err
	}
	var view persistence.StateReadView
	var viewErr error
	var ownsRead bool
	s, _, seq, err := h.capture(q, func() int64 {
		position := j.Position(q.ID)
		if q.Projection.Current().View() != nil {
			view = q.Projection.Current().View()
			if view.Position() != position {
				viewErr = fmt.Errorf("tenant %s: SQL snapshot view position differs", q.ID)
			}
			if h.Scopes() {
				ownsRead = true
				h.Detach()
			}
		} else {
			view, viewErr = views.BeginStateRead(ctx, q.ID, position)
			ownsRead = true
		}
		return position
	}, false, nil)
	if err != nil {
		return nil, 0, err
	}
	if viewErr != nil {
		if view != nil && ownsRead {
			_ = view.Close()
		}
		return nil, 0, viewErr
	}
	if view == nil {
		return nil, 0, fmt.Errorf("tenant %s: SQL snapshot read view unavailable", q.ID)
	}
	if ownsRead {
		defer view.Close()
	}
	digest, err := view.Checkpoint(ctx, q.ID)
	if err != nil {
		return nil, 0, err
	}
	s.Runtime = &SQLCheckpoint{Format: SQLCheckpointFormat, Position: seq, Records: digest}
	compatibility.Host = TenantSQLSnapshotFormat
	s.Checkpoint = &SnapshotCheckpoint{SnapshotCompatibility: compatibility, Tenant: q.ID, Position: seq, Build: h.Build()}
	raw, err := json.Marshal(s)
	return raw, seq, err
}

func (h TenantSnapshots[Job, Endpoint]) Checkpoint(q *Sequence, ctx context.Context, j persistence.Journals, code string) (int64, error) {
	state, seq, err := h.SnapshotRuntime(q, ctx, j)
	if err != nil {
		return 0, err
	}
	compatibility, err := h.Compatibility()
	if err != nil {
		return 0, err
	}
	key, err := compatibility.StorageKey(code, state)
	if err != nil {
		return 0, err
	}
	if err = SaveCheckpoint(ctx, j, q.ID, seq, key, state); err != nil {
		return 0, err
	}
	return seq, nil
}

func (h TenantSnapshots[Job, Endpoint]) capture(q *Sequence, position func() int64, includeRows bool, prepare func() (func(), error)) (TenantImage[Job, Endpoint], map[string][]*records.Row, int64, error) {
	q.Lane.Lock()
	defer q.Lane.Unlock()
	var none TenantImage[Job, Endpoint]
	if h.Quarantined() {
		return none, nil, 0, fmt.Errorf("tenant %s is quarantined", q.ID)
	}
	if h.Pending() {
		return none, nil, 0, fmt.Errorf("tenant %s: events pending", q.ID)
	}
	if prepare != nil {
		cleanup, err := prepare()
		if err != nil {
			return none, nil, 0, err
		}
		if cleanup != nil {
			defer cleanup()
		}
	}
	s := TenantImage[Job, Endpoint]{Apps: map[string]json.RawMessage{}, Records: map[string][]records.StoredState{}, Bindings: map[string]string{}, Queues: map[string][]string{}}
	for _, a := range h.Apps {
		raw, err := a.Snapshot()
		if err != nil {
			return none, nil, 0, fmt.Errorf("tenant %s: app %s: %v", q.ID, a.Manifest().ID, err)
		}
		s.Apps[a.Manifest().ID] = raw
	}
	rows := map[string][]*records.Row{}
	if includeRows {
		func() {
			q.Projection.Lock()
			defer q.Projection.Unlock()
			for typ, et := range q.Projection.Types() {
				rows[typ] = make([]*records.Row, 0, et.Rows().Len())
				for _, value := range et.Rows().All() {
					rows[typ] = append(rows[typ], value)
				}
			}
		}()
	}
	if includeRows {
		state := q.Committed.SnapshotState()
		s.DecisionDefinitions, s.SubmissionSchemas, s.CompositeAnswers = state.DecisionDefinitions, state.SubmissionSchemas, state.CompositeAnswers
		s.Refusals, s.AcceptedAnswers, s.AcceptedInputs, s.Composites = state.Refusals, state.AcceptedAnswers, state.AcceptedInputs, state.Composites
	}
	unlock, err := h.Capture(&s)
	if err != nil {
		return none, nil, 0, err
	}
	defer unlock()
	seq := position()
	if seq == 0 {
		seq = q.SnapshotPosition
	}
	q.SnapshotPosition = seq
	return s, rows, seq, nil
}

func (h TenantSnapshots[Job, Endpoint]) Restore(q *Sequence, raw json.RawMessage) error {
	var s TenantImage[Job, Endpoint]
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if s.Runtime != nil && (s.Runtime.Format != SQLCheckpointFormat || len(s.Records) != 0 || q.Projection.Current().Backend() == nil || q.Projection.Current().View() == nil || q.Projection.Current().View().Position() != s.Runtime.Position) {
		return fmt.Errorf("tenant %s: SQL checkpoint requires its matching runtime store", q.ID)
	}
	if s.Checkpoint != nil {
		compatibility, err := h.Compatibility()
		if err != nil {
			return err
		}
		if err := compatibility.Check(q.ID, raw, s.Checkpoint.Position, h.Build()); err != nil {
			return err
		}
	}
	q.Lane.Lock()
	defer q.Lane.Unlock()
	h.Invalidate()
	if s.Checkpoint != nil {
		q.SnapshotPosition = s.Checkpoint.Position
	}
	q.Projection.Current().SelectRelease(s.ActiveRelease)
	if len(s.Apps) != len(h.Apps) {
		return fmt.Errorf("tenant %s: the snapshot has %d apps, the tenant %d", q.ID, len(s.Apps), len(h.Apps))
	}
	for _, a := range h.Apps {
		raw, saved := s.Apps[a.Manifest().ID]
		if !saved {
			return fmt.Errorf("tenant %s: app %s has no snapshot", q.ID, a.Manifest().ID)
		}
		if err := a.Restore(raw); err != nil {
			return fmt.Errorf("tenant %s: app %s: %v", q.ID, a.Manifest().ID, err)
		}
	}
	held, err := q.RestoreRecords(s.Records, h.CheckRecord)
	if err != nil {
		return err
	}
	if err := h.Definitions(&s, held); err != nil {
		return err
	}
	if err := q.Projection.ValidateLinkConstraints(); err != nil {
		return err
	}
	if err := h.Services(&s); err != nil {
		return err
	}
	if err := q.RestoreCommitted(CommittedState{DecisionDefinitions: s.DecisionDefinitions, SubmissionSchemas: s.SubmissionSchemas, CompositeAnswers: s.CompositeAnswers, Refusals: s.Refusals, AcceptedAnswers: s.AcceptedAnswers, AcceptedInputs: s.AcceptedInputs, Composites: s.Composites},
		ReleaseSnapshot{Requests: s.ReleaseRequests, Applied: s.ReleaseApplied, Candidates: s.ReleaseCandidates, Active: s.ActiveRelease},
		h.Apps, h.AppOf, h.Inputs); err != nil {
		return err
	}
	return h.Operations(&s)
}
