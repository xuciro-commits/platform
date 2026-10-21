package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/aistate"
	"platformserver/internal/records"
	"slices"
	"strings"

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
	return journal.StateDelta{Records: slices.Collect(maps.Values(b.records)), Replacements: b.replacements, Changes: changes, Values: slices.Collect(maps.Values(b.values)), ValueUpdates: b.valueUpdates, Unique: slices.Collect(maps.Values(b.unique)), LegacyUnique: b.legacyUnique}, nil
}

type storageDeltaBuilder struct {
	records      map[string]journal.StateRecord
	replacements []journal.StateReplacement
	changes      map[string]journal.StateChange
	changeOrder  []string
	values       map[string]journal.StateValue
	valueUpdates []journal.StateValueUpdate
	unique       map[string]journal.StateUnique
	legacyUnique bool
}

func (b *storageDeltaBuilder) value(owner, key string, image any) {
	// A later complete owner image already includes preceding observations.
	b.valueUpdates = slices.DeleteFunc(b.valueUpdates, func(update journal.StateValueUpdate) bool { return update.Owner == owner && update.Key == key })
	raw, err := json.Marshal(image)
	if err != nil {
		panic(err)
	}
	b.values[owner+"/"+key] = journal.StateValue{Owner: owner, Key: key, Image: raw}
}
func (b *storageDeltaBuilder) meter(usage platform.AIUsage) {
	b.valueUpdates = append(b.valueUpdates, journal.StateValueUpdate{Owner: "app-state", Key: platform.AIApp, Apply: func(raw json.RawMessage) (json.RawMessage, error) { return aistate.ApplyUsageState(raw, usage) }})
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
func (b *storageDeltaBuilder) receipt(app string, raw json.RawMessage, definition *resultformat.DecisionDefinition, binding *platform.SchemaVersion) error {
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
func (b *storageDeltaBuilder) plan(plan resultformat.ChangePlan) error {
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
		b.value("effect", intent.ID, resultformat.EffectState{Effect: intent})
	}
	if plan.Notices != nil {
		for _, notice := range plan.Notices.Notices {
			b.value("notice", notice.ID, notice)
		}
		for _, intent := range plan.Notices.Effects {
			b.value("effect", intent.ID, resultformat.EffectState{Effect: intent})
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
		result, err := resultformat.DecodeInput(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 2
		b.value("input", result.App+"/"+result.Key, raw)
		return b.plan(result.ChangePlan())
	case "effect-result":
		result, err := resultformat.DecodeEffect(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 4
		if err := b.plan(result.ChangePlan()); err != nil {
			return err
		}
		if len(result.Work) > 0 {
			b.value("work", "kernel", result.Work)
		}
		b.value("effect", result.After.ID, result.After)
		if result.Usage != nil {
			b.meter(*result.Usage)
		}
		return nil
	case "work-result":
		result, err := decodeAcceptedWork(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 3
		if err := b.plan(result.ChangePlan()); err != nil {
			return err
		}
		b.value("work", "kernel", result.Work)
		b.value("task", result.After.ID, result.After)
		return nil
	case "record-batch":
		result, receipt, err := resultformat.DecodeBatch(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 8
		sub, err := resultformat.BatchSubmission(result, receipt)
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
		return b.plan(result.ChangePlan())
	case "refusal":
		result, sub, err := resultformat.DecodeRefusal(raw)
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
		return b.plan(resultformat.ChangePlan{Changes: result.Changes})
	case "release-result":
		result, err := resultformat.DecodeRelease(raw)
		if err != nil {
			return err
		}
		b.value("answer", "release:"+result.Key, raw)
		if err := b.plan(resultformat.ChangePlan{Changes: result.Bootstrap}); err != nil {
			return err
		}
		for _, installation := range result.Installations {
			b.value("installation", installation.Schema+"/"+installation.Record, installation)
			record, recordErr := result.InstallationRecord(installation)
			if recordErr != nil {
				return recordErr
			}
			typ, id, ok := strings.Cut(record, "/")
			if !ok {
				return fmt.Errorf("installation record identity differs")
			}
			b.replacements = append(b.replacements, journal.StateReplacement{Type: typ, ID: id, Before: installation.Before, Value: installation.Image})
		}
		return nil
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
		if err := b.plan(resultformat.ChangePlan{Changes: result.Changes}); err != nil {
			return err
		}
		if result.UsageBefore != "" {
			b.meter(result.Step.Usage)
		}
		return nil
	case "host-result":
		_, err := decodeAcceptedHostControl(raw)
		return err
	case "usage-result":
		result, err := resultformat.DecodeUsage(raw)
		if err == nil {
			b.meter(result.Usage)
		}
		return err
	default:
		result, _, err := resultformat.DecodeRecord(raw)
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
		return b.plan(resultformat.ChangePlan{Unique: result.Unique})
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
	if upgrade, ok := j.(journal.RuntimeStateUpgrade); ok {
		needed, upgradeErr := upgrade.StateNeedsUpgrade(ctx, t.ID)
		if upgradeErr != nil {
			return upgradeErr
		}
		if needed {
			position := j.Position(t.ID)
			if prefix != nil {
				position = prefix.Position
			}
			images, values, readErr := legacyStatePreimages(ctx, j, t.ID, position)
			if readErr != nil {
				return readErr
			}
			if prefix != nil {
				prefix.Delta.LegacyImages, prefix.Delta.LegacyValues = images, values
			} else {
				d.LegacyImages, d.LegacyValues = images, values
			}
		}
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
	// The migration compared every original image and history at this position.
	// Keep SQL as the current-record source instead of decoding a second copy.
	t.installRuntimeRecords(store, func() int64 { return j.Position(t.ID) })
	t.installRuntimeHistory(store, func() int64 { return j.Position(t.ID) })
	t.enableCurrentReadScopes()
	return nil
}

type acceptedStateReader interface {
	acceptedValueReader
	StateChanges(context.Context, string, string, string, string, int) ([][]byte, error)
}

// The snapshot archive and the current SQL view share the original history
// readers. During tail recovery the archive remains the fixed prefix while
// accepted application adds its original, privately held replacements.
func (t *Tenant) installRuntimeHistory(store acceptedStateReader, position func() int64) {
	for _, a := range t.apps {
		a.AcceptedLedger().UseChangeArchive(changeArchive{store, t.ID, a.Manifest().ID})
		if owner, ok := a.(platform.AcceptedHistoryApp); ok {
			id := a.Manifest().ID
			owner.UseAcceptedHistory(func() (json.RawMessage, error) {
				return store.StateValue(context.Background(), t.ID, "app-state", id, -1)
			})
		}
	}
	t.committed.useStorage(store, t.ID, position)
}

// installRuntimeRecords is called only after migration or recovery established
// agreement with the durable SQL position. Fetched rows stay in the request's
// private work set rather than a second tenant-wide current-state authority.
func (t *Tenant) installRuntimeRecords(store journal.RuntimeState, position func() int64) {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	t.records.historyStore, t.records.tenant = store, t.ID
	t.records.appliedPosition = position()
	t.records.currentPosition = position
	t.records.useCurrentRows()
}

// The tenant lock fixes definitions and all owner state at the same replay position.
func (t *Tenant) runtimeStateDeltaLocked() (journal.StateDelta, error) {
	d := journal.StateDelta{}
	if err := func() error {
		t.records.mu.Lock()
		defer t.records.mu.Unlock()
		for typ, et := range t.records.types {
			for id, r := range et.rows.All() {
				image, err := r.Image()
				if err != nil {
					return err
				}
				state := recordState{Value: image, Format: r.Format, Projection: r.Projection, Extraction: r.Extraction}
				head, err := r.HistoryDigest()
				if err != nil {
					return err
				}
				rr := journal.StateRecord{Type: typ, ID: id, Image: jsonOf(state), HistoryBase: -1, HistoryHead: head}
				for _, h := range r.RecordHistory() {
					rr.History = append(rr.History, jsonOf(h))
				}
				d.Records = append(d.Records, rr)
				d.Unique = append(d.Unique, journal.StateUnique{Type: typ, ID: id, Keys: t.records.uniqueKeys(typ, id)})
			}
		}
		return nil
	}(); err != nil {
		return journal.StateDelta{}, err
	}
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
	store       acceptedStateReader
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

// Only the historical SQL projection requires these original preimages. New
// writes carry their installation replacement in the same sealed storage delta.
func legacyStatePreimages(ctx context.Context, j Journals, tenant string, through int64) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	entries, err := j.Entries(ctx, tenant, 0)
	if err != nil {
		return nil, nil, err
	}
	initialAI, err := json.Marshal(aistate.State{})
	if err != nil {
		return nil, nil, err
	}
	values := map[string]json.RawMessage{"app-state/" + platform.AIApp: initialAI}
	ordinary := map[string]json.RawMessage{}
	installed := map[string]json.RawMessage{}
	for index, entry := range entries {
		if int64(index+1) > through {
			break
		}
		if entry.Kind != "accepted-result" {
			continue
		}
		delta, err := acceptedStorageDelta(entry.Body)
		if err != nil {
			return nil, nil, err
		}
		for _, value := range delta.Values {
			if value.Owner == "app-state" && value.Key == platform.AIApp {
				values[value.Owner+"/"+value.Key] = value.Image
			}
		}
		for _, record := range delta.Records {
			key := record.Type + "/" + record.ID
			ordinary[key] = record.Image
			delete(installed, key)
		}
		for _, replacement := range delta.Replacements {
			key := replacement.Type + "/" + replacement.ID
			if image := ordinary[key]; len(image) != 0 {
				installed[key] = image
			}
		}
	}
	return installed, values, nil
}
