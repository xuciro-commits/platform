// Package erp is the ERP app (ADR-0024): accounting first — a chart of
// accounts, journal entries posted with numbers and without gaps, reversal,
// and periods that close. Scaffolded by capabilities/server/cmd/new-app.
package erp

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const (
	ID          = "erp"
	AccountType = "erp.account"
	EntryType   = "erp.entry"
	PostingType = "erp.posting"
	PeriodType  = "erp.period"

	SchemaPeriodOpen = "erp.period.open"

	Accountant = "accountant" // drafts, posts and reverses entries
	Controller = "controller" // also keeps the chart of accounts and closes periods

	TrialBalance = "trial-balance" // the read
)

// Journals, each numbered by its own sequence per year.
var Journals = []string{"general", "purchases", "production"}

// Account is one account of the chart; its ID is its code ("1400").
type Account struct {
	platform.Record
	Name string `json:"name" field:"required,search" example:"Raw materials"`
	Kind string `json:"kind" field:"required" choices:"asset,liability,equity,income,expense" help:"Where the account is reported; assets, liabilities and equity on the balance sheet, income and expenses in profit and loss"`
}

// Line is one line of a journal entry: an amount on the debit or the credit side of an account.
type Line struct {
	Account platform.Ref[Account] `json:"account" field:"required"`
	Debit   platform.Money        `json:"debit"`
	Credit  platform.Money        `json:"credit"`
	Text    string                `json:"text,omitempty"`
}

// Entry is a journal entry: drafted, then posted, which is final; a posted
// entry is corrected only by reversing it (D5).
type Entry struct {
	platform.Record
	Number     string `json:"number,omitempty" field:"readonly,search" help:"Given when the entry is posted, per journal and year, without gaps" example:"GJ/2026/00001"`
	Journal    string `json:"journal" field:"required" choices:"general,purchases,production"`
	Date       string `json:"date" field:"required" type:"date" help:"The accounting date; its period must be open to post"`
	Reference  string `json:"reference,omitempty" field:"search" help:"What the entry is about, such as an invoice, a receipt or an order"`
	Lines      []Line `json:"lines" help:"Debits and credits; they must balance to post"`
	State      string `json:"state" field:"readonly" choices:"draft,posted,reversed"`
	Reverses   string `json:"reverses,omitempty" field:"readonly" title:"Reverses" help:"The entry this one reverses"`
	ReversedBy string `json:"reversedBy,omitempty" field:"readonly" title:"Reversed by"`
}

// Posting is one posted line, kept apart so balances are aggregates over
// postings (D2); nobody edits one.
type Posting struct {
	platform.Record
	Entry   platform.Ref[Entry]   `json:"entry" field:"readonly"`
	Number  string                `json:"number" field:"readonly,search"`
	Journal string                `json:"journal" field:"readonly" choices:"general,purchases,production"`
	Date    string                `json:"date" field:"readonly" type:"date"`
	Account platform.Ref[Account] `json:"account" field:"readonly"`
	Debit   platform.Money        `json:"debit" field:"readonly"`
	Credit  platform.Money        `json:"credit" field:"readonly"`
	Text    string                `json:"text,omitempty" field:"readonly"`
}

// Period is an accounting month ("2026-10"): postings dated in it need it open (D4).
type Period struct {
	platform.Record
	State string `json:"state" field:"readonly" choices:"open,closed"`
}

var month = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

func Entities() []platform.Entity {
	both := []string{Accountant, Controller}
	return []platform.Entity{
		{Type: AccountType, Title: "Account", Model: Account{}, Synonyms: "ledger account, GL account",
			Description: "An account of the chart of accounts; its ID is its code. Its balance is the sum of its postings.",
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Controller}}},
		{Type: EntryType, Title: "Journal entry", Plural: "Journal entries", Model: Entry{}, Synonyms: "voucher, journal voucher",
			Description: "A journal entry: debits and credits that balance. Posting makes it final and numbers it; a mistake is corrected by reversing it.",
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: both},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
				States: []platform.State{
					{Name: "draft", Title: "Draft", Tone: "info", Description: "Being prepared; it counts nowhere yet"},
					{Name: "posted", Title: "Posted", Tone: "success", Description: "Final: its lines are postings on its accounts"},
					{Name: "reversed", Title: "Reversed", Tone: "neutral", Description: "Posted, then cancelled by a reversing entry"}},
				Transitions: []platform.Transition{
					{Name: "post", Title: "Post", Description: "Post the entry: its debits and credits must balance and its date's period be open; it takes its number.",
						From: []string{"draft"}, To: []string{"posted"}, Roles: both, Do: post, After: posted},
					{Name: "reverse", Title: "Reverse", Description: "Cancel a posted entry by posting a reversing one with debits and credits swapped, dated the given day (default: the entry's date).",
						From: []string{"posted"}, To: []string{"reversed"}, Roles: both,
						Payload: []platform.Field{{Name: "date", Type: "date", Description: "Date of the reversing entry"}}, Do: reverse, After: reversed}}}},
		{Type: PostingType, Title: "Posting", Model: Posting{}, Synonyms: "ledger line, journal item",
			Description: "A posted line of a journal entry on one account; balances and the trial balance add them up."},
		{Type: PeriodType, Title: "Period", Model: Period{},
			Description: "An accounting month; entries dated in it post only while it is open.",
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "open",
				States: []platform.State{{Name: "open", Title: "Open", Tone: "success"}, {Name: "closed", Title: "Closed", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "close", Title: "Close", Description: "Close the period: nothing dated in it posts until it is reopened.",
						From: []string{"open"}, To: []string{"closed"}, Roles: []string{Controller}},
					{Name: "reopen", Title: "Reopen", Description: "Reopen a closed period for corrections.",
						From: []string{"closed"}, To: []string{"open"}, Roles: []string{Controller}}}}},
	}
}

func Actions() *platform.Catalog {
	actions := []platform.Action{{Schema: SchemaPeriodOpen, Target: PeriodType, Capability: "periods", Title: "Open period",
		Description: "Open an accounting month for posting; its ID is the month, 2026-10.", Roles: []string{Controller}, Payload: []platform.Field{}}}
	for _, e := range Entities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	return platform.NewCatalog(actions...)
}

// Translations: i18n/<language>.json, keyed by the English text (ADR-0023).
//
//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

// App is the ERP in one tenant; the host keeps its records (ADR-0016).
type App struct {
	mu     sync.Mutex
	ledger *platform.Ledger
}

func New(tenant string) *App {
	return &App{ledger: platform.NewLedger(tenant, ID, Actions(), AccountType, EntryType, PostingType, PeriodType)}
}

func (a *App) Manifest() platform.Manifest {
	sequences := make([]platform.Sequence, len(Journals))
	for i, j := range Journals {
		sequences[i] = platform.Sequence{Name: "entry." + j, Pattern: strings.ToUpper(j[:1]) + "J/{year}/{n:5}", Yearly: true}
	}
	return platform.Manifest{ID: ID, Title: "ERP", Version: "1", Actions: a.ledger.Catalog, Entities: Entities(), Languages: languages,
		Reads: []string{TrialBalance}, Sequences: sequences}
}

func (a *App) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *App) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *App) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }

func (a *App) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (a *App) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A posted entry is final: only a draft is edited or archived (D5).
	draft := func() bool {
		schema := s.GetSchema().GetName()
		if schema != EntryType+".edit" && schema != EntryType+".archive" {
			return true
		}
		e, _ := platform.Get[Entry](c, s.GetTarget().GetId())
		return e.State == "draft"
	}
	if record, err, ok := a.ledger.Generated(c, s, now, draft, Entities()...); ok {
		return record, err
	}
	return a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		id := s.GetTarget().GetId()
		if s.GetSchema().GetName() != SchemaPeriodOpen || !month.MatchString(id) {
			return nil, invalid()
		}
		if _, known := platform.Get[Period](c, id); known {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		return func(r *pb.ChangeRecord) { c.Put(r, Period{Record: platform.Record{ID: id}}) }, nil
	})
}

func invalid() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT} }

// post checks an entry before it is posted: at least two lines, each on an
// active account with one positive side in the tenant's currency (the books'),
// debits equal to credits, and its date in an open period.
func post(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	e := record.(*Entry)
	currency := c.Setting("platform/currency")
	if len(e.Lines) < 2 || !open(c, e.Date) {
		return invalid()
	}
	var debit, credit int64
	for i := range e.Lines {
		l := &e.Lines[i]
		account, known := platform.Get[Account](c, string(l.Account))
		if !known || account.Archived || l.Debit.Amount < 0 || l.Credit.Amount < 0 || (l.Debit.Amount > 0) == (l.Credit.Amount > 0) {
			return invalid()
		}
		for _, m := range []*platform.Money{&l.Debit, &l.Credit} {
			if m.Currency == "" {
				m.Currency = currency
			}
			if m.Currency != currency {
				return invalid()
			}
		}
		debit, credit = debit+l.Debit.Amount, credit+l.Credit.Amount
	}
	if debit != credit {
		return invalid()
	}
	return nil
}

// open tells whether date ("2026-10-01") lies in an open period.
func open(c platform.Caller, date string) bool {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return false
	}
	p, known := platform.Get[Period](c, date[:7])
	return known && !p.Archived && p.State == "open"
}

// posted numbers the entry and writes its postings, in the decision that posted it.
func posted(c platform.Caller, r *pb.ChangeRecord, record any, _ time.Time) {
	book(c, r, *record.(*Entry))
}

// book takes an entry's number and puts it with one posting per line.
func book(c platform.Caller, r *pb.ChangeRecord, e Entry) {
	date, _ := time.Parse(time.DateOnly, e.Date)
	e.Number, _ = c.Next(r, "entry."+e.Journal, date)
	c.Put(r, e)
	for i, l := range e.Lines {
		c.Put(r, Posting{Record: platform.Record{ID: fmt.Sprintf("%s.%d", e.ID, i+1)}, Entry: platform.Ref[Entry](e.ID), Number: e.Number,
			Journal: e.Journal, Date: e.Date, Account: l.Account, Debit: l.Debit, Credit: l.Credit, Text: l.Text})
	}
}

// reversalDate is the reversing entry's date: the payload's, else the entry's.
func reversalDate(e *Entry, payload json.RawMessage) string {
	var p struct{ Date string }
	json.Unmarshal(payload, &p)
	if p.Date == "" {
		return e.Date
	}
	return p.Date
}

// reverse checks the reversing entry's date and names it.
func reverse(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
	e := record.(*Entry)
	if !open(c, reversalDate(e, payload)) {
		return invalid()
	}
	e.ReversedBy = e.ID + "-R"
	if _, taken := platform.Get[Entry](c, e.ReversedBy); taken {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return nil
}

// reversed posts the reversing entry in the decision that reversed e: the
// same lines, debits and credits swapped.
func reversed(c platform.Caller, r *pb.ChangeRecord, record any, _ time.Time) {
	e := record.(*Entry)
	lines := slices.Clone(e.Lines)
	for i := range lines {
		lines[i].Debit, lines[i].Credit = lines[i].Credit, lines[i].Debit
	}
	book(c, r, Entry{Record: platform.Record{ID: e.ReversedBy}, Journal: e.Journal, Date: reversalDate(e, r.GetSubmission().GetPayload()),
		Reference: "Reversal of " + e.Number, Lines: lines, State: "posted", Reverses: e.ID})
}

// Balance is one account's line of the trial balance, in minor units of the company's currency.
type Balance struct {
	Account string `json:"account"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Debit   int64  `json:"debit"`
	Credit  int64  `json:"credit"`
	Balance int64  `json:"balance"` // debit minus credit
}

// Read serves the trial balance: every account with postings, and their sums.
func (a *App) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name != TrialBalance || c.Role() == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	sums := map[string]*Balance{}
	for _, p := range platform.Records[Posting](c) {
		b := sums[string(p.Account)]
		if b == nil {
			account, _ := platform.Get[Account](c, string(p.Account))
			b = &Balance{Account: account.ID, Name: account.Name, Kind: account.Kind}
			sums[b.Account] = b
		}
		b.Debit, b.Credit = b.Debit+p.Debit.Amount, b.Credit+p.Credit.Amount
		b.Balance = b.Debit - b.Credit
	}
	out := []Balance{}
	for _, b := range sums {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(x, y Balance) int { return strings.Compare(x.Account, y.Account) })
	return out, nil
}

// Chart is a small chart of accounts to start from, coded like China's
// enterprise accounting standards; a controller creates what the company needs.
func Chart() []Account {
	a := func(code, name, kind string) Account {
		return Account{Record: platform.Record{ID: code}, Name: name, Kind: kind}
	}
	return []Account{
		a("1001", "Cash", "asset"), a("1002", "Bank", "asset"), a("1122", "Accounts receivable", "asset"),
		a("1403", "Raw materials", "asset"), a("1405", "Finished goods", "asset"), a("5001", "Work in progress", "asset"),
		a("2202", "Accounts payable", "liability"), a("2203", "Goods received not invoiced", "liability"),
		a("4001", "Share capital", "equity"), a("6001", "Sales", "income"),
		a("6401", "Cost of goods sold", "expense"), a("6602", "Administrative expenses", "expense"), a("6711", "Scrap", "expense"),
	}
}
