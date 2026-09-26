package mes

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"production"
)

// The ERP app through production.orders/1 (ADR-0024 7c): in a tenant where an
// app provides the protocol, its released orders are planned orders the plant
// may release against, and a completed order that fulfils one is confirmed
// through the protocol: the provider accepts or refuses it at once, as the
// plant's own decision's rule (K4 C10). Planned orders from an external ERP
// keep the connector and the confirmation effect until the adapter replaces
// them (7d).

// erpOrders are the released and confirmed orders of the protocol's providers.
func erpOrders(c platform.Caller) []production.Order {
	results, err := c.Query(production.ID, "orders")
	if err != nil {
		return nil
	}
	var out []production.Order
	for _, r := range results {
		orders, _ := r.Result.([]production.Order)
		out = append(out, orders...)
	}
	return out
}

// erpOrder is the provider's order id, when there is one.
func erpOrder(c platform.Caller, id string) (production.Order, bool) {
	for _, o := range erpOrders(c) {
		if o.ID == id {
			return o, true
		}
	}
	return production.Order{}, false
}

// confirmThrough confirms o to the provider of its planned order, in the rules
// of the plant's decision s, and returns how to record the answer; ok is false
// when o's planned order is not one of the protocol's.
func (p *Plant) confirmThrough(who platform.Caller, s *pb.Submission, o Order, now time.Time) (func(*pb.ChangeRecord), bool) {
	planned, ok := erpOrder(who, o.Planned)
	if !ok {
		return nil, false
	}
	c := p.confirmed(who, o)
	raw, _ := json.Marshal(production.Confirmation{ShopOrder: o.ID, Yield: float64(c.Yield), Scrap: float64(c.Scrap)})
	_, _, err := who.Invoke(production.ID, "confirm", planned.ID, raw, "mes:"+s.GetIdempotencyKey(), s.GetIdempotencyKey(), now)
	return func(r *pb.ChangeRecord) {
		o.ERP, o.Confirmation, o.ERPDetail = "confirmed", planned.Number, ""
		if err != nil {
			o.ERP, o.Confirmation, o.ERPDetail = "refused", "", err.Error()
			who.Notify(platform.Notification{Title: fmt.Sprintf("ERP %s the confirmation of %s", o.ERP, o.ID), Body: o.ERPDetail,
				Ref: OrderType + "/" + o.ID, Key: "erp:" + s.GetIdempotencyKey()}, now, p.supervisorsOfOrder(o))
		}
		who.Put(r, o)
	}, true
}
