package core

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The books (ADR-0076, ADR-0069 Ⅱ): the transaction skeleton SAP keeps in
// code, as shared configuration every application posts into. A chart of
// accounts, fiscal periods that open and close, and journal entries - the
// double-entry record of what happened in money. Applications do not write
// journals by hand: a builder action declares what it posts (apps/build
// Journal), and the host lands one balanced entry per accepted decision,
// idempotent by the decision's change id. Stock, receivables and payables
// stay subledgers on the applications' own balances (ADR-0063); the journal
// is the general ledger they summarise into.
const (
	AccountType = "core.account"
	PeriodType  = "core.period"
	JournalType = "core.journal"
	// Accountant keeps the chart and the periods, posts and reverses by hand; Steward may read.
	Accountant = "accountant"
	// ReadTrialBalance is the per-account debit and credit of a period.
	ReadTrialBalance = "core.trial-balance"
)

// Account is one line of the chart of accounts.
type Account struct {
	platform.Record
	Code   string `json:"code" field:"required,search" help:"Unique, sorts the chart, e.g. 1403"`
	Name   string `json:"name" field:"required,search"`
	Kind   string `json:"kind" field:"required" choices:"asset,liability,equity,revenue,expense"`
	Parent string `json:"parent,omitempty" title:"Under" help:"The summary account's code"`
	// Posting says whether entries hit it directly; summary accounts only total.
	Posting bool `json:"posting" title:"Postable"`
	Active  bool `json:"active" title:"Active"`
}

// Period is a fiscal period. Entries dated inside it belong to it while it is
// open; once closed, a late entry goes to the next open period and says so.
type Period struct {
	platform.Record
	Code  string `json:"code" field:"required,search" help:"e.g. 2026-10"`
	Year  int    `json:"year" field:"required"`
	From  string `json:"from" field:"required" title:"First day" help:"YYYY-MM-DD"`
	To    string `json:"to" field:"required" title:"Last day" help:"YYYY-MM-DD, inclusive"`
	State string `json:"state" field:"readonly"`
}

// Journal is one balanced entry: lines whose debits equal their credits.
type Journal struct {
	platform.Record
	Number string `json:"number,omitempty" field:"readonly,search" title:"Document number"`
	Date   string `json:"date" field:"required" title:"Posting date" help:"YYYY-MM-DD"`
	Period string `json:"period,omitempty" field:"readonly" title:"Period"`
	// Shifted tells that the date's period was closed and the entry went to Period instead.
	Shifted  bool          `json:"shifted,omitempty" field:"readonly" title:"Posted to a later period"`
	Text     string        `json:"text,omitempty" field:"search"`
	Currency string        `json:"currency,omitempty" help:"ISO 4217; default the tenant's first currency"`
	Lines    []JournalLine `json:"lines" field:"required" type:"json" title:"Lines"`
	Total    float64       `json:"total,omitempty" field:"readonly" title:"Total debit"`
	// Source is the decision that caused it - "<app>/<change id>" - or empty when posted by hand.
	Source   string `json:"source,omitempty" field:"search" help:"The decision that posted it, set by the platform"`
	Reverses string `json:"reverses,omitempty" field:"readonly" title:"Reverses"`
	Reversed string `json:"reversed,omitempty" field:"readonly" title:"Reversed by"`
	State    string `json:"state" field:"readonly"`
}

type JournalLine struct {
	Account string  `json:"account"` // the account's code
	Debit   float64 `json:"debit,omitempty"`
	Credit  float64 `json:"credit,omitempty"`
	Text    string  `json:"text,omitempty"`
	// Object and Partner tie the line to what it is about: a material, a site, a business partner.
	Object  string `json:"object,omitempty"`
	Partner string `json:"partner,omitempty"`
}

func refuse(message string, args ...any) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
}

func bookEntities() []platform.Entity {
	books := platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Accountant}}
	return []platform.Entity{
		{Type: AccountType, Title: "Account", Model: Account{}, Display: "name", Synonyms: "GL account,ledger account,chart of accounts",
			Description: "One line of the chart of accounts; entries post to postable accounts, summary accounts total them.", Standard: books, Implements: []string{Coded},
			Validate: validateAccount},
		{Type: PeriodType, Title: "Fiscal period", Model: Period{}, Display: "code", Synonyms: "accounting period,month,close",
			Description: "A fiscal period entries belong to; closing it sends late entries to the next open one.", Standard: books, Validate: validatePeriod,
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "open", States: []platform.State{{Name: "open", Title: "Open", Tone: "success"}, {Name: "closed", Title: "Closed", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "close", Title: "Close", Description: "No more entries in it; later ones go to the next open period.", From: []string{"open"}, To: []string{"closed"}, Roles: []string{Accountant}, Payload: []platform.Field{}},
					{Name: "reopen", Title: "Reopen", Description: "Accept entries again.", From: []string{"closed"}, To: []string{"open"}, Roles: []string{Accountant}, Payload: []platform.Field{}}}}},
		{Type: JournalType, Title: "Journal entry", Model: Journal{}, Display: "number", Synonyms: "accounting document,voucher,posting",
			Description: "A balanced double-entry record of what happened in money; applications' actions post them, people reverse them.",
			Standard:    platform.Standard{Create: true, Roles: []string{Accountant}}, Compute: computeJournal, Validate: validateJournal,
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "posted", States: []platform.State{{Name: "posted", Title: "Posted", Tone: "success"}, {Name: "reversed", Title: "Reversed", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "reverse", Title: "Reverse", Description: "Post the opposite entry today; nothing is deleted.", From: []string{"posted"}, To: []string{"reversed"}, Roles: []string{Accountant},
						Payload: []platform.Field{{Name: "date", Type: "string", Description: "The reversal's posting date; default today"}}, Do: reverseJournal}}}},
	}
}

func validateAccount(c platform.Caller, record any) *kernel.Error {
	a := record.(*Account)
	if a.Code == "" {
		return refuse("An account has a code")
	}
	others, _, _ := platform.Find[Account](c, platform.Query{Limit: 2000})
	for _, o := range others {
		if o.ID != a.ID && o.Code == a.Code && !o.Archived {
			return refuse("Account {code} already exists", a.Code)
		}
	}
	if a.Parent != "" && !slices.ContainsFunc(others, func(o Account) bool { return o.Code == a.Parent }) {
		return refuse("The summary account {code} does not exist", a.Parent)
	}
	return nil
}

func day(s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	return t, err == nil
}

func validatePeriod(c platform.Caller, record any) *kernel.Error {
	p := record.(*Period)
	from, ok1 := day(p.From)
	to, ok2 := day(p.To)
	if !ok1 || !ok2 || to.Before(from) {
		return refuse("A period runs from a first day to a last day, YYYY-MM-DD")
	}
	others, _, _ := platform.Find[Period](c, platform.Query{Limit: 500})
	for _, o := range others {
		if o.ID == p.ID || o.Archived {
			continue
		}
		if o.Code == p.Code {
			return refuse("Period {code} already exists", p.Code)
		}
		if of, _ := day(o.From); !from.After(mustDay(o.To)) && !to.Before(of) {
			return refuse("Period {code} overlaps {other}", p.Code, o.Code)
		}
	}
	return nil
}

func mustDay(s string) time.Time { t, _ := day(s); return t }

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func computeJournal(record any) {
	j := record.(*Journal)
	total := 0.0
	for _, l := range j.Lines {
		total += l.Debit
	}
	j.Total = round2(total)
	if j.State == "" {
		j.State = "posted"
	}
}

// validateJournal is the whole discipline of the books: balanced lines on
// postable accounts, dated into an open period - or the next one, visibly.
func validateJournal(c platform.Caller, record any) *kernel.Error {
	j := record.(*Journal)
	if len(j.Lines) < 2 {
		return refuse("An entry has at least two lines")
	}
	accounts, _, _ := platform.Find[Account](c, platform.Query{Limit: 2000})
	debit, credit := 0.0, 0.0
	for i, l := range j.Lines {
		if l.Debit < 0 || l.Credit < 0 || (l.Debit == 0) == (l.Credit == 0) {
			return refuse("Line {n} has either a debit or a credit, above zero", i+1)
		}
		k := slices.IndexFunc(accounts, func(a Account) bool { return a.Code == l.Account && !a.Archived })
		if k < 0 {
			return refuse("Line {n}: account {code} does not exist", i+1, l.Account)
		}
		if !accounts[k].Posting || !accounts[k].Active {
			return refuse("Line {n}: account {code} is not postable", i+1, l.Account)
		}
		debit, credit = debit+l.Debit, credit+l.Credit
	}
	if round2(debit) != round2(credit) {
		return refuse("Debits {debit} and credits {credit} do not balance", fmt.Sprint(round2(debit)), fmt.Sprint(round2(credit)))
	}
	date, ok := day(j.Date)
	if !ok {
		return refuse("The posting date is YYYY-MM-DD")
	}
	if j.Period != "" && j.Revision > 0 {
		return nil // an existing entry keeps its period
	}
	periods, _, _ := platform.Find[Period](c, platform.Query{Limit: 500})
	periods = slices.DeleteFunc(periods, func(p Period) bool { return p.Archived })
	sort.Slice(periods, func(a, b int) bool { return periods[a].From < periods[b].From })
	own := slices.IndexFunc(periods, func(p Period) bool { return !date.Before(mustDay(p.From)) && !date.After(mustDay(p.To)) })
	if own < 0 {
		return refuse("No fiscal period covers {date}", j.Date)
	}
	if periods[own].State == "open" {
		j.Period, j.Shifted = periods[own].Code, false
		j.Number = nextNumber(c, j.Period)
		return nil
	}
	for _, p := range periods[own+1:] {
		if p.State == "open" {
			j.Period, j.Shifted = p.Code, true
			j.Number = nextNumber(c, j.Period)
			return nil
		}
	}
	return refuse("Period {period} is closed and no later period is open", periods[own].Code)
}

func reverseJournal(c platform.Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error {
	j := record.(*Journal)
	var p struct {
		Date string `json:"date"`
	}
	json.Unmarshal(payload, &p)
	if p.Date == "" {
		p.Date = now.UTC().Format(time.DateOnly)
	}
	rev := Journal{Record: platform.Record{ID: j.ID + "-rev"}, Date: p.Date, Text: "Reversal of " + firstNonEmpty(j.Number, j.ID) + " " + j.Text, Currency: j.Currency, Reverses: j.ID, Source: j.Source}
	for _, l := range j.Lines {
		rev.Lines = append(rev.Lines, JournalLine{Account: l.Account, Debit: l.Credit, Credit: l.Debit, Text: l.Text, Object: l.Object, Partner: l.Partner})
	}
	computeJournal(&rev)
	if err := validateJournal(c, &rev); err != nil {
		return err
	}
	if err := c.Put(nil, rev); err != nil {
		return err
	}
	j.Reversed = rev.ID
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// nextNumber numbers an entry within its period: <period>-<n>, gapless in
// order of posting. The chart's own numbering ranges for documents live with
// the applications (apps/build Numbering); the journal's is this one.
func nextNumber(c platform.Caller, period string) string {
	entries, _, _ := platform.Find[Journal](c, platform.Query{Domain: json.RawMessage(`[["period","=",` + strconv(period) + `]]`), Limit: 5000})
	return fmt.Sprintf("%s-%04d", period, len(entries)+1)
}

func strconv(s string) string { b, _ := json.Marshal(s); return string(b) }

// TrialBalance is the per-account total of a period.
type TrialBalance struct {
	Period string             `json:"period"`
	Rows   []TrialBalanceLine `json:"rows"`
	Debit  float64            `json:"debit"`
	Credit float64            `json:"credit"`
}

type TrialBalanceLine struct {
	Account string  `json:"account"`
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Debit   float64 `json:"debit"`
	Credit  float64 `json:"credit"`
	Balance float64 `json:"balance"` // debit minus credit
	Entries int     `json:"entries"`
}

func trialBalance(c platform.Caller, period string) TrialBalance {
	accounts, _, _ := platform.Find[Account](c, platform.Query{Limit: 2000})
	entries, _, _ := platform.Find[Journal](c, platform.Query{Limit: 5000})
	sums := map[string]*TrialBalanceLine{}
	for _, j := range entries {
		if j.Archived || period != "" && j.Period != period {
			continue
		}
		for _, l := range j.Lines {
			row := sums[l.Account]
			if row == nil {
				row = &TrialBalanceLine{Account: l.Account}
				if k := slices.IndexFunc(accounts, func(a Account) bool { return a.Code == l.Account }); k >= 0 {
					row.Name, row.Kind = accounts[k].Name, accounts[k].Kind
				}
				sums[l.Account] = row
			}
			row.Debit, row.Credit, row.Entries = round2(row.Debit+l.Debit), round2(row.Credit+l.Credit), row.Entries+1
		}
	}
	out := TrialBalance{Period: period}
	for _, row := range sums {
		row.Balance = round2(row.Debit - row.Credit)
		out.Rows = append(out.Rows, *row)
		out.Debit, out.Credit = round2(out.Debit+row.Debit), round2(out.Credit+row.Credit)
	}
	sort.Slice(out.Rows, func(a, b int) bool { return out.Rows[a].Account < out.Rows[b].Account })
	return out
}

// DefaultAccounts is a small chart a tenant can start from: enough for goods
// receipts, invoices and payments; stewards extend it.
func DefaultAccounts() []any {
	mk := func(code, name, kind, parent string, posting bool) Account {
		return Account{Record: platform.Record{ID: "acc-" + code}, Code: code, Name: name, Kind: kind, Parent: parent, Posting: posting, Active: true}
	}
	return []any{
		mk("1000", "Assets", "asset", "", false), mk("1001", "Bank", "asset", "1000", true), mk("1122", "Receivables", "asset", "1000", true), mk("1403", "Raw materials", "asset", "1000", true), mk("1405", "Finished goods", "asset", "1000", true),
		mk("2000", "Liabilities", "liability", "", false), mk("2202", "Payables", "liability", "2000", true), mk("2221", "Tax payable", "liability", "2000", true), mk("2290", "GR/IR clearing", "liability", "2000", true),
		mk("4000", "Equity", "equity", "", false), mk("4001", "Capital", "equity", "4000", true),
		mk("6000", "Revenue", "revenue", "", false), mk("6001", "Sales", "revenue", "6000", true),
		mk("6400", "Expenses", "expense", "", false), mk("6401", "Cost of goods sold", "expense", "6400", true), mk("6601", "Operating expenses", "expense", "6400", true),
	}
}

// DefaultPeriods are the twelve months of year.
func DefaultPeriods(year int) []any {
	var out []any
	for m := time.January; m <= time.December; m++ {
		first := time.Date(year, m, 1, 0, 0, 0, 0, time.UTC)
		last := first.AddDate(0, 1, -1)
		code := first.Format("2006-01")
		out = append(out, Period{Record: platform.Record{ID: "per-" + code}, Code: code, Year: year, From: first.Format(time.DateOnly), To: last.Format(time.DateOnly), State: "open"})
	}
	return out
}

var _ = strings.TrimSpace
