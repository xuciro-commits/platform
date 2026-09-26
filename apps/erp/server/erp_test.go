package erp

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

type books struct {
	t       *testing.T
	tn      *platformserver.Tenant
	journal []platformserver.Entry
	now     time.Time
	keys    int
}

func newBooks(t *testing.T) *books {
	b := &books{t: t, now: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)}
	b.tn = build(t)
	b.tn.Record = func(e platformserver.Entry) { b.journal = append(b.journal, e) }
	return b
}

func build(t *testing.T) *platformserver.Tenant {
	seat := func(id, role string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{ID: role}}}
	}
	controller := seat("cy", Controller)
	controller.Roles[platformserver.PlatformApp] = platformserver.Admin // sets the tenant's currency
	tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t", seat("ada", Accountant), controller, seat("bo", Buyer)),
		platformserver.NewWork("t"), platformserver.NewFlows("t"), New("t"))
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func (b *books) do(who, schema, typ, id string, payload any) string {
	return b.as(ID, who, schema, typ, id, payload)
}

func (b *books) as(authority, who, schema, typ, id string, payload any) string {
	b.keys++
	m, _ := b.tn.Member(who)
	raw, _ := json.Marshal(payload)
	r, err := b.tn.Submit(m, &pb.Submission{TenantId: "t", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", b.keys),
		Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, b.now)
	switch {
	case err != nil:
		return err.Error()
	case r.GetSubmission().GetSchema().GetName() != schema: // held for approval (ADR-0017)
		return r.GetSubmission().GetSchema().GetName()
	}
	return "ok"
}

func (b *books) expect(what, got, want string) {
	b.t.Helper()
	if got != want {
		b.t.Fatalf("%s: got %s, want %s", what, got, want)
	}
}

func (b *books) entry(id string) Entry {
	m, _ := b.tn.Member("cy")
	v, err := b.tn.RecordOf(m, EntryType, id, b.now)
	if err != nil {
		b.t.Fatalf("entry %s: %v", id, err)
	}
	return v.Record.(Entry)
}

func line(account string, debit, credit int64) map[string]any {
	return map[string]any{"account": account, "debit": map[string]any{"amount": debit}, "credit": map[string]any{"amount": credit}}
}

// An accountant drafts and posts entries; posting checks balance, accounts and
// the period, takes a number only when accepted, and writes postings; a posted
// entry is final and corrected by reversal; a closed period refuses postings;
// the trial balance adds the postings up; replay rebuilds all of it, numbers included.
func TestBooks(t *testing.T) {
	b := newBooks(t)
	b.expect("the books' currency", b.as(platformserver.PlatformApp, "cy", platformserver.SchemaSettingSet, platformserver.SettingType,
		platformserver.PlatformApp+"/"+platformserver.SettingCurrency, map[string]string{"value": "CNY"}), "ok")
	for _, a := range Chart() {
		b.expect("account "+a.ID, b.do("cy", AccountType+".create", AccountType, a.ID, map[string]string{"name": a.Name, "kind": a.Kind}), "ok")
	}
	b.expect("an accountant keeps no chart", b.do("ada", AccountType+".create", AccountType, "9999", map[string]string{"name": "Mine", "kind": "asset"}), "ERROR_CODE_POLICY_DENIED")
	b.expect("period", b.do("cy", SchemaPeriodOpen, PeriodType, "2026-10", map[string]any{}), "ok")
	b.expect("a period is a month", b.do("cy", SchemaPeriodOpen, PeriodType, "2026-13", map[string]any{}), "ERROR_CODE_INVALID_ARGUMENT")

	draft := func(id, date string, lines ...map[string]any) {
		t.Helper()
		b.expect("draft "+id, b.do("ada", EntryType+".create", EntryType, id, map[string]any{"journal": "general", "date": date, "reference": "capital", "lines": lines}), "ok")
	}
	post := func(id string) string { return b.do("ada", EntryType+".post", EntryType, id, map[string]any{}) }

	draft("E-1", "2026-10-01", line("1002", 100000, 0), line("4001", 0, 90000))
	b.expect("unbalanced", post("E-1"), "ERROR_CODE_INVALID_ARGUMENT")
	b.expect("no number for a refusal", b.entry("E-1").Number, "")
	b.expect("fix the draft", b.do("ada", EntryType+".edit", EntryType, "E-1", map[string]any{"lines": []any{line("1002", 100000, 0), line("4001", 0, 100000)}}), "ok")
	b.expect("post", post("E-1"), "ok")
	e1 := b.entry("E-1")
	b.expect("posted", fmt.Sprint(e1.State, " ", e1.Number, " ", e1.Lines[0].Debit.Currency), "posted GJ/2026/00001 CNY")
	b.expect("a posted entry is final", b.do("ada", EntryType+".edit", EntryType, "E-1", map[string]any{"reference": "changed"}), "ERROR_CODE_POLICY_DENIED")

	draft("E-2", "2026-10-02", line("1403", 30000, 0), line("1002", 0, 30000))
	draft("E-X", "2026-10-02", line("1403", 500, 0), line("1403", 0, 0))
	b.expect("a line has one side", post("E-X"), "ERROR_CODE_INVALID_ARGUMENT")
	draft("E-Y", "2026-11-02", line("1403", 500, 0), line("1002", 0, 500))
	b.expect("no open period", post("E-Y"), "ERROR_CODE_INVALID_ARGUMENT")
	b.expect("post E-2", post("E-2"), "ok")
	b.expect("gapless", b.entry("E-2").Number, "GJ/2026/00002")

	b.expect("reverse", b.do("ada", EntryType+".reverse", EntryType, "E-2", map[string]string{"date": "2026-10-03"}), "ok")
	r := b.entry("E-2-R")
	b.expect("reversal", fmt.Sprint(b.entry("E-2").State, " ", r.State, " ", r.Number, " ", r.Date, " ", r.Reverses, " ", r.Lines[0].Credit.Amount), "reversed posted GJ/2026/00003 2026-10-03 E-2 30000")

	b.expect("close", b.do("cy", PeriodType+".close", PeriodType, "2026-10", map[string]any{}), "ok")
	draft("E-3", "2026-10-04", line("6602", 1000, 0), line("1001", 0, 1000))
	b.expect("closed period", post("E-3"), "ERROR_CODE_INVALID_ARGUMENT")
	b.expect("reopen", b.do("cy", PeriodType+".reopen", PeriodType, "2026-10", map[string]any{}), "ok")
	b.expect("post E-3", post("E-3"), "ok")

	m, _ := b.tn.Member("ada")
	out, err := b.tn.Read(m, TrialBalance)
	if err != nil {
		t.Fatal(err)
	}
	var debit, credit int64
	got := ""
	for _, x := range out.([]Balance) {
		debit, credit = debit+x.Debit, credit+x.Credit
		got += fmt.Sprintf("%s %d; ", x.Account, x.Balance)
	}
	b.expect("trial balance", fmt.Sprint(got, debit == credit), "1001 -1000; 1002 100000; 1403 0; 4001 -100000; 6602 1000; true")
	platformserver.CheckReplay(t, b.tn, b.journal, func() *platformserver.Tenant { return build(t) })
}

// Every text of the app reads in Simplified Chinese (AGENTS.md rule 10).
func TestChinese(t *testing.T) {
	tn := build(t)
	if missing := tn.Untranslated(ID, "zh-CN"); len(missing) > 0 {
		t.Errorf("add to i18n/zh-CN.json: %q", missing)
	}
}
