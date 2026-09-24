package mes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Writing back to the ERP (#101, ADR-0014): when the last SFC of an order is
// done or scrapped, the plant confirms the order to the ERP, as SAP's production
// order confirmation does. The plant never calls the ERP itself: it emits an
// effect the host sends to the endpoint the tenant bound to it, at least once
// with the order as key. The ERP's answer comes back as an observation on the
// order, which the plant then acts on (D4).

const EffectConfirmation = "erp-confirmation"

// Confirmation is what the ERP receives.
type Confirmation struct {
	Order    string   `json:"order"`
	Planned  string   `json:"planned,omitempty"` // the ERP's planned order it fulfils
	Product  string   `json:"product"`
	Quantity int      `json:"quantity"`
	Yield    int      `json:"yield"`
	Scrap    int      `json:"scrap"`
	SFCs     []string `json:"sfcs"`
}

// confirmIfFinished emits the order's confirmation once all its SFCs have ended.
func (p *Plant) confirmIfFinished(who platform.Caller, order string, now time.Time) {
	o := p.orders[order]
	if o == nil || o.ERP != "" {
		return
	}
	for _, id := range o.SFCs {
		if s := p.sfcs[id].State; s != "done" && s != "scrapped" {
			return
		}
	}
	p.confirm(who, o, now)
}

// key names the order's current confirmation: the order, then "<order>#<n>"
// for the n-th corrected one, so the ERP receives a correction as a new message.
func (o *Order) key() string {
	if o.Resent == 0 {
		return o.ID
	}
	return fmt.Sprintf("%s#%d", o.ID, o.Resent+1)
}

// confirm emits the order's confirmation (#101) with its current key.
func (p *Plant) confirm(who platform.Caller, o *Order, now time.Time) {
	done := 0
	for _, id := range o.SFCs {
		if p.sfcs[id].State == "done" {
			done++
		}
	}
	yield := o.Quantity * done / len(o.SFCs)
	c := Confirmation{Order: o.ID, Planned: o.Planned, Product: o.Product, Quantity: o.Quantity, Yield: yield, Scrap: o.Quantity - yield, SFCs: o.SFCs}
	if n, _ := who.Emit(EffectConfirmation, o.key(), OrderType+"/"+o.ID, c, now); n > 0 {
		o.ERP = "sent"
	}
}

type answer struct {
	State        string `json:"state"` // confirmed, refused, failed
	Confirmation string `json:"confirmation,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

// Answer records how the ERP answered as an observation on the order, with the
// endpoint as provenance, and tells the line's supervisors when it was refused.
// The answer to a confirmation since corrected is recorded, and changes nothing.
func (p *Plant) Answer(c platform.Caller, e platform.Effect, o platform.Outcome, now time.Time) *kernel.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	id, _, _ := strings.Cut(e.Key, "#")
	order := p.orders[id]
	if order == nil {
		return notFound
	}
	a := answer{State: map[string]string{"delivered": "confirmed", "rejected": "refused"}[e.State], Detail: o.Detail}
	if a.State == "" {
		a.State = "failed"
	}
	var body struct {
		Confirmation string `json:"confirmation"`
		Error        string `json:"error"`
	}
	json.Unmarshal(o.Answer, &body)
	a.Confirmation = body.Confirmation
	if body.Error != "" {
		a.Detail = body.Error
	}
	raw, _ := json.Marshal(a)
	if _, err := p.facts.Record(&pb.Fact{TenantId: p.tenant, Kind: pb.FactKind_FACT_KIND_OBSERVATION,
		Subject: &pb.EntityRef{Type: OrderType, Id: order.ID}, Attribute: "erp-confirmation",
		Schema: &pb.SchemaRef{Name: schemaAnswer, Version: 1}, IdempotencyKey: e.ID + ":" + strconv.Itoa(e.Attempts), Payload: raw,
		Provenance: &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: e.Endpoint}, SourceTime: timestamppb.New(now)}}, now); err != nil {
		return err
	}
	if e.Key != order.key() {
		return nil
	}
	order.ERP, order.Confirmation, order.ERPDetail = a.State, a.Confirmation, a.Detail
	if a.State != "confirmed" {
		c.Notify(platform.Notification{Title: fmt.Sprintf("ERP %s the confirmation of %s", a.State, order.ID), Body: a.Detail,
			Ref: OrderType + "/" + order.ID, Key: "erp:" + e.ID + ":" + strconv.Itoa(e.Attempts)}, now, p.supervisorsOfOrder(order))
	}
	return nil
}

// supervisorsOfOrder are the supervisors of the order's line.
func (p *Plant) supervisorsOfOrder(o *Order) platform.Recipient {
	return platform.Recipient{Structure: SiteStructure, Role: string(Supervisor), Unit: p.orderLine(o)}
}

// orderLine is the line where the order's routing starts.
func (p *Plant) orderLine(o *Order) string {
	if prod := p.product(o.Product); prod != nil && len(prod.Operations) > 0 {
		return p.workCenter(prod.Operations[0].WorkCenter).Line
	}
	return ""
}

// claimed reports whether the ERP sent planned order id (its claim is on record).
func (p *Plant) claimed(id string) bool {
	for _, r := range p.facts.Records(p.tenant) {
		if f := r.GetFact(); f.GetSchema().GetName() == schemaPlanned && f.GetSubject().GetId() == id {
			return true
		}
	}
	return false
}
