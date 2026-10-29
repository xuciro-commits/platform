package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/aistate"
	"platformserver/journal"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"slices"
	"strings"
)

func StorageDelta(raw []byte) (journal.StateDelta, error) {
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
	image, err := json.Marshal(records.StoredState{Value: r.Value, Format: r.Format, Projection: r.Projection, Extraction: r.Extraction})
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
		stored.History = append(stored.History, platform.Raw(h))
	}
	b.records[r.Type+"/"+r.ID] = stored
	return nil
}

func (b *storageDeltaBuilder) receipt(app string, raw json.RawMessage, definition *platform.DecisionDefinition, binding *platform.SchemaVersion) error {
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

func (b *storageDeltaBuilder) plan(plan acceptedformat.ChangePlan) error {
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
		b.value("effect", intent.ID, acceptedformat.EffectState{Effect: intent})
	}
	if plan.Notices != nil {
		for _, notice := range plan.Notices.Notices {
			b.value("notice", notice.ID, notice)
		}
		for _, intent := range plan.Notices.Effects {
			b.value("effect", intent.ID, acceptedformat.EffectState{Effect: intent})
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
		result, err := acceptedformat.DecodeInput(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 2
		b.value("input", result.App+"/"+result.Key, raw)
		return b.plan(result.ChangePlan())
	case "effect-result":
		result, err := acceptedformat.DecodeEffect(raw)
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
		result, err := acceptedformat.DecodeWork(raw)
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
		result, receipt, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			return err
		}
		b.legacyUnique = b.legacyUnique || result.Version < 8
		sub, err := acceptedformat.BatchSubmission(result, receipt)
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
			if publication := decision.Publication; publication != nil {
				installation, err := acceptedformat.SystemDefinitionInstallation(publication.Schema, publication.Image)
				if err != nil {
					return err
				}
				if len(installation) > 0 {
					if err := b.accepted(installation); err != nil {
						return err
					}
				}
			}
		}
		return b.plan(result.ChangePlan())
	case "refusal":
		result, sub, err := acceptedformat.DecodeRefusal(raw)
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
		result, err := acceptedformat.DecodeComposite(raw)
		if err != nil {
			return err
		}
		b.value("composite-answer", result.Key, raw)
		b.value("composite", result.Key, result.Digest)
		return b.plan(acceptedformat.ChangePlan{Changes: result.Changes})
	case "release-result":
		result, err := acceptedformat.DecodeRelease(raw)
		if err != nil {
			return err
		}
		b.value("answer", "release:"+result.Key, raw)
		if err := b.plan(acceptedformat.ChangePlan{Changes: result.Bootstrap}); err != nil {
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
		result, err := acceptedformat.DecodeOperationClaim(raw)
		if err != nil {
			return err
		}
		b.value("work", "kernel", result.Work)
		b.value("effect", result.After.ID, result.After)
		return nil
	case "agent-result":
		result, err := acceptedformat.DecodeAgent(raw)
		if err != nil {
			return err
		}
		if err := b.plan(acceptedformat.ChangePlan{Changes: result.Changes}); err != nil {
			return err
		}
		if result.UsageBefore != "" {
			b.meter(result.Step.Usage)
		}
		return nil
	case "host-result":
		_, err := acceptedformat.DecodeHostControl(raw)
		return err
	case "usage-result":
		result, err := acceptedformat.DecodeUsage(raw)
		if err == nil {
			b.meter(result.Usage)
		}
		return err
	default:
		result, _, err := acceptedformat.DecodeRecord(raw)
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
		return b.plan(acceptedformat.ChangePlan{Unique: result.Unique})
	}
}

type AcceptedStateReader interface {
	AcceptedValueReader
	StateChanges(context.Context, string, string, string, string, int) ([][]byte, error)
}

func StoredJSONEqual(a, b json.RawMessage) bool {
	x, e := platform.CanonicalDigest(a)
	y, f := platform.CanonicalDigest(b)
	return e == nil && f == nil && x == y
}

type ChangeArchive struct {
	store       AcceptedStateReader
	tenant, app string
}

func (a ChangeArchive) read(tenant, key, id string, through int) []*pb.ChangeRecord {
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

func (a ChangeArchive) Records(tenant string, through int) []*pb.ChangeRecord {
	return a.read(tenant, "", "", through)
}

func (a ChangeArchive) Accepted(tenant, key string, through int) *pb.ChangeRecord {
	rows := a.read(tenant, key, "", through)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func (a ChangeArchive) Contains(tenant, id string, through int) bool {
	return len(a.read(tenant, "", id, through)) > 0
}

func LegacyStatePreimages(ctx context.Context, j journal.Journals, tenant string, through int64) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
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
		delta, err := StorageDelta(entry.Body)
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

func NewChangeArchive(store AcceptedStateReader, tenant, app string) ChangeArchive {
	return ChangeArchive{store: store, tenant: tenant, app: app}
}

type SQLCheckpoint struct {
	Format   string              `json:"format"`
	Position int64               `json:"position"`
	Records  journal.StateDigest `json:"records"`
}

const SQLCheckpointFormat = "platform.sql-checkpoint.v1"

func ReadCheckpoint(raw []byte) (*SQLCheckpoint, error) {
	var header struct {
		Runtime *SQLCheckpoint `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	if header.Runtime != nil {
		var shape struct {
			Records map[string]json.RawMessage `json:"records"`
		}
		if err := json.Unmarshal(raw, &shape); err != nil {
			return nil, err
		}
		if len(shape.Records) > 0 {
			return nil, fmt.Errorf("SQL owner checkpoint contains portable record state")
		}
	}
	return header.Runtime, nil
}

func SaveCheckpoint(ctx context.Context, j journal.Journals, tenant string, seq int64, key string, state []byte) error {
	runtime, err := ReadCheckpoint(state)
	if err != nil {
		return err
	}
	if runtime != nil {
		snapshots, ok := j.(journal.StateSnapshotStorage)
		if !ok {
			return fmt.Errorf("tenant %s: SQL checkpoint archive unavailable", tenant)
		}
		return snapshots.SaveStateSnapshot(ctx, tenant, seq, key, state, runtime.Records)
	}
	return j.SaveSnapshot(ctx, tenant, seq, key, state)
}
