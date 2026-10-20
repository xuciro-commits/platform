package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/acceptance"
	"platformserver/internal/records"
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
	b := storageDeltaBuilder{records: map[string]journal.StateRecord{}, changes: map[string]journal.StateChange{}, values: map[string]journal.StateValue{}, unique: map[string]journal.StateUnique{}}
	if err := b.accepted(raw); err != nil {
		return journal.StateDelta{}, err
	}
	changes := make([]journal.StateChange, 0, len(b.changeOrder))
	for _, key := range b.changeOrder {
		changes = append(changes, b.changes[key])
	}
	return journal.StateDelta{Records: slices.Collect(maps.Values(b.records)), Changes: changes, Values: slices.Collect(maps.Values(b.values)), Unique: slices.Collect(maps.Values(b.unique)), LegacyUnique: b.legacyUnique}, nil
}

type storageDeltaBuilder struct {
	records      map[string]journal.StateRecord
	changes      map[string]journal.StateChange
	changeOrder  []string
	values       map[string]journal.StateValue
	unique       map[string]journal.StateUnique
	legacyUnique bool
}

func (b *storageDeltaBuilder) value(owner, key string, image any) {
	raw, err := json.Marshal(image)
	if err != nil {
		panic(err)
	}
	b.values[owner+"/"+key] = journal.StateValue{Owner: owner, Key: key, Image: raw}
}
func (b *storageDeltaBuilder) record(r records.Image, base int) error {
	image, err := json.Marshal(recordState{Value: r.Value, Format: r.Format, Projection: r.Projection, Extraction: r.Extraction})
	if err != nil {
		return err
	}
	if r.HistoryRef != nil {
		base = r.HistoryRef.Base
	}
	stored := journal.StateRecord{Type: r.Type, ID: r.ID, Image: image, HistoryBase: base}
	if r.HistoryRef != nil {
		stored.HistoryPrior = r.HistoryRef.Prior
		stored.HistoryHead = r.HistoryRef.Head
	}
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

// plan translates the same declared changes used by accepted application.
// It never infers writes from application actions or reinterprets a family as
// a loosely typed envelope with coincidentally matching field names.
func (b *storageDeltaBuilder) plan(plan acceptedChangePlan) error {
	for _, row := range plan.Rows {
		if err := b.record(row.Image, -1); err != nil {
			return err
		}
	}
	for _, state := range plan.States {
		b.value("app-state", state.App, state.Image)
	}
	if plan.Control != nil {
		for _, change := range plan.Control.Changes {
			b.value("control", change.Kind, change.Image)
		}
	}
	for name, n := range plan.Sequences {
		b.value("sequence", name, n)
	}
	for _, intent := range plan.Intents {
		b.value("effect", intent.ID, effectState{Effect: intent})
	}
	if plan.Notices != nil {
		for _, notice := range plan.Notices.Notices {
			b.value("notice", notice.ID, notice)
		}
		for _, intent := range plan.Notices.Effects {
			b.value("effect", intent.ID, effectState{Effect: intent})
		}
		b.value("notice-sequence", "current", plan.Notices.Seq)
	}
	if len(plan.Changes) > 0 {
		if err := b.accepted(plan.Changes); err != nil {
			return err
		}
	}
	for _, replacement := range plan.Unique {
		b.unique[replacement.Type+"/"+replacement.ID] = replacement
	}
	return nil
}

func (b *storageDeltaBuilder) accepted(raw json.RawMessage) error {
	var envelope struct{ Kind string }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	switch envelope.Kind {
	case "input-result":
		result, err := decodeAcceptedInput(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 2
		b.value("input", result.App+"/"+result.Key, raw)
		return b.plan(result.changePlan())
	case "effect-result":
		result, err := decodeAcceptedEffect(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 4
		if err := b.plan(result.changePlan()); err != nil {
			return err
		}
		if len(result.Work) > 0 {
			b.value("work", "kernel", result.Work)
		}
		b.value("effect", result.After.ID, result.After)
		return nil
	case "work-result":
		result, err := decodeAcceptedWork(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 3
		if err := b.plan(result.changePlan()); err != nil {
			return err
		}
		b.value("work", "kernel", result.Work)
		b.value("task", result.After.ID, result.After)
		return nil
	case "record-batch":
		result, receipt, err := decodeAcceptedBatch(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 8
		sub, err := batchSubmission(result, receipt)
		if err != nil {
			return err
		}
		key := result.App + "/" + sub.GetIdempotencyKey()
		b.value("answer", key, raw)
		if result.SchemaVersion != nil {
			b.value("schema-version", key, result.SchemaVersion)
		}
		for _, row := range result.Rows {
			base := -1
			if result.Version >= 2 {
				base = row.HistoryBase
			}
			if err := b.record(row.Image, base); err != nil {
				return err
			}
		}
		for _, callback := range result.DirectRows {
			if err := b.record(callback.Row.Image, -1); err != nil {
				return err
			}
		}
		for _, decision := range result.Decisions {
			if err := b.receipt(decision.Event.App, decision.Receipt, decision.Definition, nil); err != nil {
				return err
			}
		}
		return b.plan(result.changePlan())
	case "refusal":
		result, sub, err := decodeRefusedResult(raw)
		if err != nil {
			return err
		}
		key := result.App + "/" + sub.GetIdempotencyKey()
		b.value("refusal", key, raw)
		if result.SchemaVersion != nil {
			b.value("schema-version", key, result.SchemaVersion)
		}
		return nil
	case "composite-result":
		result, err := decodeAcceptedComposite(raw)
		if err != nil {
			return err
		}
		b.value("composite-answer", result.Key, raw)
		b.value("composite", result.Key, result.Digest)
		return b.plan(acceptedChangePlan{Changes: result.Changes})
	case "release-result":
		result, err := decodeAcceptedRelease(raw)
		if err != nil {
			return err
		}
		b.value("answer", "release:"+result.Key, raw)
		for _, installation := range result.Installations {
			b.value("installation", installation.Schema+"/"+installation.Record, installation)
		}
		return b.plan(acceptedChangePlan{Changes: result.Bootstrap})
	case "operation-claim":
		result, err := decodeOperationClaim(raw)
		if err != nil {
			return err
		}
		b.value("work", "kernel", result.Work)
		b.value("effect", result.After.ID, result.After)
		return nil
	case "agent-result":
		result, err := decodeAcceptedAgent(raw)
		if err != nil {
			return err
		}
		return b.plan(acceptedChangePlan{Changes: result.Changes})
	case "host-result":
		_, err := decodeAcceptedHostControl(raw)
		return err
	case "usage-result":
		_, err := decodeAcceptedUsage(raw)
		return err
	default:
		result, _, err := decodeAcceptedResult(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 5
		if err := b.record(result.Row, -1); err != nil {
			return err
		}
		if err := b.receipt(result.App, result.Receipt, result.Definition, result.SchemaVersion); err != nil {
			return err
		}
		return b.plan(acceptedChangePlan{Unique: result.Unique})
	}
}

// migrateRuntimeState is called before serving this tenant. Its mutex pauses
// writes while the original in-memory state is copied and compared at one
// journal position. Only then are SQL-backed reads installed and history freed.
func (t *Tenant) migrateRuntimeState(ctx context.Context, j Journals) error {
	return t.migrateRuntimeStateFrom(ctx, j, nil)
}

func (t *Tenant) migrateRuntimeStateFrom(ctx context.Context, j Journals, prefix *journal.StateCheckpoint) error {
	store, ok := j.(journal.RuntimeState)
	if !ok {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d, err := t.runtimeStateDeltaLocked()
	if err != nil {
		return err
	}
	if prefix != nil {
		recovery, ok := j.(journal.RuntimeStateRecovery)
		if !ok {
			return fmt.Errorf("tenant %s state store cannot recover a retained suffix", t.ID)
		}
		if err := recovery.RestoreState(ctx, t.ID, j.Position(t.ID), *prefix, d); err != nil {
			return err
		}
	} else if err := store.InitializeState(ctx, t.ID, j.Position(t.ID), d); err != nil {
		return err
	}
	// Load committed current images rather than retaining the reconstruction as
	// a second authority. Definitions have already installed their original types.
	t.records.mu.Lock()
	for typ, et := range t.records.types {
		for id, r := range et.rows.All() {
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
			actual, _ := r.Image()
			if !sameStoredJSON(actual, image.Value) {
				t.records.mu.Unlock()
				return fmt.Errorf("SQL current image differs: %s/%s", typ, id)
			}
			decoded, err := records.DecodeFormattedImage(et.info, image.Value, image.Projection, t.records.readRelease, image.Format)
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
			count := r.HistoryLength()
			head, err := r.HistoryDigest()
			if err != nil {
				t.records.mu.Unlock()
				return err
			}
			current := &records.Row{Value: value, Format: image.Format, HistoryCount: count, HistoryHead: head, Projection: image.Projection, Extraction: image.Extraction}
			current.RetainOriginal(image.Value)
			t.records.attachHistory(et, id, current, store, t.ID)
		}
	}
	for _, et := range t.records.types {
		et.uniqueIndexes = nil
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

// The tenant lock fixes definitions and all owner state at the same replay position.
func (t *Tenant) runtimeStateDeltaLocked() (journal.StateDelta, error) {
	d := journal.StateDelta{}
	t.records.mu.Lock()
	for typ, et := range t.records.types {
		for id, r := range et.rows.All() {
			image, err := r.Image()
			if err != nil {
				t.records.mu.Unlock()
				return journal.StateDelta{}, err
			}
			state := recordState{Value: image, Format: r.Format, Projection: r.Projection, Extraction: r.Extraction}
			head, err := r.HistoryDigest()
			if err != nil {
				t.records.mu.Unlock()
				return journal.StateDelta{}, err
			}
			rr := journal.StateRecord{Type: typ, ID: id, Image: jsonOf(state), HistoryBase: -1, HistoryHead: head}
			for _, h := range r.RecordHistory() {
				rr.History = append(rr.History, jsonOf(h))
			}
			d.Records = append(d.Records, rr)
			d.Unique = append(d.Unique, journal.StateUnique{Type: typ, ID: id, Keys: t.records.uniqueKeys(typ, id)})
		}
	}
	t.records.mu.Unlock()
	for _, a := range t.apps {
		for index, r := range a.AcceptedLedger().RecordsFor(t.ID) {
			image, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
			if err != nil {
				return journal.StateDelta{}, err
			}
			d.Changes = append(d.Changes, journal.StateChange{App: a.Manifest().ID, ID: r.GetChangeId(), Key: r.GetSubmission().GetIdempotencyKey(), Index: index + 1, Image: image})
		}
	}
	for _, a := range t.apps {
		if owner, ok := a.(platform.AcceptedStateApp); ok {
			raw, err := owner.AcceptedState()
			if err != nil {
				return journal.StateDelta{}, err
			}
			d.Values = append(d.Values, journal.StateValue{Owner: "app-state", Key: a.Manifest().ID, Image: raw})
		}
	}
	t.committed.storageValues(&d)
	for name, n := range t.sequences.clone() {
		d.Values = append(d.Values, journal.StateValue{Owner: "sequence", Key: name, Image: jsonOf(n)})
	}
	return d, nil
}

func sameStoredJSON(a, b json.RawMessage) bool {
	x, e := acceptance.Digest(a)
	y, f := acceptance.Digest(b)
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
