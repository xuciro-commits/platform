package erp

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Purchasing and inventory (ADR-0024 7b): a buyer orders from a supplier; an
// order above the approval limit waits for a controller; receiving it moves the
// goods in and posts them at standard cost against goods received not invoiced
// (the difference to the order's price is a price variance); the supplier's bill
// clears goods received not invoiced against payables. On hand is the sum of moves.

const (
	PartnerType  = "erp.partner"
	ProductType  = "erp.product"
	PurchaseType = "erp.purchase"
	MoveType     = "erp.move"

	Buyer = "buyer" // orders from suppliers and receives what arrives

	OnHand = "on-hand" // the read

	// The accounts purchasing posts to, by code in the chart (Chart).
	AccountMaterials     = "1403"
	AccountFinished      = "1405"
	AccountPayable       = "2202"
	AccountReceivedNotIn = "2203"
	AccountPriceVariance = "6403"
)

// Partner is a supplier or a customer of the company (D10: the ERP's own).
type Partner struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Role  string `json:"role" field:"required" choices:"supplier,customer,both"`
	Email string `json:"email,omitempty" field:"search"`
}

// Product is what the company buys, makes or sells, valued at a standard cost (D9); its ID is its code ("P-100").
type Product struct {
	platform.Record
	Name string         `json:"name" field:"required,search"`
	Kind string         `json:"kind" field:"required" choices:"material,finished" help:"Material is bought and used; finished goods are made and sold"`
	Unit string         `json:"unit" field:"required" example:"pcs"`
	Cost platform.Money `json:"cost" title:"Standard cost" help:"What one unit is worth in the books; receipts and production are valued at it"`
}

// PurchaseLine is a product ordered, its quantity and its price per unit.
type PurchaseLine struct {
	Product  platform.Ref[Product] `json:"product" field:"required"`
	Quantity float64               `json:"quantity" field:"required"`
	Price    platform.Money        `json:"price" title:"Unit price"`
}

// Purchase is an order to a supplier.
type Purchase struct {
	platform.Record
	Number   string                `json:"number,omitempty" field:"readonly,search" help:"Given when the order is placed" example:"PO/2026/00001"`
	Supplier platform.Ref[Partner] `json:"supplier" field:"required"`
	Date     string                `json:"date" field:"required" type:"date"`
	Lines    []PurchaseLine        `json:"lines"`
	Total    platform.Money        `json:"total" field:"readonly"`
	Invoice  string                `json:"invoice,omitempty" field:"readonly,search" title:"Supplier invoice"`
	State    string                `json:"state" field:"readonly" choices:"draft,ordered,received,billed,canceled"`
}

// Move is goods in (a positive quantity) or out of stock, and what they are worth.
type Move struct {
	platform.Record
	Product  platform.Ref[Product] `json:"product" field:"readonly"`
	Quantity float64               `json:"quantity" field:"readonly" help:"In when positive, out when negative"`
	Value    platform.Money        `json:"value" field:"readonly" help:"The quantity at the product's standard cost"`
	Reason   string                `json:"reason" field:"readonly" choices:"receipt,issue,output,scrap"`
	Document string                `json:"document" field:"readonly,search" help:"The order the move belongs to"`
	Date     string                `json:"date" field:"readonly" type:"date"`
}

func purchasing() []platform.Entity {
	buying := []string{Buyer, Controller}
	return []platform.Entity{
		{Type: PartnerType, Title: "Partner", Model: Partner{}, Synonyms: "supplier, vendor, customer",
			Description: "A company the ERP buys from or sells to.",
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: buying}},
		{Type: ProductType, Title: "Product", Model: Product{}, Synonyms: "material, item, article",
			Description: "Something the company buys, makes or sells; its ID is its code, and it is valued at its standard cost.",
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Controller}}},
		{Type: PurchaseType, Title: "Purchase order", Model: Purchase{}, Synonyms: "PO, order to a supplier",
			Description: "An order to a supplier: placed, then received into stock, then billed by the supplier.",
			Standard:    platform.Standard{Create: true, Edit: true, Roles: buying},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
				States: []platform.State{
					{Name: "draft", Title: "Draft", Tone: "info", Description: "Being prepared; nothing is ordered yet"},
					{Name: "ordered", Title: "Ordered", Tone: "warning", Description: "Placed with the supplier; the goods have not arrived"},
					{Name: "received", Title: "Received", Tone: "warning", Description: "The goods are in stock; the supplier's bill has not come"},
					{Name: "billed", Title: "Billed", Tone: "success", Description: "Received and billed: what is owed is in payables"},
					{Name: "canceled", Title: "Canceled", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "order", Title: "Place order", From: []string{"draft"}, To: []string{"ordered"}, Roles: buying,
						Description: "Place the order with the supplier; from the approval limit on, a controller approves it first.",
						Approval: &platform.Approval{Levels: []platform.ApprovalLevel{
							{Title: "Controller", AppRole: Controller, When: overLimit, Due: 48 * time.Hour}}},
						Do: order, After: ordered},
					{Name: "receive", Title: "Receive", From: []string{"ordered"}, To: []string{"received"}, Roles: buying,
						Description: "The goods arrived: move them into stock and post them at standard cost against goods received not invoiced.",
						Do:          receivable, After: received},
					{Name: "bill", Title: "Book bill", From: []string{"received"}, To: []string{"billed"}, Roles: []string{Accountant, Controller},
						Description: "Book the supplier's bill for the order: goods received not invoiced against payables.",
						Payload:     []platform.Field{{Name: "invoice", Type: "string", Description: "The supplier's invoice number"}},
						Do:          bill, After: billed},
					{Name: "cancel", Title: "Cancel", From: []string{"draft", "ordered"}, To: []string{"canceled"}, Roles: buying,
						Description: "Cancel an order that has not arrived."}}}},
		{Type: MoveType, Title: "Stock move", Model: Move{}, Synonyms: "goods movement, inventory transaction",
			Description: "Goods into or out of stock; on hand is the sum of a product's moves."},
	}
}

// totalOf is the order's total, when its lines are all in one currency.
func totalOf(p Purchase) (platform.Money, bool) {
	var total platform.Money
	for _, l := range p.Lines {
		if total.Currency != "" && l.Price.Currency != total.Currency {
			return total, false
		}
		total.Currency = l.Price.Currency
		total.Amount += round(float64(l.Price.Amount) * l.Quantity)
	}
	return total, true
}

func round(v float64) int64 { return int64(math.Round(v)) }

// overLimit: the order's total reaches the approval limit (in whole units of the currency).
func overLimit(c platform.Caller, s *pb.Submission) bool {
	p, _ := platform.Get[Purchase](c, s.GetTarget().GetId())
	total, _ := totalOf(p)
	limit, _ := strconv.ParseInt(c.Setting("approval-limit"), 10, 64)
	return total.Amount >= limit*100
}

// order checks an order before it is placed: a supplier, lines of known
// products with positive quantities, prices in the tenant's currency.
func order(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	p := record.(*Purchase)
	supplier, known := platform.Get[Partner](c, string(p.Supplier))
	if !known || supplier.Archived || supplier.Role == "customer" || len(p.Lines) == 0 {
		return invalid()
	}
	currency := c.Setting("platform/currency")
	for i := range p.Lines {
		l := &p.Lines[i]
		if product, known := platform.Get[Product](c, string(l.Product)); !known || product.Archived || l.Quantity <= 0 || l.Price.Amount < 0 {
			return invalid()
		}
		if l.Price.Currency == "" {
			l.Price.Currency = currency
		}
		if l.Price.Currency != currency {
			return invalid()
		}
	}
	p.Total, _ = totalOf(*p)
	return nil
}

func ordered(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	p := *record.(*Purchase)
	date, _ := time.Parse(time.DateOnly, p.Date)
	p.Number, _ = c.Next(r, "purchase", date)
	c.Put(r, p)
}

// receivable: the receipt is posted today, so today's period must be open, and
// the accounts it posts to must exist.
func receivable(c platform.Caller, _ any, _ json.RawMessage, now time.Time) *kernel.Error {
	if !open(c, now.Format(time.DateOnly)) {
		return invalid()
	}
	for _, code := range []string{AccountMaterials, AccountFinished, AccountReceivedNotIn, AccountPriceVariance} {
		if a, known := platform.Get[Account](c, code); !known || a.Archived {
			return invalid()
		}
	}
	return nil
}

// received moves the goods in and posts the receipt: stock at standard cost,
// goods received not invoiced at the order's price, the difference a price variance.
func received(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	p := record.(*Purchase)
	date := now.Format(time.DateOnly)
	var lines []Line
	var standard int64
	for i, l := range p.Lines {
		product, _ := platform.Get[Product](c, string(l.Product))
		value := platform.Money{Amount: round(float64(product.Cost.Amount) * l.Quantity), Currency: p.Total.Currency}
		c.Put(r, Move{Record: platform.Record{ID: fmt.Sprintf("%s.%d", p.ID, i+1)}, Product: l.Product, Quantity: l.Quantity, Value: value,
			Reason: "receipt", Document: p.ID, Date: date})
		lines = append(lines, Line{Account: stockAccount(product), Debit: value, Text: string(l.Product)})
		standard += value.Amount
	}
	lines = append(lines, Line{Account: AccountReceivedNotIn, Credit: p.Total, Text: p.Number})
	if v := p.Total.Amount - standard; v != 0 {
		variance := Line{Account: AccountPriceVariance, Text: p.Number}
		if v > 0 {
			variance.Debit = platform.Money{Amount: v, Currency: p.Total.Currency}
		} else {
			variance.Credit = platform.Money{Amount: -v, Currency: p.Total.Currency}
		}
		lines = append(lines, variance)
	}
	book(c, r, Entry{Record: platform.Record{ID: p.ID + "-GR"}, Journal: "purchases", Date: date, Reference: "Receipt of " + p.Number,
		Lines: lines, State: "posted"})
}

func stockAccount(p Product) platform.Ref[Account] {
	if p.Kind == "finished" {
		return AccountFinished
	}
	return AccountMaterials
}

func bill(c platform.Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error {
	var in struct{ Invoice string }
	json.Unmarshal(payload, &in)
	record.(*Purchase).Invoice = strings.TrimSpace(in.Invoice)
	if !open(c, now.Format(time.DateOnly)) {
		return invalid()
	}
	if a, known := platform.Get[Account](c, AccountPayable); !known || a.Archived {
		return invalid()
	}
	return nil
}

// billed posts the supplier's bill: goods received not invoiced against payables.
func billed(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	p := record.(*Purchase)
	book(c, r, Entry{Record: platform.Record{ID: p.ID + "-IV"}, Journal: "purchases", Date: now.Format(time.DateOnly),
		Reference: strings.TrimSpace("Bill " + p.Invoice + " for " + p.Number), State: "posted",
		Lines: []Line{{Account: AccountReceivedNotIn, Debit: p.Total, Text: p.Number}, {Account: AccountPayable, Credit: p.Total, Text: p.Invoice}}})
}

// Stock is one product's line of the on-hand read.
type Stock struct {
	Product  string  `json:"product"`
	Name     string  `json:"name"`
	Unit     string  `json:"unit"`
	Quantity float64 `json:"quantity"`
	Value    int64   `json:"value"` // minor units of the tenant's currency
}

// onHand sums every product's moves.
func onHand(c platform.Caller) []Stock {
	sums := map[string]*Stock{}
	for _, m := range platform.Records[Move](c) {
		s := sums[string(m.Product)]
		if s == nil {
			product, _ := platform.Get[Product](c, string(m.Product))
			s = &Stock{Product: product.ID, Name: product.Name, Unit: product.Unit}
			sums[s.Product] = s
		}
		s.Quantity += m.Quantity
		s.Value += m.Value.Amount
	}
	out := []Stock{}
	for _, s := range sums {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(x, y Stock) int { return strings.Compare(x.Product, y.Product) })
	return out
}
