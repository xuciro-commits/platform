package mes

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"production"
)

// The plant's one path to an ERP is production.orders/1 (ADR-0024 7c, 7d):
// the provider's orders are the planned orders supervisors release shop orders
// against, and a completed shop order is confirmed through the protocol in the
// rules of the plant's own decision (K4 C10). The ERP app decides at once; the
// adapter to an ERP outside takes the confirmation as sent, and the plant
// records the ERP's answer when the provider shows it.

// PlannedOrder is a provider's order as the plant lists it.
type PlannedOrder struct {
	ERPID    string `json:"erpId"`
	Number   string `json:"number"`
	Product  string `json:"product"`
	Quantity int    `json:"quantity"`
	Due      string `json:"due,omitempty"`
	State    string `json:"state"`
}

// erpOrders are the orders of the protocol's providers; ok is false when the
// tenant has none.
func erpOrders(c platform.Caller) (orders []production.Order, ok bool) {
	results, err := c.Query(production.ID, "orders")
	for _, r := range results {
		o, _ := r.Result.([]production.Order)
		orders = append(orders, o...)
	}
	return orders, err == nil && len(results) > 0
}

// erpOrder is the provider's order id, when there is one.
func erpOrder(c platform.Caller, id string) (production.Order, bool) {
	orders, _ := erpOrders(c)
	for _, o := range orders {
		if o.ID == id {
			return o, true
		}
	}
	return production.Order{}, false
}

// planned lists the providers' orders.
func planned(c platform.Caller) []PlannedOrder {
	orders, _ := erpOrders(c)
	out := []PlannedOrder{}
	for _, o := range orders {
		out = append(out, PlannedOrder{ERPID: o.ID, Number: o.Number, Product: o.Product, Quantity: int(o.Quantity), Due: o.Due, State: o.State})
	}
	return out
}

// fits says why an order cannot fulfil planned order id, or "" when it can: a
// provider has it open, for the same product and at least the quantity, and
// no other order fulfils it.
func (p *Plant) fits(c platform.Caller, o Order, id string) string {
	planned, known := erpOrder(c, id)
	switch {
	case !known:
		return "the ERP has no planned order " + id
	case planned.State == "sent" || planned.State == "confirmed":
		return id + " is " + planned.State
	case planned.Product != o.Product:
		return fmt.Sprintf("%s is for %s, not %s", id, planned.Product, o.Product)
	case int(planned.Quantity) < o.Quantity:
		return fmt.Sprintf("%s is for %g, fewer than %d", id, planned.Quantity, o.Quantity)
	}
	others, _, _ := platform.Find[Order](c, platform.Query{Domain: json.RawMessage(`[["planned","=",` + fmt.Sprintf("%q", id) + `]]`)})
	for _, other := range others {
		if other.ID != o.ID {
			return other.ID + " already fulfils " + id
		}
	}
	return ""
}

// made is what the order made and lost, by its SFCs.
func (p *Plant) made(who platform.Caller, o Order) (yield, scrap int) {
	done := 0
	for _, id := range o.SFCs {
		if s, _ := platform.Get[SFC](who, string(id)); s.State == "done" {
			done++
		}
	}
	yield = o.Quantity * done / len(o.SFCs)
	return yield, o.Quantity - yield
}

// confirm confirms o to the provider of its planned order in the rules of the
// plant's decision s, and returns how to record the outcome: refused at once,
// answered at once, or sent.
func (p *Plant) confirm(who platform.Caller, s *pb.Submission, o Order, now time.Time) func(*pb.ChangeRecord) {
	key := s.GetIdempotencyKey()
	if o.Planned == "" {
		return func(r *pb.ChangeRecord) {
			p.answered(who, r, o, production.Order{State: "refused", Detail: "no planned order to confirm against"}, key, now)
		}
	}
	yield, scrap := p.made(who, o)
	raw, _ := json.Marshal(production.Confirmation{ShopOrder: o.ID, Yield: float64(yield), Scrap: float64(scrap)})
	_, _, err := who.Invoke(production.ID, "confirm", o.Planned, raw, "mes:"+key, key, now)
	planned, _ := erpOrder(who, o.Planned)
	return func(r *pb.ChangeRecord) {
		switch {
		case err != nil:
			p.answered(who, r, o, production.Order{State: "refused", Detail: err.Error()}, key, now)
		case planned.Answered(o.ID):
			p.answered(who, r, o, planned, key, now)
		default:
			o.ERP = "sent"
			who.Put(r, o)
		}
	}
}

// answered records the ERP's answer on the order and tells the line's
// supervisors when it was refused or never arrived.
func (p *Plant) answered(who platform.Caller, r *pb.ChangeRecord, o Order, answer production.Order, key string, now time.Time) {
	o.ERP, o.Confirmation, o.ERPDetail = answer.State, answer.Confirmation, answer.Detail
	who.Put(r, o)
	if o.ERP != "confirmed" {
		who.Notify(platform.Notification{Title: fmt.Sprintf("ERP %s the confirmation of %s", o.ERP, o.ID), Body: o.ERPDetail,
			Ref: OrderType + "/" + o.ID, Key: "erp:" + key}, now, p.supervisorsOfOrder(o))
	}
}

// awaited is the provider's answer to a sent order's confirmation, once there is one.
func awaited(c platform.Caller, o Order) (production.Order, bool) {
	planned, _ := erpOrder(c, o.Planned)
	return planned, o.ERP == "sent" && planned.Answered(o.ID)
}

// fulfilled are the planned orders shop orders fulfil.
func (p *Plant) fulfilled(c platform.Caller) []string {
	orders, _, _ := platform.Find[Order](c, platform.Query{Sort: []string{"id"}})
	var out []string
	for _, o := range orders {
		if o.Planned != "" {
			out = append(out, o.Planned)
		}
	}
	return out
}
