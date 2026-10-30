package platformserver

import automation "platformserver/engine/automation"

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"maps"

	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/studio/releases"

	"platformserver/journal"
	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

type tenantState struct {
	Runtime             *runtimekernel.SQLCheckpoint            `json:"runtime,omitempty"`
	Checkpoint          *snapshotCheckpoint                     `json:"checkpoint,omitempty"`
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
	Jobs                []automation.Task                       `json:"jobs"`
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
	digest, err := platform.CanonicalDigest(composition)
	compatibility.Composition = digest
	return compatibility, err
}

func (t *Tenant) snapshotKey(code string) (string, error) {
	compatibility, err := t.snapshotCompatibility()
	if err != nil || compatibility.Host == "" {
		return code, err
	}
	return platform.CanonicalDigest(compatibility)
}

func (t *Tenant) runtimeSnapshotKey(code string) (string, error) {
	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return "", err
	}
	exact := compatibility.Host == ""
	compatibility.Host = tenantSQLSnapshotFormat
	if exact {
		return platform.CanonicalDigest(struct {
			Compatibility snapshotCompatibility
			Build         string
		}{compatibility, code})
	}
	return platform.CanonicalDigest(compatibility)
}

func (t *Tenant) snapshotStorageKey(code string, raw []byte) (string, error) {
	runtime, err := runtimekernel.ReadCheckpoint(raw)
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
		Checkpoint *snapshotCheckpoint          `json:"checkpoint"`
		Runtime    *runtimekernel.SQLCheckpoint `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Checkpoint == nil && header.Runtime == nil {
		return nil
	}
	compatibility, err := t.snapshotCompatibility()
	if err != nil {
		return err
	}
	saved := header.Checkpoint
	if header.Runtime != nil {
		if saved == nil || header.Runtime.Format != runtimekernel.SQLCheckpointFormat || header.Runtime.Position != position {
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
		if t.records.Current().View() != nil {
			read = t.records.Current().View()
			if t.currentReadScopes {
				ownsRead = true
				return t.detachSnapshotRead, nil
			}
			return nil, nil
		}
		views, ok := t.records.Current().Backend().(journal.StateReadViews)
		if !ok {
			return nil, nil
		}
		var err error
		read, err = views.BeginStateRead(context.Background(), t.ID, t.records.Current().Applied())
		if err != nil {
			return nil, err
		}
		ownsRead = true
		t.records.Current().Bind(read)
		return t.detachSnapshotRead, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if compatibility.Host != "" {
		s.Checkpoint = &snapshotCheckpoint{snapshotCompatibility: compatibility, Tenant: t.ID, Position: seq, Build: snapshotBuild()}
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

func (t *Tenant) detachSnapshotRead() {
	t.records.Current().Bind(nil)
	if store, ok := t.records.Current().Backend().(runtimekernel.AcceptedStateReader); ok && t.records.Current().Position() != nil {
		t.installRuntimeHistory(store, t.records.Current().Position())
	}
}

func (t *Tenant) snapshotRuntime(ctx context.Context, j Journals) (json.RawMessage, int64, error) {
	views, ok := j.(journal.StateReadViews)
	if !ok || t.records.Current().Backend() == nil {
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
		if t.records.Current().View() != nil {
			view = t.records.Current().View()
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
	s.Runtime = &runtimekernel.SQLCheckpoint{Format: runtimekernel.SQLCheckpointFormat, Position: seq, Records: digest}
	compatibility.Host = tenantSQLSnapshotFormat
	s.Checkpoint = &snapshotCheckpoint{snapshotCompatibility: compatibility, Tenant: t.ID, Position: seq, Build: snapshotBuild()}
	raw, err := json.Marshal(s)
	return raw, seq, err
}

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
	s := tenantState{Apps: map[string]json.RawMessage{}, Records: map[string][]records.StoredState{}, Bindings: map[string]string{}, Queues: map[string][]string{}}
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
			t.records.Lock()
			defer t.records.Unlock()
			for typ, et := range t.records.Types() {
				rows[typ] = make([]*records.Row, 0, et.Rows().Len())
				for _, value := range et.Rows().All() {
					rows[typ] = append(rows[typ], value)
				}
			}
		}()
	}
	s.Audit, s.Deliveries = t.audit.SnapshotState()
	if facts := t.claims.Records(t.ID); len(facts) > 0 {
		var err error
		s.Facts, err = platform.Protos(facts)
		if err != nil {
			return tenantState{}, nil, 0, err
		}
	}
	if identities := t.entityIDs.State(); len(identities.Entities) > 0 || len(identities.Redirects) > 0 {
		s.Identities = &identities
	}
	if includeRows {
		state := t.committed.SnapshotState()
		s.DecisionDefinitions, s.SubmissionSchemas, s.CompositeAnswers = state.DecisionDefinitions, state.SubmissionSchemas, state.CompositeAnswers
		s.Refusals, s.AcceptedAnswers, s.AcceptedInputs, s.Composites = state.Refusals, state.AcceptedAnswers, state.AcceptedInputs, state.Composites
	}
	releaseState := t.releases.SnapshotState()
	s.ReleaseRequests, s.ReleaseCandidates, s.ReleaseApplied = releaseState.ReleaseRequests, releaseState.ReleaseCandidates, releaseState.ReleaseApplied
	s.ActiveRelease, s.Sealed, s.ReleaseOrigins = releaseState.ActiveRelease, releaseState.Sealed, releaseState.ReleaseOrigins
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
	work, workErr := t.work.Snapshot()
	s.Queues = work.Queues
	s.Failed = append(s.Failed, work.Failed...)
	s.Tasks = append(s.Tasks, work.Tasks...)
	s.Jobs = append(s.Jobs, work.Jobs...)
	if err := workErr; err != nil {
		return tenantState{}, nil, 0, err
	}
	descriptors, marks := t.connectors.kernel.State()
	if s.Connectors, err = platform.Protos(descriptors); err != nil {
		return tenantState{}, nil, 0, err
	}
	s.Marks = marks
	t.connectors.snapshot(&s)
	s.Notices, s.NoticeSeq = t.notices.State()
	s.Settings, s.Endpoints, s.Words = t.settings.Snapshot(), t.endpoints, t.words.Clone()
	s.Sequences = t.sequences.Clone()
	for _, x := range t.outbound {
		s.Outbound = append(s.Outbound, acceptedformat.EffectState{Effect: x.Effect, Since: x.since})
	}
	image := t.console.SnapshotState()
	s.HostLifecycle, s.Support, s.Migrations = image.Lifecycle, image.Support, image.Migrations
	s.HostRequests, s.RuntimeSelection, s.DisabledOwners = image.Requests, image.RuntimeSelection, image.DisabledOwners
	s.Staged = t.staged.Snapshot()
	seq := position()
	if seq == 0 {
		seq = t.snapshotPosition
	}
	t.snapshotPosition = seq
	return s, rows, seq, nil
}

func (t *Tenant) restoreRecords(saved map[string][]records.StoredState) (map[string][]records.StoredState, error) {
	held := map[string][]records.StoredState{}
	t.records.Lock()
	defer t.records.Unlock()
	for typ, rows := range saved {
		et := t.records.Type(typ)
		if et == nil {
			held[typ] = rows
			continue
		}
		entries := make([]records.Entry[*records.Row], 0, len(rows))
		for _, r := range rows {
			v := reflect.New(et.Info().Go).Elem()
			decode, err := records.DecodeRowImage(et.Info(), r.Value, r.Projection, t.records.Current().Release(), r.Format)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(decode, v.Addr().Interface()); err != nil {
				return nil, err
			}
			if err := checkRunApplicationBindings(v.Interface()); err != nil {
				return nil, err
			}
			records.EmptyLists(v)
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
		et.SetRows(index)
		t.records.Restored()
	}
	return held, nil
}

func (t *Tenant) Restore(raw json.RawMessage) error {
	var s tenantState
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if s.Runtime != nil && (s.Runtime.Format != runtimekernel.SQLCheckpointFormat || len(s.Records) != 0 || t.records.Current().Backend() == nil || t.records.Current().View() == nil || t.records.Current().View().Position() != s.Runtime.Position) {
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
	t.records.Current().SelectRelease(s.ActiveRelease)
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
	held, err := t.restoreRecords(s.Records)
	if err != nil {
		return err
	}
	if len(s.Facts) > 0 {
		facts, err := platform.Unprotos[*pb.FactRecord](s.Facts)
		if err != nil {
			return err
		}
		t.claims.Restore(t.ID, facts)
	}
	if s.Identities != nil {
		t.entityIDs.Restore(*s.Identities)
	}
	if err := t.restoreDefinitions(&s, held); err != nil {
		return err
	}
	if err := t.records.ValidateLinkConstraints(); err != nil {
		return err
	}
	t.audit.Restore(s.Audit, s.Deliveries)
	if err := controlowner.ValidateHostImage(t.ID, platform.HostControlImage{Lifecycle: s.HostLifecycle, Support: s.Support, Migrations: s.Migrations, Requests: s.HostRequests, RuntimeSelection: s.RuntimeSelection, DisabledOwners: s.DisabledOwners}); err != nil {
		return err
	}
	t.console.Restore(platform.HostControlImage{Lifecycle: s.HostLifecycle, Support: s.Support, Migrations: s.Migrations, Requests: s.HostRequests, RuntimeSelection: s.RuntimeSelection, DisabledOwners: s.DisabledOwners})
	if s.Staged != nil {
		t.staged.Restore(s.Staged)
	}
	if err := t.verifyCommitted(&s); err != nil {
		return err
	}
	t.releases.Restore(releases.State{ReleaseRequests: s.ReleaseRequests, ReleaseCandidates: s.ReleaseCandidates, ReleaseApplied: s.ReleaseApplied, ActiveRelease: s.ActiveRelease, Sealed: s.Sealed, ReleaseOrigins: s.ReleaseOrigins})
	if err := t.restoreSystemDefinitionProjection(); err != nil {
		return err
	}
	for protocol, provider := range s.Bindings {
		if !t.rebind(protocol, provider) {
			return fmt.Errorf("tenant %s: %s is not a provider of %s", t.ID, provider, protocol)
		}
	}
	return t.restoreOperations(&s)
}

func (t *Tenant) restoreDefinitions(s *tenantState, held map[string][]records.StoredState) error {
	needsDefinitions := len(held) > 0 || s.Runtime != nil
	for typ, kind := range map[string]platform.AssetKind{build.PropertyTypeType: platform.AssetPropertyType, build.ValueTypeType: platform.AssetValueType, build.LinkTypeType: platform.AssetLinkType, build.QueryType: platform.AssetQuery, build.ObjectType: platform.AssetObject, build.PageType: platform.AssetPage, build.AppType: platform.AssetApp, build.ProcessType: platform.AssetFlow, build.FunctionType: platform.AssetFunction, build.CodeType: platform.AssetCompute, build.TableType: platform.AssetCompute} {
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
				owner, ok := t.procs.(platform.FlowDefinitionPresence)
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
			result, answer, err := acceptedformat.DecodeBatch(raw)
			if err != nil {
				return fmt.Errorf("tenant %s: schema snapshot %s has an invalid answer", t.ID, key)
			}
			sub, err = acceptedformat.BatchSubmission(result, answer)
			if err != nil || sub.GetIdempotencyKey() != request || result.App != app {
				return fmt.Errorf("tenant %s: schema snapshot %s has no matching request", t.ID, key)
			}
		}
		if sub == nil || acceptedformat.ValidateSubmissionSchemaVersion(sub, &binding) != nil {
			return fmt.Errorf("tenant %s: schema snapshot %s has no matching receipt", t.ID, key)
		}
	}
	for key, result := range s.Refusals {
		raw, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("tenant %s: refusal snapshot %s: %w", t.ID, key, err)
		}
		decoded, sub, err := acceptedformat.DecodeRefusal(raw)
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
		r, err := acceptedformat.DecodeComposite(raw)
		if err != nil || r.Version < 2 || r.Tenant != t.ID || r.Key != key || s.Composites[key] != r.Digest {
			return fmt.Errorf("tenant %s: composite answer snapshot %s is incompatible", t.ID, key)
		}
		if r.Refusal == "" {
			batch, _, _ := acceptedformat.DecodeBatch(r.Changes)
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
	t.committed.Restore(runtimekernel.CommittedState{DecisionDefinitions: s.DecisionDefinitions, SubmissionSchemas: s.SubmissionSchemas, CompositeAnswers: s.CompositeAnswers, Refusals: s.Refusals, AcceptedAnswers: s.AcceptedAnswers, AcceptedInputs: s.AcceptedInputs, Composites: s.Composites})
	for key, raw := range s.AcceptedAnswers {
		if strings.HasPrefix(key, "release:") {
			result, err := acceptedformat.DecodeRelease(raw)
			if err != nil || (result.Version != 3 && result.Version != 4) || result.Tenant != t.ID || key != "release:"+result.Key || s.ReleaseApplied[result.Key] != result.Digest || !bytes.Equal(s.ReleaseCandidates[result.CandidateID], result.Bytes) {
				return fmt.Errorf("tenant %s: release answer snapshot %s is incompatible", t.ID, key)
			}
			continue
		}
		result, receipt, err := acceptedformat.DecodeBatch(raw)
		sub, subErr := acceptedformat.BatchSubmission(result, receipt)
		if err != nil || subErr != nil || result.Tenant != t.ID ||
			!result.HasSavedAnswer(sub) || key != result.App+"/"+sub.GetIdempotencyKey() {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s is incompatible", t.ID, key)
		}
		owner, ok := t.app(receipt.GetSubmission().GetAuthority()).(platform.App)
		if !ok || !proto.Equal(owner.AcceptedLedger().AcceptedFor(t.ID, receipt.GetSubmission().GetIdempotencyKey()), receipt) {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s has no receipt", t.ID, key)
		}
		if _, refused := t.committed.Refusal(key); refused {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s reuses a refused key", t.ID, key)
		}
		binding, bound := s.SubmissionSchemas[key]
		if bound != (result.SchemaVersion != nil) || result.SchemaVersion != nil && binding != *result.SchemaVersion {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s has a different schema binding", t.ID, key)
		}
	}
	for key, raw := range s.AcceptedInputs {
		result, err := acceptedformat.DecodeInput(raw)
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

func (t *Tenant) restoreOperations(s *tenantState) error {
	for _, job := range s.Jobs {
		if err := platform.CheckApplicationBindings(job.Applications); err != nil {
			return err
		}
	}
	for _, effect := range s.Outbound {
		if err := platform.CheckApplicationBindings(effect.Effect.Applications); err != nil {
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
	if err := t.work.Restore(t.ID, automation.WorkState{Queues: s.Queues, Failed: s.Failed, Tasks: s.Tasks, Jobs: s.Jobs}); err != nil {
		return err
	}
	t.connectors.restore(t.ID, descriptors, s)
	t.notices.Restore(s.Notices, s.NoticeSeq)
	t.settings.Restore(s.Settings)
	t.words.Restore(s.Words)
	t.i18n.Reset()
	t.endpoints = s.Endpoints
	t.sequences.Restore(s.Sequences)
	t.outbound = nil
	for _, x := range s.Outbound {
		t.outbound = append(t.outbound, &effect{Effect: x.Effect, since: x.Since})
	}
	return nil
}
