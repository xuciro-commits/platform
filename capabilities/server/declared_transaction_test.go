package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestDeclaredTimeCapacityAtomicRetryAndRecovery(t *testing.T) {
	for _, unit := range []string{"day", "hour"} {
		t.Run(unit, func(t *testing.T) {
			kind, layout := "date", time.DateOnly
			if unit == "hour" {
				kind, layout = "datetime", time.RFC3339Nano
			}
			points := make([]string, 14)
			for i := range points {
				at := txnAt.AddDate(0, 0, i)
				if unit == "hour" {
					at = txnAt.Add(time.Duration(i) * time.Hour)
				}
				points[i] = at.Format(layout)
			}
			id := "time-" + unit
			tn := builderTenant(t, id)
			var entries []Entry
			fail := false
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
				if fail {
					fail = false
					return nil, errors.New("injected append failure")
				}
				entries = append(entries, e)
				return e.Body, nil
			}
			publishObject(t, tn, "builder", "CAPACITY", map[string]any{"name": "capacity", "title": "Capacity", "fields": []construction.Field{{Name: "resource", Title: "Resource", Type: "text"}, {Name: "slot", Title: "Slot", Type: kind}, {Name: "available", Title: "Available", Type: "integer"}}}, txnAt)
			action := func(name string, subtract bool) construction.Action {
				resource := platform.ExprReference("text", "resource")
				return construction.Action{Name: name, Title: name, From: []string{"draft"}, Roles: []string{"builder", "user"}, Transaction: &platform.BusinessTxn{Subject: "build.reservation", Steps: []platform.TxnStep{
					{Name: "slots", Kind: "for-each", ForEach: &platform.TxnForEach{Range: &platform.TxnRange{From: platform.ExprReference(kind, "from"), Until: platform.ExprReference(kind, "until"), Unit: unit, Step: 1, Limit: 7}, Steps: []platform.TxnStep{
						{Name: "capacity", Kind: "post", Target: platform.TxnRef{Type: "build.capacity"}, Post: &platform.TxnPost{Match: []platform.TxnSet{{Field: "resource", Expr: &resource}, {Field: "slot", From: "row.start"}}, Field: "available", Amount: platform.ExprLiteralValue("integer", 1), Subtract: subtract, Floor: true}},
					}}},
					{Name: "finish", Kind: "update", Target: platform.TxnRef{Type: "build.reservation", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", From: "=done"}}},
				}}}
			}
			modify := action("modify", true)
			modify.Inputs = []construction.Input{{Name: "note", Title: "Note", Type: "text", Required: true}}
			modify.Transaction.Steps = []platform.TxnStep{{Name: "amend", Kind: "update", Target: platform.TxnRef{Type: "build.reservation", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "note", From: "note"}}}, modify.Transaction.Steps[0]}
			publishObject(t, tn, "builder", "RESERVATION", map[string]any{"name": "reservation", "title": "Reservation", "fields": []construction.Field{{Name: "resource", Title: "Resource", Type: "text", Required: true}, {Name: "from", Title: "From", Type: kind, Required: true}, {Name: "until", Title: "Until", Type: kind, Required: true}, {Name: "note", Title: "Note", Type: "text"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}, {Name: "done", Title: "Done"}}, "actions": []construction.Action{action("fund", false), action("reserve", true), modify}}, txnAt)
			create := func(id string, from, until int) {
				t.Helper()
				decide(t, tn, "user", "build", "build.reservation.create", "build.reservation", id, map[string]any{"resource": "one", "from": points[from], "until": points[until]}, txnAt)
			}
			create("fund", 0, 3)
			decide(t, tn, "user", "build", "build.reservation.fund", "build.reservation", "fund", nil, txnAt)
			create("first", 1, 2)
			decide(t, tn, "user", "build", "build.reservation.reserve", "build.reservation", "first", nil, txnAt)
			create("second", 0, 2)
			before := txnState(tn)
			if got := refuse(t, tn, "user", "build", "build.reservation.reserve", "build.reservation", "second", nil, txnAt); got == "ok" {
				t.Fatal("later full slot allowed an over-capacity reservation")
			}
			if txnState(tn) != before {
				t.Fatal("a later full slot left an earlier slot or reservation changed")
			}
			if got := refuse(t, tn, "user", "build", "build.reservation.modify", "build.reservation", "second", map[string]any{"note": "Changed before the later refusal"}, txnAt); got == "ok" || txnState(tn) != before {
				t.Fatal("a refused interval change retained an earlier reservation amendment")
			}
			for _, bad := range []struct {
				id          string
				from, until int
			}{{"empty", 0, 0}, {"overlimit", 0, 13}} {
				create(bad.id, bad.from, bad.until)
				before = txnState(tn)
				if got := refuse(t, tn, "user", "build", "build.reservation.reserve", "build.reservation", bad.id, nil, txnAt); got == "ok" || txnState(tn) != before {
					t.Fatal("invalid time range was accepted or left a partial change")
				}
			}
			create("retry", 0, 1)
			request := submission(tn, memberOf(t, tn, "user"), "build", "build.reservation.reserve", "build.reservation", "retry", nil)
			before = txnState(tn)
			fail = true
			if _, err := tn.Submit(memberOf(t, tn, "user"), request, txnAt); err == nil || txnState(tn) != before {
				t.Fatal("append failure exposed a reservation or capacity change")
			}
			answer, err := tn.Submit(memberOf(t, tn, "user"), request, txnAt)
			if err != nil || answer == nil {
				t.Fatalf("time iteration retry: %v", err)
			}
			again, err := tn.Submit(memberOf(t, tn, "user"), request, txnAt)
			if err != nil || again.GetChangeId() != answer.GetChangeId() {
				t.Fatalf("retry changed the accepted identity: %v", err)
			}
			row, refusal := tn.RecordOf(memberOf(t, tn, "user"), "build.capacity", build.BalanceID("build.capacity", []string{"one", points[0]}), txnAt)
			if refusal != nil || !strings.Contains(string(platform.Raw(row.Record)), `"available":0`) {
				t.Fatalf("first slot was not consumed exactly once: %s %v", platform.Raw(row.Record), refusal)
			}
			restored := builderTenant(t, id)
			if err := restored.Replay(entries); err != nil || snapshot(tn) != snapshot(restored) {
				t.Fatalf("time range recovery changed saved results: %v", err)
			}
		})
	}
}

func TestDeclaredPeriodDateEnumeration(t *testing.T) {
	tn := builderTenant(t, "period-dates")
	publishObject(t, tn, "builder", "DAY", map[string]any{"name": "periodday", "title": "Period day", "fields": []construction.Field{{Name: "date", Title: "Date", Type: "date"}, {Name: "ordinal", Title: "Ordinal", Type: "integer"}}}, txnAt)
	publishObject(t, tn, "builder", "PERIOD", map[string]any{"name": "periodcalendar", "title": "Period calendar", "fields": []construction.Field{{Name: "from", Title: "From", Type: "date"}, {Name: "until", Title: "Until", Type: "date"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}}, "actions": []construction.Action{{Name: "expand", Title: "Expand dates", From: []string{"draft"}, Roles: []string{"user"}, Transaction: &platform.BusinessTxn{Subject: "build.periodcalendar", Steps: []platform.TxnStep{{Name: "days", Kind: "for-each", ForEach: &platform.TxnForEach{Range: &platform.TxnRange{From: platform.ExprReference("date", "from"), Until: platform.ExprReference("date", "until"), Unit: "day", Step: 1, Limit: 31}, Steps: []platform.TxnStep{{Name: "day", Kind: "create", Target: platform.TxnRef{Type: "build.periodday"}, Changes: []platform.TxnSet{{Field: "date", From: "row.start"}, {Field: "ordinal", From: "row.index"}}}}}}}}}}}, txnAt)
	decide(t, tn, "user", "build", "build.periodcalendar.create", "build.periodcalendar", "october", map[string]any{"from": "2026-10-01", "until": "2026-11-01"}, txnAt)
	decide(t, tn, "user", "build", "build.periodcalendar.expand", "build.periodcalendar", "october", nil, txnAt)
	rows, failure := tn.Records(memberOf(t, tn, "user"), "build.periodday", platform.Query{Limit: 100}, txnAt)
	if failure != nil || rows.Total != 31 || len(rows.Records) != 31 {
		t.Fatalf("period dates: %+v %v", rows, failure)
	}
}

func TestDeclaredQuantityCreatesStableChildren(t *testing.T) {
	tn := builderTenant(t, "quantity-groups")
	publishObject(t, tn, "builder", "GROUP", map[string]any{"name": "groupplan", "title": "Group plan", "fields": []construction.Field{{Name: "quantity", Title: "Quantity", Type: "integer", Required: true}}, "states": []construction.State{{Name: "draft", Title: "Draft"}, {Name: "split", Title: "Split"}}}, txnAt)
	publishObject(t, tn, "builder", "ITEM", map[string]any{"name": "groupitem", "title": "Group item", "fields": []construction.Field{{Name: "group", Title: "Group", Type: "reference", Ref: "build.groupplan"}, {Name: "ordinal", Title: "Ordinal", Type: "integer"}}}, txnAt)
	transaction := &platform.BusinessTxn{Subject: "build.groupplan", Steps: []platform.TxnStep{
		{Name: "items", Kind: "for-each", ForEach: &platform.TxnForEach{Range: &platform.TxnRange{From: platform.ExprLiteralValue("integer", 0), Until: platform.ExprReference("integer", "quantity"), Unit: "item", Step: 1, Limit: 20}, Steps: []platform.TxnStep{{Name: "item", Kind: "create", Target: platform.TxnRef{Type: "build.groupitem"}, Changes: []platform.TxnSet{{Field: "group", From: "$subject"}, {Field: "ordinal", From: "row.start"}}}}}},
		{Name: "finish", Kind: "update", Target: platform.TxnRef{Type: "build.groupplan", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", From: "=split"}}},
	}}
	decide(t, tn, "builder", "build", "build.object.edit", construction.ObjectType, "GROUP", map[string]any{"actions": []construction.Action{{Name: "split", Title: "Split group", From: []string{"draft"}, Roles: []string{"user"}, Transaction: transaction}}}, txnAt)
	decide(t, tn, "builder", "build", construction.SchemaPublish, construction.ObjectType, "GROUP", nil, txnAt)
	decide(t, tn, "user", "build", "build.groupplan.create", "build.groupplan", "group", map[string]any{"quantity": 3}, txnAt)
	request := submission(tn, memberOf(t, tn, "user"), "build", "build.groupplan.split", "build.groupplan", "group", nil)
	first, failure := tn.Submit(memberOf(t, tn, "user"), request, txnAt)
	if failure != nil {
		t.Fatal(failure)
	}
	again, failure := tn.Submit(memberOf(t, tn, "user"), request, txnAt)
	rows, readFailure := tn.Records(memberOf(t, tn, "user"), "build.groupitem", platform.Query{Limit: 100}, txnAt)
	if failure != nil || readFailure != nil || first.GetChangeId() != again.GetChangeId() || rows.Total != 3 {
		t.Fatalf("quantity split or retry duplicated children: %+v %v %v", rows, failure, readFailure)
	}
}

func TestDeclaredJournalCollectionAndClosedPeriod(t *testing.T) {
	compose := func() *Tenant {
		return composeTenant(t, "journal-lines", []Seat{seatOf("builder", "build:builder", "core:accountant"), seatOf("user", "build:user"), seatOf("limited", "build:limited")}, build.New("journal-lines"), core.New("journal-lines"))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	access := []construction.Access{{Role: "user", Read: "all", Create: true, Edit: true}, {Role: "limited", Read: "all", Create: true, Edit: true}}
	publishObject(t, tn, "builder", "INVOICE", map[string]any{"name": "invoice", "title": "Invoice", "fields": []construction.Field{{Name: "date", Title: "Date", Type: "date"}}, "states": []construction.State{{Name: "draft", Title: "Draft"}, {Name: "submitted", Title: "Submitted"}}, "access": access}, txnAt)
	publishObject(t, tn, "builder", "LINE", map[string]any{"name": "invoiceline", "title": "Invoice line", "fields": []construction.Field{{Name: "invoice", Title: "Invoice", Type: "reference", Ref: "build.invoice", Inverse: "lines"}, {Name: "account", Title: "Account", Type: "text"}, {Name: "side", Title: "Side", Type: "choice", Choices: "debit,credit"}, {Name: "amount", Title: "Amount", Type: "decimal", Read: []string{"builder", "user"}}}, "access": access}, txnAt)
	rowAmount := platform.ExprReference("decimal", "row.amount")
	side := func(value string) *platform.Expr {
		return booleanRule("=", platform.ExprReference("choice", "row.side"), platform.ExprLiteralValue("choice", value))
	}
	post := func(name, policy string) construction.Action {
		date := platform.ExprReference("date", "date")
		return construction.Action{Name: name, Title: name, From: []string{"draft"}, Roles: []string{"user", "limited", "builder"}, Transaction: &platform.BusinessTxn{Subject: "build.invoice", Steps: []platform.TxnStep{
			{Name: "books", Kind: "journal", Journal: &platform.TxnJournal{Set: "lines", Date: &date, ClosedPeriod: policy, Lines: []platform.TxnJournalLine{{When: side("debit"), Account: platform.ExprReference("text", "row.account"), Debit: &rowAmount}, {When: side("credit"), Account: platform.ExprReference("text", "row.account"), Credit: &rowAmount}}}},
			{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.invoice", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", From: "=submitted"}}},
		}}}
	}
	decide(t, tn, "builder", "build", "build.object.edit", construction.ObjectType, "INVOICE", map[string]any{"sets": []platform.RecordSet{{Name: "lines", From: "lines", Direction: "inverse", Object: "build.invoiceline", Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}, Limit: 100}}, "actions": []construction.Action{post("post", "reject"), post("shift", "next-open")}}, txnAt)
	decide(t, tn, "builder", "build", construction.SchemaPublish, construction.ObjectType, "INVOICE", nil, txnAt)
	create := func(id, date string) {
		t.Helper()
		decide(t, tn, "user", "build", "build.invoice.create", "build.invoice", id, map[string]any{"date": date}, txnAt)
		for i, line := range []struct{ account, side, amount string }{{"1403", "debit", "0.1"}, {"1403", "debit", "0.2"}, {"2290", "credit", "0.3"}} {
			decide(t, tn, "user", "build", "build.invoiceline.create", "build.invoiceline", fmt.Sprintf("%s-%d", id, i), map[string]any{"invoice": id, "account": line.account, "side": line.side, "amount": json.Number(line.amount)}, txnAt)
		}
	}
	decide(t, tn, "user", "build", "build.invoice.create", "build.invoice", "empty", map[string]any{"date": "2026-10-12"}, txnAt)
	before := txnState(tn)
	if got := refuse(t, tn, "user", "build", "build.invoice.post", "build.invoice", "empty", nil, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("missing accounting lines produced a journal intent")
	}
	create("open", "2026-10-12")
	before = txnState(tn)
	if got := refuse(t, tn, "limited", "build", "build.invoice.post", "build.invoice", "open", nil, txnAt); got == "ok" || txnState(tn) != before {
		t.Fatal("withheld accounting lines produced a partial journal")
	}
	decide(t, tn, "user", "build", "build.invoice.post", "build.invoice", "open", nil, txnAt)
	if journals, _, _ := platform.Find[platform.Journal](tn.automation(core.ID, false), platform.Query{}); len(journals) != 0 {
		t.Fatal("the transaction wrote into the independent books authority")
	}
	tn.Dispatch(txnAt)
	journals, _, failure := platform.Find[platform.Journal](tn.automation(core.ID, false), platform.Query{})
	if failure != nil || len(journals) != 1 || len(journals[0].Lines) != 3 || journals[0].Total != 0.3 || journals[0].ClosedPeriod != "reject" {
		t.Fatalf("complete collection did not create one balanced entry: %+v %v", journals, failure)
	}
	decide(t, tn, "builder", core.ID, core.PeriodType+".close", core.PeriodType, "per-2026-09", nil, txnAt)
	create("closed", "2026-09-12")
	decide(t, tn, "user", "build", "build.invoice.post", "build.invoice", "closed", nil, txnAt)
	decide(t, tn, "builder", "build", "build.object.edit", construction.ObjectType, "INVOICE", map[string]any{"actions": []construction.Action{post("post", "next-open"), post("shift", "next-open")}}, txnAt)
	decide(t, tn, "builder", "build", construction.SchemaPublish, construction.ObjectType, "INVOICE", nil, txnAt)
	tn.Dispatch(txnAt)
	create("shifted", "2026-09-12")
	decide(t, tn, "user", "build", "build.invoice.shift", "build.invoice", "shifted", nil, txnAt)
	tn.Dispatch(txnAt)
	journals, _, failure = platform.Find[platform.Journal](tn.automation(core.ID, false), platform.Query{})
	if failure != nil || len(journals) != 2 {
		t.Fatalf("reject/shift policies were lost in delivery: %+v %v", journals, failure)
	}
	shifted := false
	for _, journal := range journals {
		shifted = shifted || journal.Date == "2026-09-12" && journal.Period == "2026-10" && journal.Shifted
	}
	if !shifted {
		t.Fatal("closed-period shift did not preserve its original date and actual period")
	}
	numbers := map[string]bool{}
	for _, journal := range journals {
		numbers[journal.Number] = true
	}
	if !numbers["2026-10-0001"] || !numbers["2026-10-0002"] {
		t.Fatalf("a refusal consumed or repeated a journal number: %+v", numbers)
	}
	restored := compose()
	if err := restored.Replay(entries); err != nil || snapshot(tn) != snapshot(restored) {
		t.Fatalf("accounting collection recovery changed saved facts: %v", err)
	}
}
