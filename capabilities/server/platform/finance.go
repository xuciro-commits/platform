package platform

const (
	BooksApp    = "core"
	JournalType = "core.journal"
)

// Journal is one balanced entry: lines whose debits equal their credits.
type Journal struct {
	Record
	Number       string `json:"number,omitempty" field:"readonly,search" title:"Document number"`
	Date         string `json:"date" field:"required" title:"Posting date" help:"YYYY-MM-DD"`
	Period       string `json:"period,omitempty" field:"readonly" title:"Period"`
	ClosedPeriod string `json:"closedPeriod,omitempty" choices:"reject,next-open" title:"Closed period policy"`
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
