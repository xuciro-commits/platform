package mes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
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
func (p *Plant) confirmIfFinished(who platformserver.Caller, order string, now time.Time) {
	o := p.orders[order]
	if o == nil || o.ERP != "" {
		return
	}
	done := 0
	for _, id := range o.SFCs {
		switch p.sfcs[id].State {
		case "done":
			done++
		case "scrapped":
		default:
			return
		}
	}
	yield := o.Quantity * done / len(o.SFCs)
	c := Confirmation{Order: o.ID, Planned: o.Planned, Product: o.Product, Quantity: o.Quantity, Yield: yield, Scrap: o.Quantity - yield, SFCs: o.SFCs}
	if n, _ := who.Emit(EffectConfirmation, o.ID, OrderType+"/"+o.ID, c, now); n > 0 {
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
func (p *Plant) Answer(c platformserver.Caller, e platformserver.Effect, o platformserver.Outcome, now time.Time) *kernel.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	order := p.orders[e.Key]
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
	order.ERP, order.Confirmation, order.ERPDetail = a.State, a.Confirmation, a.Detail
	if a.State != "confirmed" {
		c.Notify(platformserver.Notification{Title: fmt.Sprintf("ERP %s the confirmation of %s", a.State, order.ID), Body: a.Detail,
			Ref: OrderType + "/" + order.ID, Key: "erp:" + e.ID + ":" + strconv.Itoa(e.Attempts)}, now, p.supervisorsOfOrder(order))
	}
	return nil
}

// supervisorsOfOrder are the supervisors of the line where the order's routing starts.
func (p *Plant) supervisorsOfOrder(o *Order) platformserver.Recipient {
	r := platformserver.Recipient{Structure: SiteStructure, Role: string(Supervisor)}
	if prod := p.product(o.Product); prod != nil && len(prod.Operations) > 0 {
		r.Unit = p.workCenter(prod.Operations[0].WorkCenter).Line
	}
	return r
}
