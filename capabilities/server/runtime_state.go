package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/journal"
	"platformserver/platform"
)

// The result owners already sealed these deltas. This adapter translates their
// envelopes into storage addresses; SQL never interprets actions or decisions.
func acceptedStorageDelta(raw []byte) (journal.StateDelta, error) {
	b := storageDeltaBuilder{records: map[string]journal.StateRecord{}, changes: map[string]journal.StateChange{}, values: map[string]journal.StateValue{}}
	if err := b.accepted(raw); err != nil {
		return journal.StateDelta{}, err
	}
	changes := make([]journal.StateChange, 0, len(b.changeOrder))
	for _, key := range b.changeOrder {
		changes = append(changes, b.changes[key])
	}
	return journal.StateDelta{Records: slices.Collect(maps.Values(b.records)), Changes: changes, Values: slices.Collect(maps.Values(b.values))}, nil
}

type storageDeltaBuilder struct {
	records     map[string]journal.StateRecord
	changes     map[string]journal.StateChange
	changeOrder []string
	values      map[string]journal.StateValue
}

func (b *storageDeltaBuilder) value(owner, key string, image any) {
	raw, err := json.Marshal(image)
	if err != nil {
		panic(err)
	}
	b.values[owner+"/"+key] = journal.StateValue{Owner: owner, Key: key, Image: raw}
}
func (b *storageDeltaBuilder) record(r acceptedRow, base int) error {
	image, err := json.Marshal(recordState{Value: r.Value, Projection: r.Projection, Extraction: r.Extraction})
	if err != nil {
		return err
	}
	stored := journal.StateRecord{Type: r.Type, ID: r.ID, Image: image, HistoryBase: base}
	for _, h := range r.History {
		stored.History = append(stored.History, jsonOf(h))
	}
	b.records[r.Type+"/"+r.ID] = stored
	return nil
}
func (b *storageDeltaBuilder) receipt(app string, raw json.RawMessage, definition *DecisionDefinition, binding *platform.SchemaVersion) error {
	r := new(pb.ChangeRecord)
	if err := protojson.Unmarshal(raw, r); err != nil {
		return err
	}
	image, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
	if err != nil {
		return err
	}
	key := app + "/" + r.GetChangeId()
	if _, exists := b.changes[key]; !exists {
		b.changeOrder = append(b.changeOrder, key)
	}
	b.changes[key] = journal.StateChange{App: app, ID: r.GetChangeId(), Key: r.GetSubmission().GetIdempotencyKey(), Image: image}
	if definition != nil {
		b.value("decision-definition", app+"/"+r.GetChangeId(), definition)
	}
	if binding != nil {
		b.value("schema-version", app+"/"+r.GetSubmission().GetIdempotencyKey(), binding)
	}
	return nil
}
func (b *storageDeltaBuilder) accepted(raw json.RawMessage) error {
	id, err := acceptedIdentity(raw)
	if err != nil {
		return err
	}
	var e struct {
		Kind          string                  `json:"kind"`
		Version       int                     `json:"version"`
		Row           acceptedRow             `json:"row"`
		Rows          []acceptedBatchRow      `json:"rows"`
		DirectRows    []acceptedCallbackRow   `json:"directRows"`
		Receipt       json.RawMessage         `json:"receipt"`
		Decisions     []acceptedDecision      `json:"decisions"`
		Definition    *DecisionDefinition     `json:"definition"`
		SchemaVersion *platform.SchemaVersion `json:"schemaVersion"`
		Changes       json.RawMessage         `json:"changes"`
		Bootstrap     json.RawMessage         `json:"bootstrap"`
		States        []acceptedState         `json:"states"`
		Control       *acceptedControl        `json:"control"`
		Sequences     map[string]int          `json:"sequences"`
		Intents       []platform.Effect       `json:"intents"`
		Notices       *acceptedNotices        `json:"notices"`
		Work          json.RawMessage         `json:"work"`
		After         json.RawMessage         `json:"after"`
		Installations []releaseInstallation   `json:"installations"`
	}
	if err = json.Unmarshal(raw, &e); err != nil {
		return err
	}
	key := id.App + "/" + id.Key
	switch e.Kind {
	case "record-batch":
		b.value("answer", key, raw)
	case "input-result":
		b.value("input", key, raw)
	case "refusal":
		b.value("refusal", key, raw)
	case "composite-result":
		b.value("composite-answer", id.Key, raw)
		var c acceptedComposite
		_ = json.Unmarshal(raw, &c)
		b.value("composite", id.Key, c.Digest)
	case "release-result":
		b.value("answer", "release:"+id.Key, raw)
	}
	if e.SchemaVersion != nil {
		b.value("schema-version", key, e.SchemaVersion)
	}
	if len(e.Row.Value) > 0 {
		if err = b.record(e.Row, -1); err != nil {
			return err
		}
	}
	for _, r := range e.Rows {
		base := -1
		if e.Kind == "record-batch" && e.Version >= 2 {
			base = r.HistoryBase
		}
		if err = b.record(r.acceptedRow, base); err != nil {
			return err
		}
	}
	for _, r := range e.DirectRows {
		if err = b.record(r.Row.acceptedRow, -1); err != nil {
			return err
		}
	}
	if len(e.Receipt) > 0 && len(e.Row.Value) > 0 {
		if err = b.receipt(id.App, e.Receipt, e.Definition, e.SchemaVersion); err != nil {
			return err
		}
	}
	for _, r := range e.Decisions {
		if err = b.receipt(r.Event.App, r.Receipt, r.Definition, nil); err != nil {
			return err
		}
	}
	for _, r := range e.States {
		b.value("app-state", r.App, r.Image)
	}
	if e.Control != nil {
		for _, r := range e.Control.Changes {
			b.value("control", r.Kind, r.Image)
		}
	}
	for name, n := range e.Sequences {
		b.value("sequence", name, n)
	}
	for _, r := range e.Intents {
		b.value("effect", r.ID, effectState{Effect: r})
	}
	if e.Notices != nil {
		for _, n := range e.Notices.Notices {
			b.value("notice", n.ID, n)
		}
		for _, r := range e.Notices.Effects {
			b.value("effect", r.ID, effectState{Effect: r})
		}
		b.value("notice-sequence", "current", e.Notices.Seq)
	}
	if len(e.Work) > 0 {
		b.value("work", "kernel", e.Work)
	}
	if len(e.After) > 0 {
		var r struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(e.After, &r) == nil && r.ID != "" {
			owner := "task"
			if e.Kind == "effect-result" || e.Kind == "operation-claim" {
				owner = "effect"
			}
			b.value(owner, r.ID, e.After)
		}
	}
	for _, r := range e.Installations {
		b.value("installation", r.Schema+"/"+r.Record, r)
	}
	// Original nested result formats retain the same single accepted transaction.
	for _, child := range []json.RawMessage{e.Changes, e.Bootstrap} {
		if len(child) > 0 {
			if err = b.accepted(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateRuntimeState is called before serving this tenant. Its mutex pauses
// writes while the original in-memory state is copied and compared at one
// journal position. Only then are SQL-backed reads installed and history freed.
func (t *Tenant) migrateRuntimeState(ctx context.Context, j Journals) error {
	store, ok := j.(journal.RuntimeState)
	if !ok {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d := journal.StateDelta{}
	t.records.mu.Lock()
	for typ, et := range t.records.types {
		for id, r := range et.rows.all() {
			image, err := r.image()
			if err != nil {
				t.records.mu.Unlock()
				return err
			}
			state := recordState{Value: image, Projection: r.projection, Extraction: r.extraction}
			rr := journal.StateRecord{Type: typ, ID: id, Image: jsonOf(state), HistoryBase: -1}
			for _, h := range r.recordHistory() {
				rr.History = append(rr.History, jsonOf(h))
			}
			d.Records = append(d.Records, rr)
		}
	}
	t.records.mu.Unlock()
	for _, a := range t.apps {
		for index, r := range a.AcceptedLedger().RecordsFor(t.ID) {
			image, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
			if err != nil {
				return err
			}
			d.Changes = append(d.Changes, journal.StateChange{App: a.Manifest().ID, ID: r.GetChangeId(), Key: r.GetSubmission().GetIdempotencyKey(), Index: index + 1, Image: image})
		}
	}
	for _, a := range t.apps {
		if owner, ok := a.(platform.AcceptedStateApp); ok {
			raw, err := owner.AcceptedState()
			if err != nil {
				return err
			}
			d.Values = append(d.Values, journal.StateValue{Owner: "app-state", Key: a.Manifest().ID, Image: raw})
		}
	}
	t.committed.storageValues(&d)
	for name, n := range t.sequences.clone() {
		d.Values = append(d.Values, journal.StateValue{Owner: "sequence", Key: name, Image: jsonOf(n)})
	}
	if err := store.InitializeState(ctx, t.ID, j.Position(t.ID), d); err != nil {
		return err
	}
	// Load committed current images rather than retaining the reconstruction as
	// a second authority. Definitions have already installed their original types.
	t.records.mu.Lock()
	for typ, et := range t.records.types {
		for id, r := range et.rows.all() {
			raw, err := store.StateRecord(ctx, t.ID, typ, id)
			if err != nil {
				t.records.mu.Unlock()
				return err
			}
			var image recordState
			if err = json.Unmarshal(raw, &image); err != nil {
				t.records.mu.Unlock()
				return err
			}
			actual, _ := r.image()
			if !sameStoredJSON(actual, image.Value) {
				t.records.mu.Unlock()
				return fmt.Errorf("SQL current image differs: %s/%s", typ, id)
			}
			decoded, err := rowDecodeImage(et.info, image.Value, image.Projection, t.records.readRelease)
			if err != nil {
				t.records.mu.Unlock()
				return err
			}
			value := reflect.New(et.info.Go).Elem()
			if err = json.Unmarshal(decoded, value.Addr().Interface()); err != nil {
				t.records.mu.Unlock()
				return err
			}
			emptyLists(value)
			count := len(r.history)
			if r.loadHistory != nil {
				count = r.historyCount
			}
			current := &row{value: value, historyCount: count, projection: image.Projection, extraction: image.Extraction}
			current.retainOriginal(image.Value)
			t.records.attachHistory(et, id, current, store, t.ID)
		}
	}
	t.records.historyStore, t.records.tenant = store, t.ID
	t.records.appliedPosition = j.Position(t.ID)
	t.records.currentPosition = func() int64 { return j.Position(t.ID) }
	t.records.mu.Unlock()
	for _, a := range t.apps {
		a.AcceptedLedger().UseChangeArchive(changeArchive{store, t.ID, a.Manifest().ID})
		if owner, ok := a.(platform.AcceptedHistoryApp); ok {
			id := a.Manifest().ID
			owner.UseAcceptedHistory(func() (json.RawMessage, error) {
				return store.StateValue(context.Background(), t.ID, "app-state", id, -1)
			})
		}
	}
	t.committed.useStorage(store, t.ID, func() int64 { return j.Position(t.ID) })
	return nil
}
func sameStoredJSON(a, b json.RawMessage) bool {
	x, e := canonicalDigest(a)
	y, f := canonicalDigest(b)
	return e == nil && f == nil && x == y
}

type changeArchive struct {
	store       journal.RuntimeState
	tenant, app string
}

func (a changeArchive) read(tenant, key, id string, through int) []*pb.ChangeRecord {
	if tenant != a.tenant {
		return nil
	}
	rows, err := a.store.StateChanges(context.Background(), tenant, a.app, key, id, through)
	if err != nil {
		panic(fmt.Errorf("change history read: %w", err))
	}
	out := make([]*pb.ChangeRecord, 0, len(rows))
	for _, raw := range rows {
		r := new(pb.ChangeRecord)
		if err := proto.Unmarshal(raw, r); err != nil {
			panic(err)
		}
		out = append(out, r)
	}
	return out
}
func (a changeArchive) Records(tenant string, through int) []*pb.ChangeRecord {
	return a.read(tenant, "", "", through)
}
func (a changeArchive) Accepted(tenant, key string, through int) *pb.ChangeRecord {
	rows := a.read(tenant, key, "", through)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}
func (a changeArchive) Contains(tenant, id string, through int) bool {
	return len(a.read(tenant, "", id, through)) > 0
}
