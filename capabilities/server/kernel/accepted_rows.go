package kernel

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func AcceptedIdentity(raw []byte) (persistence.Identity, error) {
	var identity persistence.Identity
	var envelope struct{ Kind string }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return identity, err
	}
	identity.Scope = "submission"
	if envelope.Kind == "host-result" {
		r, e := acceptedformat.DecodeHostControl(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "host"
		return identity, e
	}
	if envelope.Kind == "composite-result" {
		r, e := acceptedformat.DecodeComposite(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "composite"
		return identity, e
	}
	if envelope.Kind == "usage-result" {
		r, e := acceptedformat.DecodeUsage(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "usage"
		return identity, e
	}
	if envelope.Kind == "agent-result" {
		r, e := acceptedformat.DecodeAgent(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "agent"
		return identity, e
	}
	if envelope.Kind == "operation-claim" {
		result, err := acceptedformat.DecodeOperationClaim(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	if envelope.Kind == "refusal" {
		result, sub, err := acceptedformat.DecodeRefusal(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "record-batch" {
		result, receipt, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			return identity, err
		}
		sub, err := acceptedformat.BatchSubmission(result, receipt)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "work-result" {
		result, err := acceptedformat.DecodeWork(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "work"
		return identity, err
	}
	if envelope.Kind == "input-result" {
		result, err := acceptedformat.DecodeInput(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "input"
		return identity, err
	}
	if envelope.Kind == "effect-result" {
		result, err := acceptedformat.DecodeEffect(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, acceptedformat.EffectApp, result.Key, result.RequestHash
		identity.Scope = "effect"
		return identity, err
	}
	if envelope.Kind == "release-result" {
		result, err := acceptedformat.DecodeRelease(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	result, receipt, err := acceptedformat.DecodeRecord(raw)
	identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, receipt.GetSubmission().GetIdempotencyKey(), result.RequestHash
	return identity, err
}

func ApplyDirectRows(base *records.Store, rows []acceptedformat.InputRow, member string, at time.Time, checkRow func(any) error) (*records.Store, error) {
	draft := base.ForkRecords()
	draft.Lock()
	defer draft.Unlock()
	for _, image := range rows {
		et := draft.Type(image.Type)
		if et == nil || et.Info().App != image.App {
			return nil, fmt.Errorf("direct result needs missing record type %s", image.Type)
		}
		prior := et.Rows().Get(image.ID)
		before, previousHistory, revision := "", 0, uint32(0)
		if image.HistoryRef != nil {
			if err := records.CheckHistoryPredecessor(image.Image, prior); err != nil {
				return nil, err
			}
			var err error
			before, err = records.Predecessor(image.Type, image.ID, prior)
			if err != nil {
				return nil, err
			}
			if before != image.Before {
				return nil, fmt.Errorf("direct record image predecessor differs")
			}
			if prior != nil {
				revision = records.Header(prior.Value).Revision
			}
		} else {
			if prior != nil {
				row, err := records.FullImage(image.Type, image.ID, prior)
				if err != nil {
					return nil, err
				}
				before, err = platform.CanonicalDigest(row)
				if err != nil {
					return nil, err
				}
				previousHistory, revision = len(prior.RecordHistory()), records.Header(prior.Value).Revision
			}
			if image.Before != before || len(image.History) <= previousHistory {
				return nil, fmt.Errorf("direct record predecessor differs")
			}
			if prior != nil {
				old, err := platform.CanonicalDigest(image.History[:previousHistory])
				actual, actualErr := platform.CanonicalDigest(prior.RecordHistory())
				if err != nil || actualErr != nil || old != actual {
					return nil, fmt.Errorf("direct record history predecessor differs")
				}
			}
		}
		for _, change := range image.History[previousHistory:] {
			if change.Change != "" || change.Schema == "" || change.By != member || !change.At.Equal(at) {
				return nil, fmt.Errorf("direct record has a foreign change")
			}
		}
		value := reflect.New(et.Info().Go).Elem()
		decode, err := records.DecodeFormattedImage(et.Info(), image.Value, image.Projection, draft.Current().Release(), image.Format)
		if err != nil {
			return nil, err
		}
		if err := platform.DecodeJSON(decode, value.Addr().Interface(), "direct record"); err != nil {
			return nil, err
		}
		if err := checkRow(value.Interface()); err != nil {
			return nil, err
		}
		rec := records.Header(value)
		last := image.History[len(image.History)-1]
		if rec.ID != image.ID || rec.Revision != revision || rec.Changed != (platform.Stamp{
			By: member, At: at, Change: ""}) ||
			prior != nil && rec.Created != records.Header(prior.Value).Created ||
			prior == nil && rec.Created != (platform.Stamp{By: member,
				At: image.History[0].At, Change: ""}) ||
			last.Schema == "" {
			return nil, fmt.Errorf("direct record image differs from history")
		}
		recovered := &records.Row{Value: value, Format: image.Format, History: records.CopyHistory(image.History), Projection: records.CloneProjection(image.Projection), Extraction: image.Extraction}
		if image.HistoryRef != nil {
			if err := records.AppendRowHistory(recovered, prior, image.History); err != nil {
				return nil, err
			}
		}
		recovered.RetainOriginal(image.Value)
		et.SetRow(image.ID, recovered)
		draft.TrackWrite(image.Type + "/" + image.ID)
		if et.Knowledge() {
			draft.MarkDirty(image.Type + "/" + image.ID)
		}
	}
	return draft, nil
}

func UniqueReplacements(changes map[string]map[string]string) []persistence.StateUnique {
	out := make([]persistence.StateUnique, 0, len(changes))
	for _, ref := range slices.Sorted(maps.Keys(changes)) {
		typ, id, _ := strings.Cut(ref, "/")
		out = append(out, persistence.StateUnique{Type: typ, ID: id, Keys: changes[ref]})
	}
	return out
}

func ValidateUniqueReplacements(store *records.Store, saved []persistence.StateUnique) error {
	store.Lock()
	defer store.Unlock()
	expected := store.UniqueChanges()
	if len(saved) != len(expected) {
		return fmt.Errorf("record unique-key replacements differ")
	}
	seen := map[string]bool{}
	for _, row := range saved {
		ref := row.Type + "/" + row.ID
		keys, ok := expected[ref]
		if !ok || seen[ref] || !maps.Equal(keys, row.Keys) {
			return fmt.Errorf("record unique-key replacement differs for %s", ref)
		}
		seen[ref] = true
	}
	return nil
}
