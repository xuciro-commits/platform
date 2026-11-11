package kernel

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"slices"
	"strings"
)

type UsageApplier func(json.RawMessage, platform.AIUsage) (json.RawMessage, error)

func StorageDelta(raw []byte, applyUsage UsageApplier) (persistence.StateDelta, error) {
	b := storageDeltaBuilder{applyUsage: applyUsage, records: map[string]persistence.StateRecord{}, changes: map[string]persistence.StateChange{}, values: map[string]persistence.StateValue{}, unique: map[string]persistence.StateUnique{}}
	if err := b.accepted(raw); err != nil {
		return persistence.StateDelta{}, err
	}
	changes := make([]persistence.StateChange, 0, len(b.changeOrder))
	for _, key := range b.changeOrder {
		changes = append(changes, b.changes[key])
	}
	return persistence.StateDelta{Records: slices.Collect(maps.Values(b.records)), Replacements: b.replacements, Changes: changes, Values: slices.Collect(maps.Values(b.values)), ValueUpdates: b.valueUpdates, Unique: slices.Collect(maps.Values(b.unique))}, nil
}

type storageDeltaBuilder struct {
	applyUsage   UsageApplier
	records      map[string]persistence.StateRecord
	replacements []persistence.StateReplacement
	changes      map[string]persistence.StateChange
	changeOrder  []string
	values       map[string]persistence.StateValue
	valueUpdates []persistence.StateValueUpdate
	unique       map[string]persistence.StateUnique
}

func (b *storageDeltaBuilder) value(owner, key string, image any) {
	b.valueUpdates = slices.DeleteFunc(b.valueUpdates, func(update persistence.StateValueUpdate) bool { return update.Owner == owner && update.Key == key })
	raw, err := json.Marshal(image)
	if err != nil {
		panic(err)
	}
	b.values[owner+"/"+key] = persistence.StateValue{Owner: owner, Key: key, Image: raw}
}

func (b *storageDeltaBuilder) meter(usage platform.AIUsage) {
	b.valueUpdates = append(b.valueUpdates, persistence.StateValueUpdate{Owner: "app-state", Key: platform.AIApp, Apply: func(raw json.RawMessage) (json.RawMessage, error) { return b.applyUsage(raw, usage) }})
}

func (b *storageDeltaBuilder) record(r records.Image, base int) error {
	image, err := json.Marshal(records.StoredState{Value: r.Value, Format: r.Format, Projection: r.Projection, Extraction: r.Extraction})
	if err != nil {
		return err
	}
	if r.HistoryRef != nil {
		base = r.HistoryRef.Base
	}
	stored := persistence.StateRecord{Type: r.Type, ID: r.ID, Image: image, HistoryBase: base}
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
	b.changes[key] = persistence.StateChange{App: app, ID: r.GetChangeId(), Key: r.GetSubmission().GetIdempotencyKey(), Image: image}
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
		b.value("input", result.App+"/"+result.Key, raw)
		return b.plan(result.ChangePlan())
	case "effect-result":
		result, err := acceptedformat.DecodeEffect(raw)
		if err != nil {
			return err
		}
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
		}
		for _, installation := range result.Definitions {
			b.value("installation", installation.Schema+"/"+installation.Record, installation)
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
			b.replacements = append(b.replacements, persistence.StateReplacement{Type: typ, ID: id, Before: installation.Before, Value: installation.Image})
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
		if err := b.record(result.Row, -1); err != nil {
			return err
		}
		if err := b.receipt(result.App, result.Receipt, result.Definition, result.SchemaVersion); err != nil {
			return err
		}
		return b.plan(acceptedformat.ChangePlan{Unique: result.Unique})
	}
}
