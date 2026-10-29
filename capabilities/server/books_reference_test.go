package platformserver

import (
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/core"
	"platformserver/platform"
)

func TestBookReferenceReaderCannotMaintainMastersOrReadJournals(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	tn := composeTenant(t, "book-references", []platform.Seat{seatOf("reader", "core:"+core.ReferenceReader), seatOf("accountant", "core:accountant")}, core.New("book-references"))
	reader := memberOf(t, tn, "reader")
	decide(t, tn, "accountant", core.ID, core.JournalType+".create", core.JournalType, "POSTED", map[string]any{"date": "2026-10-23", "currency": "CNY", "lines": []map[string]any{{"account": "1403", "debit": 10}, {"account": "2202", "credit": 10}}}, at)
	if _, failure := tn.RecordOf(reader, core.AccountType, "acc-1403", at); failure != nil {
		t.Fatal("the reference reader cannot read a posting account", failure)
	}
	if _, failure := tn.Records(reader, core.PeriodType, platform.Query{}, at); failure != nil {
		t.Fatal("the reference reader cannot read fiscal periods", failure)
	}
	for _, input := range []struct {
		schema, typ, id string
		payload         map[string]any
	}{
		{core.AccountType + ".edit", core.AccountType, "acc-1403", map[string]any{"name": "Changed account"}},
		{core.MaterialType + ".create", core.MaterialType, "M", map[string]any{"code": "M", "name": "Unauthorized master", "kind": "material", "unit": "uom-pce"}},
		{core.JournalType + ".create", core.JournalType, "J", map[string]any{"date": "2026-10-23", "currency": "CNY", "lines": []map[string]any{{"account": "1403", "debit": 10}, {"account": "2202", "credit": 10}}}},
	} {
		before := txnState(tn)
		if result := refuse(t, tn, "reader", core.ID, input.schema, input.typ, input.id, input.payload, at); !strings.HasPrefix(result, "ERROR_CODE_POLICY_DENIED") || txnState(tn) != before {
			t.Fatalf("reference access granted a write: %s", result)
		}
	}
	if rows, failure := tn.Records(reader, core.JournalType, platform.Query{}, at); failure != nil || rows.Total != 0 || len(rows.Records) != 0 {
		t.Fatal("the reference reader received journal rows", failure)
	}
	if _, failure := tn.RecordOf(reader, core.JournalType, "POSTED", at); failure == nil {
		t.Fatal("the reference reader could read a posted journal by ID")
	}
	if _, failure := tn.Read(reader, core.ReadTrialBalance); failure == nil || failure.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatal("an unreadable journal became an empty trial balance", failure)
	}
}
