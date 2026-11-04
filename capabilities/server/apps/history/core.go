package history

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const (
	ID           = "core"
	Coded        = "core.coded"
	PersonType   = "core.person"
	PartnerType  = "core.partner"
	SiteType     = "core.site"
	LocationType = "core.location"
	MaterialType = "core.material"
	UoMType      = "core.uom"
	CurrencyType = "core.currency"
	AccountType  = "core.account"
	PeriodType   = "core.period"
	JournalType  = "core.journal"
)

type Person struct {
	platform.Record
	Element string                `json:"element,omitempty" ref:"enterprise.element" stereo:"ActualPerson" title:"Person in the model"`
	Name    string                `json:"name" field:"required,search" title:"Full name"`
	Code    string                `json:"code,omitempty" field:"search" title:"Personnel number" help:"Employee or badge number, unique when given"`
	Kind    string                `json:"kind" field:"required" choices:"employee,contractor,contact,other"`
	Email   string                `json:"email,omitempty" field:"search" personal:"contact"`
	Phone   string                `json:"phone,omitempty" personal:"contact"`
	Member  string                `json:"member,omitempty" title:"Signs in as" help:"The platform member this person is, when they have an account"`
	Partner platform.Ref[Partner] `json:"partner,omitempty" title:"Works for" help:"For a contact: the business partner they belong to"`
	Site    platform.Ref[Site]    `json:"site,omitempty" title:"Home site"`
	Active  bool                  `json:"active" title:"Active"`
	Note    string                `json:"note,omitempty" type:"longtext"`
}

type Partner struct {
	platform.Record
	Name     string                 `json:"name" field:"required,search"`
	Code     string                 `json:"code" field:"required,search" help:"Short unique code, e.g. ACME"`
	Roles    []string               `json:"roles" field:"required" title:"Roles" help:"What we do with them: customer, supplier, carrier, manufacturer; a partner may be several"`
	TaxID    string                 `json:"taxId,omitempty" title:"Tax ID"`
	Country  string                 `json:"country,omitempty" help:"ISO 3166-1 alpha-2, e.g. CN"`
	Address  string                 `json:"address,omitempty" type:"longtext"`
	Email    string                 `json:"email,omitempty"`
	Phone    string                 `json:"phone,omitempty"`
	Currency platform.Ref[Currency] `json:"currency,omitempty" title:"Trades in"`
	Active   bool                   `json:"active" title:"Active"`
	Note     string                 `json:"note,omitempty" type:"longtext"`
}

type Site struct {
	platform.Record
	Name     string `json:"name" field:"required,search"`
	Code     string `json:"code" field:"required,search" help:"Short unique code, e.g. SH01"`
	Kind     string `json:"kind" field:"required" choices:"plant,warehouse,office,store,yard,other"`
	Place    string `json:"place,omitempty" ref:"enterprise.element" stereos:"ActualOrganization,ActualLocation" title:"Place in the model" help:"The enterprise model's location for this place; the model is where its position in the enterprise lives" example:"loc-plant-1"`
	Unit     string `json:"unit,omitempty" ref:"enterprise.element" stereo:"ActualOrganization" title:"Owned by unit" help:"The organisation in the enterprise model responsible for it" example:"open-plant-1"`
	Address  string `json:"address,omitempty" type:"longtext"`
	Timezone string `json:"timezone,omitempty" help:"IANA name, e.g. Asia/Shanghai"`
	Active   bool   `json:"active" title:"Active"`
}

type Location struct {
	platform.Record
	Place  string                 `json:"place,omitempty" ref:"enterprise.element" stereos:"ActualOrganization,ActualLocation" title:"Place in the model"`
	Name   string                 `json:"name" field:"required,search"`
	Code   string                 `json:"code" field:"required,search" help:"Unique within the site, e.g. A-01-03"`
	Kind   string                 `json:"kind" field:"required" choices:"zone,aisle,rack,bin,dock,staging,line,workcenter,other"`
	Site   platform.Ref[Site]     `json:"site" field:"required" title:"Site"`
	Parent platform.Ref[Location] `json:"parent,omitempty" title:"Inside"`
	Active bool                   `json:"active" title:"Active"`
}

type Material struct {
	platform.Record
	Name        string            `json:"name" field:"required,search"`
	Code        string            `json:"code" field:"required,search" title:"Material number" help:"Unique, e.g. M-10021"`
	Kind        string            `json:"kind" field:"required" choices:"raw,component,semifinished,finished,consumable,packaging,service,other"`
	BaseUoM     platform.Ref[UoM] `json:"baseUom" field:"required" title:"Base unit"`
	Barcode     string            `json:"barcode,omitempty" field:"search" help:"EAN/UPC or internal barcode"`
	Group       string            `json:"group,omitempty" field:"search" title:"Material group"`
	Weight      float64           `json:"weight,omitempty" title:"Net weight, kg"`
	ShelfLife   int               `json:"shelfLifeDays,omitempty" title:"Shelf life, days"`
	Batches     bool              `json:"batches,omitempty" title:"Batch managed"`
	Serials     bool              `json:"serials,omitempty" title:"Serial numbered"`
	Description string            `json:"description,omitempty" type:"longtext"`
	Active      bool              `json:"active" title:"Active"`
}

type UoM struct {
	platform.Record
	Code      string `json:"code" field:"required,search" help:"UN/ECE code when one exists, e.g. PCE, KGM, MTR"`
	Name      string `json:"name" field:"required,search"`
	Dimension string `json:"dimension" field:"required" choices:"count,mass,length,area,volume,time,other"`
	Decimals  int    `json:"decimals,omitempty" help:"How many decimals a quantity in it has"`
}

type Currency struct {
	platform.Record
	Code     string `json:"code" field:"required,search" help:"ISO 4217, e.g. CNY"`
	Name     string `json:"name" field:"required,search"`
	Decimals int    `json:"decimals" help:"Minor unit digits, usually 2"`
	Symbol   string `json:"symbol,omitempty"`
}

type Account struct {
	platform.Record
	Code    string `json:"code" field:"required,search" help:"Unique, sorts the chart, e.g. 1403" source:"record"`
	Name    string `json:"name" field:"required,search" source:"record"`
	Kind    string `json:"kind" field:"required" choices:"asset,liability,equity,revenue,expense" source:"record"`
	Parent  string `json:"parent,omitempty" title:"Under" help:"The summary account's code"`
	Posting bool   `json:"posting" title:"Postable"`
	Active  bool   `json:"active" title:"Active"`
}

type Period struct {
	platform.Record
	Code  string `json:"code" field:"required,search" help:"e.g. 2026-10" source:"record"`
	Year  int    `json:"year" field:"required"`
	From  string `json:"from" field:"required" title:"First day" help:"YYYY-MM-DD"`
	To    string `json:"to" field:"required" title:"Last day" help:"YYYY-MM-DD, inclusive"`
	State string `json:"state" field:"readonly" source:"record"`
}

type Journal struct {
	platform.Record
	Number       string        `json:"number,omitempty" field:"readonly,search" title:"Document number"`
	Date         string        `json:"date" field:"required" title:"Posting date" help:"YYYY-MM-DD"`
	Period       string        `json:"period,omitempty" field:"readonly" title:"Period"`
	ClosedPeriod string        `json:"closedPeriod,omitempty" choices:"reject,next-open" title:"Closed period policy"`
	Shifted      bool          `json:"shifted,omitempty" field:"readonly" title:"Posted to a later period"`
	Text         string        `json:"text,omitempty" field:"search"`
	Currency     string        `json:"currency,omitempty" help:"ISO 4217; default the tenant's first currency"`
	Lines        []JournalLine `json:"lines" field:"required" type:"json" title:"Lines"`
	Total        float64       `json:"total,omitempty" field:"readonly" title:"Total debit"`
	Source       string        `json:"source,omitempty" field:"search" help:"The decision that posted it, set by the platform"`
	Reverses     string        `json:"reverses,omitempty" field:"readonly" title:"Reverses"`
	Reversed     string        `json:"reversed,omitempty" field:"readonly" title:"Reversed by"`
	State        string        `json:"state" field:"readonly"`
}

type JournalLine struct {
	Account string  `json:"account"`
	Debit   float64 `json:"debit,omitempty"`
	Credit  float64 `json:"credit,omitempty"`
	Text    string  `json:"text,omitempty"`
	Object  string  `json:"object,omitempty"`
	Partner string  `json:"partner,omitempty"`
}

func Interfaces() []platform.Interface {
	return []platform.Interface{{Name: Coded, Title: "Coded", Description: "Anything found by a short unique code and a name.",
		Fields: []platform.InterfaceField{{Name: "code", Type: "text", Title: "Code"}, {Name: "name", Type: "text", Title: "Name"}}}}
}

func standard() platform.Standard {
	return platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"steward"}}
}

func isValue(field, value string) platform.Expr {
	return platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", field), platform.ExprLiteralValue("text", value)}}
}

func inactive() platform.Expr {
	return platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("boolean", "active"), platform.ExprLiteralValue("boolean", false)}}
}

func Entities() []platform.Entity {
	active := platform.ExprReference("boolean", "active")
	out := []platform.Entity{
		{Type: PersonType, Title: "Person", Model: Person{}, Display: "name", Icon: "user-round", Color: "info", Synonyms: "employee,staff,contact,worker",
			Format: map[string][]platform.FormatRule{
				"kind":   {{When: isValue("kind", "employee"), Tone: "info"}, {When: isValue("kind", "contractor"), Tone: "warning"}, {When: isValue("kind", "contact"), Tone: "teal"}},
				"active": {{When: inactive(), Tone: "neutral"}},
			},
			Description: "A human the organisation works with: an employee, a contractor, a contact at a partner.", Standard: standard()},
		{Type: PartnerType, Title: "Business partner", Model: Partner{}, Display: "name", Icon: "handshake", Color: "teal", Synonyms: "customer,supplier,vendor,carrier",
			Format:      map[string][]platform.FormatRule{"active": {{When: inactive(), Tone: "neutral"}}},
			Description: "An organisation or person we trade with, in one or more roles: customer, supplier, carrier.", Standard: standard(), Implements: []string{Coded}},
		{Type: SiteType, Title: "Site", Model: Site{}, Display: "name", Icon: "building-2", Color: "orange", Synonyms: "plant,warehouse,factory,office",
			Format:      map[string][]platform.FormatRule{"active": {{When: inactive(), Tone: "neutral"}}},
			Description: "A physical place the organisation operates: a plant, a warehouse, an office.", Standard: standard(), Implements: []string{Coded}},
		{Type: LocationType, Title: "Location", Model: Location{}, Display: "code", Synonyms: "bin,storage bin,work centre,line,dock",
			Description: "A place inside a site: a zone, an aisle, a bin, a line, a work centre, a dock.", Standard: standard(), Implements: []string{Coded}},
		{Type: MaterialType, Title: "Material", Model: Material{}, Display: "name", Icon: "package", Color: "purple", Synonyms: "item,SKU,part,product,article",
			Format: map[string][]platform.FormatRule{
				"kind":   {{When: isValue("kind", "finished"), Tone: "success"}, {When: isValue("kind", "raw"), Tone: "info"}, {When: isValue("kind", "service"), Tone: "teal"}},
				"active": {{When: inactive(), Tone: "neutral"}},
			},
			Description: "Anything counted, stored, bought, made or sold.", Standard: standard(), Implements: []string{Coded}},
		{Type: UoMType, Title: "Unit of measure", Model: UoM{}, Display: "code", Synonyms: "unit,UoM",
			Description: "A unit quantities are measured in.", Standard: standard(), Implements: []string{Coded}},
		{Type: CurrencyType, Title: "Currency", Model: Currency{}, Display: "code",
			Description: "A currency amounts are kept in.", Standard: standard(), Implements: []string{Coded}},
		{Type: AccountType, Title: "Account", Model: Account{}, Display: "name", Synonyms: "GL account,ledger account,chart of accounts",
			Description: "One line of the chart of accounts; entries post to postable accounts, summary accounts total them.", Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"accountant"}}, Implements: []string{Coded}},
		{Type: PeriodType, Title: "Fiscal period", Model: Period{}, Display: "code", Synonyms: "accounting period,month,close",
			Description: "A fiscal period entries belong to; closing it sends late entries to the next open one.", Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"accountant"}},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "open", States: []platform.State{{Name: "open", Title: "Open", Tone: "success"}, {Name: "closed", Title: "Closed", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "close", Title: "Close", Description: "No more entries in it; later ones go to the next open period.", From: []string{"open"}, To: []string{"closed"}, Roles: []string{"accountant"}, Payload: []platform.Field{}},
					{Name: "reopen", Title: "Reopen", Description: "Accept entries again.", From: []string{"closed"}, To: []string{"open"}, Roles: []string{"accountant"}, Payload: []platform.Field{}}}}},
		{Type: JournalType, Title: "Journal entry", Model: Journal{}, Display: "number", Synonyms: "accounting document,voucher,posting",
			Description: "A balanced double-entry record of what happened in money; applications' actions post them, people reverse them.",
			Standard:    platform.Standard{Create: true, Roles: []string{"accountant"}},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "posted", States: []platform.State{{Name: "posted", Title: "Posted", Tone: "success"}, {Name: "reversed", Title: "Reversed", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "reverse", Title: "Reverse", Description: "Post the opposite entry today; nothing is deleted.", From: []string{"posted"}, To: []string{"reversed"}, Roles: []string{"accountant"},
						Payload: []platform.Field{{Name: "date", Type: "string", Description: "The reversal's posting date; default today"}}}}}},
	}
	for i := range out {
		if slices.Contains([]string{PersonType, SiteType, LocationType}, out[i].Type) {
			out[i].EnterpriseUse = &active
		}
	}
	return out
}

type Core struct{ ledger *platform.Ledger }

func New(tenant string) *Core {
	entities := Entities()
	types := make([]string, len(entities))
	for i, entity := range entities {
		types[i] = entity.Type
	}
	return &Core{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(), types...)}
}

func (c *Core) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: platform.BuiltinManifestTitle(ID), Version: "1", Actions: c.ledger.Catalog, Entities: Entities(), Interfaces: Interfaces()}
}

func (c *Core) AcceptedLedger() *platform.Ledger         { return c.ledger }
func (c *Core) Declarations() []*pb.AuthorityDeclaration { return c.ledger.Declarations() }
func (c *Core) SnapshotFormat() string                   { return "platform.ledger.v1" }
func (c *Core) Snapshot() (json.RawMessage, error)       { return c.ledger.Snapshot() }
func (c *Core) Restore(raw json.RawMessage) error        { return c.ledger.Restore(raw) }

func (c *Core) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
