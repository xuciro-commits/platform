package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"maps"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/host"
	"platformserver/internal/records"
	"platformserver/journal"
	"reflect"
	"slices"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// A snapshot is a tenant's state at a journal position: what the
// host keeps (portable records/history or a fixed SQL record archive, owned
// work, connectors, notices,
// settings, endpoints and effects, bindings, the audit) and each app's own,
// through platform.Snapshotter. Restored into a tenant composed as at start-up,
// then given the entries after that position, it equals a full replay;
// CheckReplay holds every composition to that. A snapshot is only a shortcut:
// the journal stays the truth. Explicit host/owner formats and the native
// composition decide compatibility; the build digest is diagnostic metadata.
type tenantState struct {
	Runtime             *runtimeSnapshot                            `json:"runtime,omitempty"`
	Checkpoint          *snapshotCheckpoint                         `json:"checkpoint,omitempty"`
	DecisionDefinitions map[string]*resultformat.DecisionDefinition `json:"decisionDefinitions,omitempty"`
	SubmissionSchemas   map[string]platform.SchemaVersion           `json:"submissionSchemas,omitempty"`
	Apps                map[string]json.RawMessage                  `json:"apps"`
	Records             map[string][]recordState                    `json:"records"`
	Audit               []AuditEntry                                `json:"audit"`
	Refusals            map[string]resultformat.Refusal             `json:"refusals,omitempty"`
	AcceptedAnswers     map[string]json.RawMessage                  `json:"acceptedAnswers,omitempty"`
	AcceptedInputs      map[string]json.RawMessage                  `json:"acceptedInputs,omitempty"`
	ReleaseCandidates   map[string]json.RawMessage                  `json:"releaseCandidates,omitempty"`
	ReleaseRequests     map[string]string                           `json:"releaseRequests,omitempty"`
	ReleaseApplied      map[string]string                           `json:"releaseApplied,omitempty"`
	ActiveRelease       string                                      `json:"activeRelease,omitempty"`
	Deliveries          []Delivery                                  `json:"deliveries"`
	Acted               int                                         `json:"acted"`
	Bindings            map[string]string                           `json:"bindings"` // protocol → provider app
	Works               json.RawMessage                             `json:"works"`
	Queues              map[string][]string                         `json:"queues"` // subscriber → task IDs, head first
	Failed              []string                                    `json:"failed"`
	Tasks               []taskState                                 `json:"tasks"` // every delivery task, by ID
	Jobs                []Task                                      `json:"jobs"`
	Connectors          json.RawMessage                             `json:"connectors"`
	Marks               []kernel.ConnectorMark                      `json:"marks"`
	LastError           map[string]ConnectorError                   `json:"lastError"`
	Notices             []platform.Notification                     `json:"notices"`
	NoticeSeq           int                                         `json:"noticeSeq"`
	Settings            map[string]string                           `json:"settings"`
	Words               map[string]map[string]string                `json:"words,omitempty"` // the tenant's translations
	Endpoints           []*Endpoint                                 `json:"endpoints"`
	Outbound            []resultformat.EffectState                  `json:"outbound"`
	// The host console's lifecycle and support sessions travel
	// with the tenant: a restart keeps a suspension and its authorized sessions.
	HostRequests     map[string]hostControlReceipt    `json:"hostRequests,omitempty"`
	HostLifecycle    string                           `json:"hostLifecycle,omitempty"`
	Support          []SupportGrant                   `json:"support,omitempty"`
	Sealed           map[string]SealedArtifact        `json:"sealed,omitempty"`
	ReleaseOrigins   map[string]ReleaseOrigin         `json:"releaseOrigins,omitempty"`
	Migrations       []MigrationManifest              `json:"migrations,omitempty"`
	CompositeAnswers map[string]json.RawMessage       `json:"compositeAnswers,omitempty"`
	Composites       map[string]string                `json:"composites,omitempty"`
	Staged           map[string]platform.StagedResult `json:"staged,omitempty"`
	Sequences        map[string]int                   `json:"sequences,omitempty"`
}

const tenantSnapshotFormat = "platform.tenant.v1"
const tenantSQLSnapshotFormat = "platform.tenant.sql.v1"

var snapshotBuild = sync.OnceValue(func() string { return CodeOf() })

type snapshotCompatibility struct {
	Host        string            `json:"host"`
	Owners      map[string]string `json:"owners"`
	Composition string            `json:"composition"`
}

type snapshotCheckpoint struct {
	snapshotCompatibility
	Tenant   string `json:"tenant"`
	Position int64  `json:"position"`
	Build    string `json:"build"`
}

// snapshotCompatibility describes only the startup composition. Published
// tenant definitions belong to the saved state and are reinstalled by Restore.
func (t *Tenant) snapshotCompatibility() (snapshotCompatibility, error) {
	if t.startupSnapshot != nil {
		return *t.startupSnapshot, nil
	}
	compatibility := snapshotCompatibility{Host: tenantSnapshotFormat, Owners: map[string]string{}}
	type compositionApp struct {
		ID, Version string
		Entities    []platform.EntityInfo
		Provides    []platform.Provision
		Consumes    []platform.Consumption
	}
	composition := make([]compositionApp, 0, len(t.apps))
	for _, app := range t.apps {
		manifest := app.Manifest()
		format, declared := app.(platform.SnapshotFormatOwner)
		if !declared || strings.TrimSpace(format.SnapshotFormat()) == "" {
			// Keep the startup composition for in-process tenant recovery even
			// when this owner requires exact-build checkpoint eligibility.
			compatibility.Host = ""
		} else {
			compatibility.Owners[manifest.ID] = format.SnapshotFormat()
		}
		item := compositionApp{ID: manifest.ID, Version: manifest.Version, Provides: manifest.Provides, Consumes: manifest.Consumes}
		own := map[reflect.Type]string{}
		for _, entity := range manifest.Entities {
			own[reflect.TypeOf(entity.Model)] = entity.Type
		}
		for _, entity := range manifest.Entities {
			info, err := platform.Describe(manifest.ID, entity, func(goType reflect.Type) string { return own[goType] })
			if err != nil {
				return snapshotCompatibility{}, err
			}
			item.Entities = append(item.Entities, info)
		}
		slices.SortFunc(item.Entities, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
		composition = append(composition, item)
	}
	slices.SortFunc(composition, func(a, b compositionApp) int { return strings.Compare(a.ID, b.ID) })
	digest, err := acceptance.Digest(composition)
	compatibility.Composition = digest
	return compatibility, err
}

// snapshotKey keeps the existing journal interface and legacy checkpoint names.
// An undeclared owner retains exact-build eligibility; explicit formats select
// a separate namespace and are verified again against the loaded checkpoint.
func (t *Tenant) snapshotKey(code string) (string, error) {
	compatibility, err := t.snapshotCompatibility()
	if err != nil || compatibility.Host == "" {
		return code, err
	}
	return acceptance.Digest(compatibility)
}

// runtimeSnapshotKey keeps an owner-only SQL checkpoint outside the portable
// namespace. Undeclared owners still require the exact binary key as before.
func (t *Tenant) runtimeSnapshotKey(code string) (string, error) {
	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return "", err
	}
	exact := compatibility.Host == ""
	compatibility.Host = tenantSQLSnapshotFormat
	if exact {
		return acceptance.Digest(struct {
			Compatibility snapshotCompatibility
			Build         string
		}{compatibility, code})
	}
	return acceptance.Digest(compatibility)
}

func (t *Tenant) snapshotStorageKey(code string, raw []byte) (string, error) {
	runtime, err := runtimeCheckpoint(raw)
	if err != nil {
		return "", err
	}
	if runtime != nil {
		return t.runtimeSnapshotKey(code)
	}
	return t.snapshotKey(code)
}

func (t *Tenant) checkSnapshotCheckpoint(raw []byte, position int64) error {
	var header struct {
		Checkpoint *snapshotCheckpoint `json:"checkpoint"`
		Runtime    *runtimeSnapshot    `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Checkpoint == nil && header.Runtime == nil {
		return nil
	} // legacy exact-code snapshot
	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return err
	}
	saved := header.Checkpoint
	if header.Runtime != nil {
		if saved == nil || header.Runtime.Format != runtimeSnapshotFormat || header.Runtime.Position != position {
			return fmt.Errorf("tenant %s: SQL checkpoint format or position differs", t.ID)
		}
		if compatibility.Host == "" && saved.Build != snapshotBuild() {
			return fmt.Errorf("tenant %s: undeclared owner requires its original checkpoint build", t.ID)
		}
		compatibility.Host = tenantSQLSnapshotFormat
	}
	if saved == nil || saved.Tenant != t.ID || saved.Position != position || compatibility.Host == "" || !reflect.DeepEqual(saved.snapshotCompatibility, compatibility) {
		return fmt.Errorf("tenant %s: checkpoint format, composition or position differs", t.ID)
	}
	return nil
}

type recordState struct {
	HistoryHead string                 `json:"historyHead,omitempty"`
	Value       json.RawMessage        `json:"value"`
	Format      string                 `json:"format,omitempty"`
	History     []records.RecordChange `json:"history"`
	Projection  *records.Projection    `json:"projection,omitempty"`
	Extraction  *records.Extraction    `json:"extraction,omitempty"`
}

type taskState struct {
	Task
	Since    int                 `json:"since,omitempty"`
	EventApp string              `json:"eventApp,omitempty"` // the event it delivers
	Event    json.RawMessage     `json:"event,omitempty"`
	Hops     int                 `json:"hops,omitempty"`
	Changed  []string            `json:"changed,omitempty"` // the records its decision put
	Plan     *resultformat.Event `json:"plan,omitempty"`    // accepted-result delivery routing
}

// Snapshot saves the tenant's state between inputs; the caller's position
// function runs under the same lock, so the journal position matches the state.
// Decisions wait only while the state is captured: records never change once
// stored (a put stores a new row), so they are encoded after the lock.
func (t *Tenant) Snapshot(position func() int64) (json.RawMessage, int64, error) {
	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return nil, 0, err
	}
	var read journal.StateReadView
	var ownsRead bool
	defer func() {
		if read != nil && ownsRead {
			_ = read.Close()
		}
	}()
	s, rows, seq, err := t.capture(position, true, func() (func(), error) {
		if t.records.readView != nil {
			read = t.records.readView
			if t.currentReadScopes {
				ownsRead = true
				return t.detachSnapshotRead, nil
			}
			return nil, nil // the recovery caller owns this archive view
		}
		views, ok := t.records.historyStore.(journal.StateReadViews)
		if !ok {
			return nil, nil
		}
		var err error
		read, err = views.BeginStateRead(context.Background(), t.ID, t.records.appliedPosition)
		if err != nil {
			return nil, err
		}
		ownsRead = true
		t.records.bindReadView(read)
		return t.detachSnapshotRead, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if compatibility.Host != "" {
		s.Checkpoint = &snapshotCheckpoint{snapshotCompatibility: compatibility, Tenant: t.ID, Position: seq, Build: snapshotBuild()}
	}
	for typ, list := range rows {
		saved := make([]recordState, len(list))
		for i, r := range list {
			raw, err := r.Image()
			if err != nil {
				return nil, 0, err
			}
			head, err := r.HistoryDigest()
			if err != nil {
				return nil, 0, err
			}
			saved[i] = recordState{HistoryHead: head, Value: raw, Format: r.Format, History: r.RecordHistory(), Projection: records.CloneProjection(r.Projection), Extraction: r.Extraction}
		}
		slices.SortFunc(saved, func(a, b recordState) int { return strings.Compare(string(a.Value), string(b.Value)) })
		s.Records[typ] = saved
	}
	raw, err := json.Marshal(s)
	return raw, seq, err
}

// A captured row can read history while it is encoded after the tenant lock
// is released. Transfer that view to the snapshot instead of opening a second
// connection; the lane resumes its ordinary storage readers before unlocking.
func (t *Tenant) detachSnapshotRead() {
	t.records.bindReadView(nil)
	if store, ok := t.records.historyStore.(acceptedStateReader); ok && t.records.currentPosition != nil {
		t.installRuntimeHistory(store, t.records.currentPosition)
	}
}

// runtimeSnapshot identifies the SQL records belonging to an owner checkpoint.
// Portable Snapshot exports retain complete rows; deployment checkpoints keep
// the same owner state without copying the complete SQL record set.
const runtimeSnapshotFormat = "platform.sql-checkpoint.v1"

type runtimeSnapshot struct {
	Format   string              `json:"format"`
	Position int64               `json:"position"`
	Records  journal.StateDigest `json:"records"`
}

func (t *Tenant) snapshotRuntime(ctx context.Context, j Journals) (json.RawMessage, int64, error) {
	views, ok := j.(journal.StateReadViews)
	if !ok || t.records.historyStore == nil {
		return t.Snapshot(func() int64 { return j.Position(t.ID) })
	}
	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return nil, 0, err
	}
	var view journal.StateReadView
	var viewErr error
	var ownsRead bool
	s, _, seq, err := t.capture(func() int64 {
		position := j.Position(t.ID)
		if t.records.readView != nil {
			view = t.records.readView
			if view.Position() != position {
				viewErr = fmt.Errorf("tenant %s: SQL snapshot view position differs", t.ID)
			}
			if t.currentReadScopes {
				ownsRead = true
				t.detachSnapshotRead()
			}
		} else {
			view, viewErr = views.BeginStateRead(ctx, t.ID, position)
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
		return nil, 0, fmt.Errorf("tenant %s: SQL snapshot read view unavailable", t.ID)
	}
	if ownsRead {
		defer view.Close()
	}
	digest, err := view.Checkpoint(ctx, t.ID)
	if err != nil {
		return nil, 0, err
	}
	s.Runtime = &runtimeSnapshot{Format: runtimeSnapshotFormat, Position: seq, Records: digest}
	compatibility.Host = tenantSQLSnapshotFormat
	s.Checkpoint = &snapshotCheckpoint{snapshotCompatibility: compatibility, Tenant: t.ID, Position: seq, Build: snapshotBuild()}
	raw, err := json.Marshal(s)
	return raw, seq, err
}

// capture takes everything but the records' encoding under the tenant's locks.
func (t *Tenant) capture(position func() int64, includeRows bool, prepare func() (func(), error)) (tenantState, map[string][]*records.Row, int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return tenantState{}, nil, 0, fmt.Errorf("tenant %s is quarantined", t.ID)
	}
	if len(t.events) > 0 {
		return tenantState{}, nil, 0, fmt.Errorf("tenant %s: events pending", t.ID)
	}
	if prepare != nil {
		cleanup, err := prepare()
		if err != nil {
			return tenantState{}, nil, 0, err
		}
		if cleanup != nil {
			defer cleanup()
		}
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
	rows := map[string][]*records.Row{}
	if includeRows {
		func() {
			t.records.mu.Lock()
			defer t.records.mu.Unlock()
			for typ, et := range t.records.types {
				rows[typ] = make([]*records.Row, 0, et.rows.Len())
				for _, value := range et.rows.All() {
					rows[typ] = append(rows[typ], value)
				}
			}
		}()
	}
	t.audit.snapshot(&s)
	if includeRows {
		t.committed.snapshot(&s)
	}
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
		s.Outbound = append(s.Outbound, resultformat.EffectState{Effect: x.Effect, Since: x.since})
	}
	t.console.snapshot(&s)
	s.Staged = t.staged.snapshot()
	seq := position()
	if seq == 0 {
		seq = t.snapshotPosition
	}
	t.snapshotPosition = seq
	return s, rows, seq, nil
}

// restoreRecords restores the records of every type declared now, and hands
// back those of types nobody has declared yet: a tenant's own objects, which
// their definitions install.
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
		entries := make([]records.Entry[*records.Row], 0, len(rows))
		for _, r := range rows {
			v := reflect.New(et.info.Go).Elem()
			decode, err := records.DecodeFormattedImage(et.info, r.Value, r.Projection, t.records.readRelease, r.Format)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(decode, v.Addr().Interface()); err != nil {
				return nil, err
			}
			if err := checkRunApplicationBindings(v.Interface()); err != nil {
				return nil, err
			}
			records.EmptyLists(v) // a snapshot of older code may hold null lists
			recovered := &records.Row{Value: v, Format: r.Format, History: r.History, Projection: records.CloneProjection(r.Projection), Extraction: r.Extraction}
			head, err := recovered.HistoryDigest()
			if err != nil {
				return nil, err
			}
			if r.HistoryHead != "" && r.HistoryHead != head {
				return nil, fmt.Errorf("snapshot history chain differs")
			}
			recovered.HistoryHead = head
			recovered.RetainOriginal(r.Value)
			entries = append(entries, records.Entry[*records.Row]{Key: records.Header(v).ID, Value: recovered})
		}
		index, err := records.From(entries)
		if err != nil {
			return nil, err
		}
		et.setRows(index)
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
	if s.Runtime != nil && (s.Runtime.Format != runtimeSnapshotFormat || len(s.Records) != 0 || t.records.historyStore == nil || t.records.readView == nil || t.records.readView.Position() != s.Runtime.Position) {
		return fmt.Errorf("tenant %s: SQL checkpoint requires its matching runtime store", t.ID)
	}
	if s.Checkpoint != nil {
		if err := t.checkSnapshotCheckpoint(raw, s.Checkpoint.Position); err != nil {
			return err
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.Checkpoint != nil {
		t.snapshotPosition = s.Checkpoint.Position
	}
	t.records.readRelease = s.ActiveRelease
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
	// those.
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
	if err := validateHostImage(t.ID, hostControlImage{s.HostLifecycle, s.Support, s.Migrations, s.HostRequests}); err != nil {
		return err
	}
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
// records of the types they install.
func (t *Tenant) restoreDefinitions(s *tenantState, held map[string][]recordState) error {
	needsDefinitions := len(held) > 0 || s.Runtime != nil
	for typ, kind := range map[string]platform.AssetKind{construction.PropertyTypeType: platform.AssetPropertyType, construction.LinkTypeType: platform.AssetLinkType, construction.QueryType: platform.AssetQuery, construction.ObjectType: platform.AssetObject, construction.PageType: platform.AssetPage, construction.AppType: platform.AssetApp, construction.ProcessType: platform.AssetFlow, construction.FunctionType: platform.AssetFunction, construction.CodeType: platform.AssetCompute, construction.TableType: platform.AssetCompute} {
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
				name = construction.TypeOf(name)
			}
			if kind == platform.AssetFlow {
				var state struct {
					Name    string `json:"name"`
					Version int    `json:"version"`
				}
				json.Unmarshal([]byte(meta.Published), &state)
				owner, ok := t.procs.(host.FlowDefinitionPresence)
				needsDefinitions = needsDefinitions || !ok || !owner.HasDefinition(construction.ID, state.Name, state.Version)
				continue
			}

			ref := platform.AssetRef{App: construction.ID, Kind: kind, Name: name}
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
	for typ, et := range t.records.types {
		for _, row := range et.rows.All() {
			if err := validateExtractionOrigin(t.records, typ, row.Extraction); err != nil {
				return err
			}
		}
	}

	return nil
}

// verifyCommitted checks that every committed answer in the snapshot is one
// this tenant and its apps still recognise, before it is restored.
func (t *Tenant) verifyCommitted(s *tenantState) error {
	for key, binding := range s.SubmissionSchemas {
		app, request, _ := strings.Cut(key, "/")
		owner := t.app(app)
		if owner == nil || request == "" || binding.Check() != nil {
			return fmt.Errorf("tenant %s: schema snapshot %s is incompatible", t.ID, key)
		}
		receipt := owner.AcceptedLedger().AcceptedFor(t.ID, request)
		sub := receipt.GetSubmission()
		if raw := s.AcceptedAnswers[key]; len(raw) > 0 {
			result, answer, err := resultformat.DecodeBatch(raw)
			if err != nil {
				return fmt.Errorf("tenant %s: schema snapshot %s has an invalid answer", t.ID, key)
			}
			sub, err = resultformat.BatchSubmission(result, answer)
			if err != nil || sub.GetIdempotencyKey() != request || result.App != app {
				return fmt.Errorf("tenant %s: schema snapshot %s has no matching request", t.ID, key)
			}
		}
		if sub == nil || resultformat.ValidateSubmissionSchemaVersion(sub, &binding) != nil {
			return fmt.Errorf("tenant %s: schema snapshot %s has no matching receipt", t.ID, key)
		}
	}
	for key, result := range s.Refusals {
		raw, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("tenant %s: refusal snapshot %s: %w", t.ID, key, err)
		}
		decoded, sub, err := resultformat.DecodeRefusal(raw)
		if err != nil || decoded.Tenant != t.ID || key != decoded.App+"/"+sub.GetIdempotencyKey() {
			return fmt.Errorf("tenant %s: refusal snapshot %s is incompatible: %v", t.ID, key, err)
		}
	}
	for key, hash := range s.ReleaseRequests {
		if len(hash) != 64 || s.ReleaseApplied[key] == "" {
			return fmt.Errorf("release request snapshot %s is incompatible", key)
		}
	}
	for key, raw := range s.CompositeAnswers {
		r, err := decodeAcceptedComposite(raw)
		if err != nil || r.Version != 2 || r.Tenant != t.ID || r.Key != key || s.Composites[key] != r.Digest {
			return fmt.Errorf("tenant %s: composite answer snapshot %s is incompatible", t.ID, key)
		}
		if r.Refusal == "" {
			batch, _, _ := resultformat.DecodeBatch(r.Changes)
			for _, decision := range batch.Decisions {
				receipt := new(pb.ChangeRecord)
				if protojson.Unmarshal(decision.Receipt, receipt) != nil {
					return fmt.Errorf("invalid composite snapshot receipt")
				}
				owner := t.app(decision.Event.App)
				if owner == nil || !proto.Equal(owner.AcceptedLedger().AcceptedFor(t.ID, receipt.GetSubmission().GetIdempotencyKey()), receipt) {
					return fmt.Errorf("composite snapshot has no accepted receipt")
				}
			}
		}
	}
	if len(s.DecisionDefinitions) > 0 {
		receipts := map[string]*pb.ChangeRecord{}
		for _, app := range t.apps {
			for _, receipt := range app.AcceptedLedger().RecordsFor(t.ID) {
				receipts[app.Manifest().ID+"/"+receipt.GetChangeId()] = receipt
			}
		}
		for key, definition := range s.DecisionDefinitions {
			receipt := receipts[key]
			if definition == nil || receipt == nil || definition.Check(receipt.GetSubmission().GetAuthority(), receipt.GetSubmission().GetSchema().GetName()) != nil || definition.Contract.Family != receipt.GetSubmission().GetSchema().GetVersion() {
				return fmt.Errorf("tenant %s: decision definition snapshot %s has no matching receipt", t.ID, key)
			}
		}
	}
	t.committed.restore(s)
	for key, raw := range s.AcceptedAnswers {
		if strings.HasPrefix(key, "release:") {
			result, err := resultformat.DecodeRelease(raw)
			if err != nil || (result.Version != 3 && result.Version != 4) || result.Tenant != t.ID || key != "release:"+result.Key || s.ReleaseApplied[result.Key] != result.Digest || !bytes.Equal(s.ReleaseCandidates[result.CandidateID], result.Bytes) {
				return fmt.Errorf("tenant %s: release answer snapshot %s is incompatible", t.ID, key)
			}
			continue
		}
		result, receipt, err := resultformat.DecodeBatch(raw)
		sub, subErr := resultformat.BatchSubmission(result, receipt)
		if err != nil || subErr != nil || result.Tenant != t.ID ||
			!result.HasSavedAnswer(sub) || key != result.App+"/"+sub.GetIdempotencyKey() {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s is incompatible", t.ID, key)
		}
		owner, ok := t.app(receipt.GetSubmission().GetAuthority()).(platform.App)
		if !ok || !proto.Equal(owner.AcceptedLedger().AcceptedFor(t.ID, receipt.GetSubmission().GetIdempotencyKey()), receipt) {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s has no receipt", t.ID, key)
		}
		if _, refused := t.committed.refusal(key); refused {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s reuses a refused key", t.ID, key)
		}
		binding, bound := s.SubmissionSchemas[key]
		if bound != (result.SchemaVersion != nil) || result.SchemaVersion != nil && binding != *result.SchemaVersion {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s has a different schema binding", t.ID, key)
		}
	}
	for key, raw := range s.AcceptedInputs {
		result, err := resultformat.DecodeInput(raw)
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
	for _, job := range s.Jobs {
		if err := checkApplicationBindings(job.Applications); err != nil {
			return err
		}
	}
	for _, effect := range s.Outbound {
		if err := checkApplicationBindings(effect.Effect.Applications); err != nil {
			return err
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
