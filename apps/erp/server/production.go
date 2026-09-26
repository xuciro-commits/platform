package erp

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"production"
)

// Production orders (ADR-0024 7c): a planner releases an order to make a
// finished product; the plant executes it and confirms what it made and lost
// through production.orders/1, which the ERP provides. Accepting a
// confirmation issues the components, receives the yield and writes off the
// scrap, all at standard cost, in one posted entry of the production journal:
// work in progress takes the components and gives the output, and what stays
// in it is a production variance.

const (
	ProductionType = "erp.production"

	Planner = "planner" // releases production orders; a plant confirms them as one

	ProductionOrders = "production-orders" // the protocol's read

	AccountWIP       = "5001"
	AccountScrap     = "6711"
	AccountVariance  = "6404"
	SchemaProduction = ProductionType + ".confirm"
)

// Component is what one unit of a finished product takes.
type Component struct {
	Product  platform.Ref[Product] `json:"product" field:"required"`
	Quantity float64               `json:"quantity" field:"required" help:"Per unit of the product"`
}

// Production is an order to make a finished product.
type Production struct {
	platform.Record
	Number    string                `json:"number,omitempty" field:"readonly,search" help:"Given when the order is released" example:"MO/2026/00001"`
	Product   platform.Ref[Product] `json:"product" field:"required"`
	Quantity  float64               `json:"quantity" field:"required"`
	Due       string                `json:"due,omitempty" type:"date"`
	ShopOrder string                `json:"shopOrder,omitempty" field:"readonly,search" title:"Shop order" help:"The plant's order that executed it"`
	Yield     float64               `json:"yield,omitempty" field:"readonly" help:"Good units the plant made"`
	Scrap     float64               `json:"scrap,omitempty" field:"readonly" help:"Units the plant scrapped"`
	State     string                `json:"state" field:"readonly" choices:"draft,released,confirmed,canceled"`
}

func producing() []platform.Entity {
	planning := []string{Planner, Controller}
	return []platform.Entity{{Type: ProductionType, Title: "Production order", Model: Production{}, Synonyms: "manufacturing order, MO, planned order",
		Description: "An order to make a finished product: released to the plant, then confirmed with what it made and scrapped.",
		Standard:    platform.Standard{Create: true, Edit: true, Roles: planning},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{
				{Name: "draft", Title: "Draft", Tone: "info", Description: "Being planned; the plant does not see it"},
				{Name: "released", Title: "Released", Tone: "warning", Description: "The plant may make it"},
				{Name: "confirmed", Title: "Confirmed", Tone: "success", Description: "Made: its components, output and scrap are posted"},
				{Name: "canceled", Title: "Canceled", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "release", Title: "Release", From: []string{"draft"}, To: []string{"released"}, Roles: planning,
					Description: "Release the order to the plant; the product must be a finished good with its components.",
					Do:          releasable, After: release},
				{Name: "confirm", Title: "Confirm", From: []string{"released"}, To: []string{"confirmed"}, Roles: planning,
					Description: "Confirm what the plant made and scrapped: its components are issued and its output received at standard cost.",
					Payload:     production.Protocol().Actions[0].Payload, Do: confirmable, After: confirmed},
				{Name: "cancel", Title: "Cancel", From: []string{"draft", "released"}, To: []string{"canceled"}, Roles: planning,
					Description: "Cancel an order the plant has not confirmed."}}}}}
}

// Provision is how the ERP provides production.orders/1 (ADR-0011).
func Provision() platform.Provision {
	return platform.Provision{Protocol: production.Protocol(),
		Actions: map[string]string{"confirm": SchemaProduction},
		Reads:   map[string]string{"orders": ProductionOrders},
		Events:  map[string]string{"released": ProductionType + ".release"}}
}

func releasable(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	o := record.(*Production)
	p, known := platform.Get[Product](c, string(o.Product))
	if !known || p.Archived || p.Kind != "finished" || len(p.Components) == 0 || o.Quantity <= 0 {
		return invalid()
	}
	return nil
}

func release(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	o := *record.(*Production)
	o.Number, _ = c.Next(r, "production", now)
	c.Put(r, o)
}

// confirmable checks a confirmation: a shop order, no more than the order's
// quantity made and scrapped, today's period open, the accounts there.
func confirmable(c platform.Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error {
	o := record.(*Production)
	var in production.Confirmation
	if json.Unmarshal(payload, &in) != nil || in.ShopOrder == "" || in.Yield < 0 || in.Scrap < 0 || in.Yield+in.Scrap <= 0 || in.Yield+in.Scrap > o.Quantity {
		return invalid()
	}
	if !open(c, now.Format(time.DateOnly)) {
		return invalid()
	}
	for _, code := range []string{AccountMaterials, AccountFinished, AccountWIP, AccountScrap, AccountVariance} {
		if a, known := platform.Get[Account](c, code); !known || a.Archived {
			return invalid()
		}
	}
	o.ShopOrder, o.Yield, o.Scrap = in.ShopOrder, in.Yield, in.Scrap
	return nil
}

// confirmed moves the components out and the yield in, and posts them.
func confirmed(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	o := record.(*Production)
	date, currency := now.Format(time.DateOnly), c.Setting("platform/currency")
	product, _ := platform.Get[Product](c, string(o.Product))
	money := func(v int64) platform.Money { return platform.Money{Amount: v, Currency: currency} }
	n := 0
	move := func(p platform.Ref[Product], qty float64, value int64, reason string) {
		n++
		c.Put(r, Move{Record: platform.Record{ID: fmt.Sprintf("%s.%d", o.ID, n)}, Product: p, Quantity: qty, Value: money(value),
			Reason: reason, Document: o.ID, Date: date})
	}
	var lines []Line
	var wip int64
	made := o.Yield + o.Scrap
	for _, part := range product.Components {
		component, _ := platform.Get[Product](c, string(part.Product))
		qty := part.Quantity * made
		value := round(float64(component.Cost.Amount) * qty)
		move(part.Product, -qty, -value, "issue")
		lines = append(lines, Line{Account: AccountWIP, Debit: money(value), Text: string(part.Product)},
			Line{Account: stockAccount(component), Credit: money(value), Text: string(part.Product)})
		wip += value
	}
	output := round(float64(product.Cost.Amount) * o.Yield)
	if o.Yield > 0 {
		move(o.Product, o.Yield, output, "output")
		lines = append(lines, Line{Account: AccountFinished, Debit: money(output), Text: string(o.Product)},
			Line{Account: AccountWIP, Credit: money(output), Text: string(o.Product)})
	}
	scrap := round(float64(product.Cost.Amount) * o.Scrap)
	if scrap > 0 {
		lines = append(lines, Line{Account: AccountScrap, Debit: money(scrap), Text: string(o.Product)},
			Line{Account: AccountWIP, Credit: money(scrap), Text: string(o.Product)})
	}
	if v := wip - output - scrap; v != 0 { // what the standard of the product does not account for
		variance := Line{Account: AccountVariance, Text: o.Number}
		back := Line{Account: AccountWIP, Text: o.Number}
		if v > 0 {
			variance.Debit, back.Credit = money(v), money(v)
		} else {
			variance.Credit, back.Debit = money(-v), money(-v)
		}
		lines = append(lines, variance, back)
	}
	book(c, r, Entry{Record: platform.Record{ID: o.ID + "-C"}, Journal: "production", Date: date,
		Reference: "Confirmation of " + o.Number + " by " + o.ShopOrder, Lines: lines, State: "posted"})
}

// productionOrders is the protocol's read: every order the plant may see.
func productionOrders(c platform.Caller) []production.Order {
	out := []production.Order{}
	for _, o := range platform.Records[Production](c) {
		if o.State == "released" || o.State == "confirmed" {
			out = append(out, production.Order{ID: o.ID, Number: o.Number, Product: string(o.Product), Quantity: o.Quantity, Due: o.Due, State: o.State})
		}
	}
	return out
}
