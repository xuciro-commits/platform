package host

import (
	"context"
	"encoding/json"
	"testing"

	"platformserver/apps/history"
	"platformserver/platform"
)

type legacySeedStateFixture map[string]json.RawMessage

func (s legacySeedStateFixture) StateRecord(_ context.Context, _, typ, id string) (json.RawMessage, error) {
	return s[typ+"/"+id], nil
}

func TestInvariantLegacyCoreSeedsRecover(t *testing.T) {
	live := newInvariantTenantReplayWithApps(t, "legacy-core-seeds", []platform.App{history.New("legacy-core-seeds")})
	if row := live.records.Type(history.UoMType).Rows().Get("uom-pce"); row != nil {
		t.Fatal("a new tenant received a legacy core seed")
	}

	state := legacySeedStateFixture{
		"core.uom/uom-pce":        json.RawMessage(`{}`),
		"core.currency/cur-cny":   json.RawMessage(`{}`),
		"core.account/acc-1000":   json.RawMessage(`{}`),
		"core.period/per-2026-01": json.RawMessage(`{}`),
	}
	if err := live.restoreLegacySeeds(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	for typ, id := range map[string]string{
		history.UoMType:      "uom-pce",
		history.CurrencyType: "cur-cny",
		history.AccountType:  "acc-1000",
		history.PeriodType:   "per-2026-01",
	} {
		row := live.records.Type(typ).Rows().Get(id)
		if row == nil || row.HistoryLength() != 1 {
			t.Fatalf("legacy seed %s/%s was not restored exactly once", typ, id)
		}
	}

	if err := live.restoreLegacySeeds(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	for typ, id := range map[string]string{
		history.UoMType:      "uom-pce",
		history.CurrencyType: "cur-cny",
		history.AccountType:  "acc-1000",
		history.PeriodType:   "per-2026-01",
	} {
		if row := live.records.Type(typ).Rows().Get(id); row == nil || row.HistoryLength() != 1 {
			t.Fatalf("legacy seed %s/%s was restored twice", typ, id)
		}
	}
}
