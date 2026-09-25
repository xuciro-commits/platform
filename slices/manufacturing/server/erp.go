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

// finishOrder completes an order once all its SFCs have ended, in the decision
// r that ended the last one; the confirmation flow then confirms it to the ERP.
func (p *Plant) finishOrder(c platform.Caller, r *pb.ChangeRecord, order string, now time.Time) {
	o, known := platform.Get[Order](c, order)
	if !known || o.Status == "completed" {
		return
	}
	for _, id := range o.SFCs {
		if s, _ := platform.Get[SFC](c, string(id)); s.State != "done" && s.State != "scrapped" {
			return
		}
	}
	o.Status = "completed"
	c.Put(r, o)
}

// confirmation is the plant's flow from a completed order to the ERP's
// acceptance (ADR-0020): confirm it, wait for the ERP's answer; when the ERP
// refuses, ask the line's supervisors to correct and resend it and wait again;
// when the ERP is silent for an hour, tell them.
func (p *Plant) confirmation() platform.Flow {
	order := func(c platform.Caller, r *platform.Run) Order { o, _ := platform.Get[Order](c, r.Key); return o }
	supervisors := func(c platform.Caller, r *platform.Run) []platform.Recipient {
		return []platform.Recipient{p.supervisorsOfOrder(order(c, r))}
	}
	id := func(_ platform.Caller, r *platform.Run) string { return r.Key }
	return platform.Flow{Name: "erp-confirmation", Title: "Confirm to the ERP", Version: 1, Owners: []string{string(Supervisor)},
		Start: platform.Start{On: []string{SchemaComplete, SchemaSign}, Begin: func(c platform.Caller, e platform.Event) (string, any, bool) {
			sfc, _ := platform.Get[SFC](c, e.Record.GetSubmission().GetTarget().GetId())
			o, known := platform.Get[Order](c, string(sfc.Order))
			return o.ID, nil, known && o.Status == "completed" && o.ERP == ""
		}},
		Steps: []platform.Step{
			{Name: "confirm", Title: "Confirm the order", Act: &platform.Act{Action: SchemaConfirm, Target: id}, Next: "answer"},
			{Name: "answer", Title: "Wait for the ERP's answer", Timeout: time.Hour, OnTimeout: "silent",
				Wait: &platform.Wait{Until: func(c platform.Caller, r *platform.Run) bool { e := order(c, r).ERP; return e != "" && e != "sent" }},
				Choose: func(c platform.Caller, r *platform.Run) (string, string) {
					o := order(c, r)
					if o.ERP == "confirmed" {
						return "", "the ERP confirmed it as " + o.Confirmation
					}
					return "propose", "the ERP " + o.ERP + " it: " + o.ERPDetail
				}},
			// The plant's agent looks for the planned order the order fulfils
			// (ADR-0021); a supervisor approves its proposal before it is resent.
			// Stopped, or without a proposal, the supervisors correct it themselves.
			{Name: "propose", Title: "The agent looks for the planned order", Fault: "correct",
				Agent: &platform.AgentStep{Agent: "erp-fixer", To: supervisors,
					Goal: func(c platform.Caller, r *platform.Run) string {
						o := order(c, r)
						return fmt.Sprintf("The ERP refused the confirmation of shop order %s (product %s, quantity %d): %s. Planned orders already fulfilled by other shop orders: %s.",
							o.ID, o.Product, o.Quantity, o.ERPDetail, strings.Join(p.fulfilled(c), ", "))
					},
					Ref: func(_ platform.Caller, r *platform.Run) string { return OrderType + "/" + r.Key }},
				Choose: func(c platform.Caller, r *platform.Run) (string, string) {
					var proposal struct {
						Planned string `json:"planned"`
					}
					if i, j := strings.Index(r.Answer, "{"), strings.LastIndex(r.Answer, "}"); i >= 0 && j > i {
						json.Unmarshal([]byte(r.Answer[i:j+1]), &proposal)
					}
					if proposal.Planned == "" || !p.claimed(proposal.Planned) {
						return "correct", "the agent found no planned order the ERP sent"
					}
					r.Set(map[string]string{"planned": proposal.Planned})
					return "approve", "the agent proposes " + proposal.Planned
				}},
			{Name: "approve", Title: "A supervisor approves the correction", Ask: &platform.Ask{To: supervisors, Answers: []string{"resend", "correct myself"},
				Title: func(c platform.Caller, r *platform.Run) string {
					return fmt.Sprintf("Resend %s to the ERP against %s?", r.Key, platform.DataOf[map[string]string](r)["planned"])
				},
				Body: func(c platform.Caller, r *platform.Run) string {
					return "The plant's agent proposes the planned order " + platform.DataOf[map[string]string](r)["planned"] + ". The ERP " + order(c, r).ERP + " the first confirmation: " + order(c, r).ERPDetail
				},
				Ref: func(_ platform.Caller, r *platform.Run) string { return OrderType + "/" + r.Key },
				On:  SchemaResend, Match: func(_ platform.Caller, r *platform.Run, e platform.Event) bool {
					return e.Record.GetSubmission().GetTarget().GetId() == r.Key
				}},
				Choose: func(_ platform.Caller, r *platform.Run) (string, string) {
					switch r.Answer {
					case "resend":
						return "resend", "a supervisor approved the agent's correction"
					case "event":
						return "answer", "someone resent it meanwhile"
					}
					return "correct", "a supervisor corrects it"
				}},
			{Name: "resend", Title: "Resend the corrected confirmation", Next: "answer", Act: &platform.Act{Action: SchemaResend, Target: id,
				Payload: func(_ platform.Caller, r *platform.Run) any { return platform.DataOf[map[string]string](r) }}},
			{Name: "correct", Title: "Correct and resend", Ask: &platform.Ask{To: supervisors, Answers: []string{"give up"},
				Title: func(c platform.Caller, r *platform.Run) string { return "Correct and resend " + r.Key + " to the ERP" },
				Body: func(c platform.Caller, r *platform.Run) string {
					return "The ERP " + order(c, r).ERP + " it: " + order(c, r).ERPDetail
				},
				Ref: func(_ platform.Caller, r *platform.Run) string { return OrderType + "/" + r.Key },
				On:  SchemaResend, Match: func(_ platform.Caller, r *platform.Run, e platform.Event) bool {
					return e.Record.GetSubmission().GetTarget().GetId() == r.Key
				}},
				Choose: func(_ platform.Caller, r *platform.Run) (string, string) {
					if r.Answer == "give up" {
						return "", "a supervisor gave up"
					}
					return "answer", "it was resent"
				}},
			{Name: "silent", Title: "Tell the supervisors", Next: "answer", Ask: &platform.Ask{To: supervisors,
				Title: func(_ platform.Caller, r *platform.Run) string {
					return "The ERP has not answered the confirmation of " + r.Key
				},
				Ref: func(_ platform.Caller, r *platform.Run) string { return OrderType + "/" + r.Key }}},
		}}
}

// key names the order's current confirmation: the order, then "<order>#<n>"
// for the n-th corrected one, so the ERP receives a correction as a new message.
func (o *Order) key() string {
	if o.Resent == 0 {
		return o.ID
	}
	return fmt.Sprintf("%s#%d", o.ID, o.Resent+1)
}

// confirm emits the order's confirmation (#101) with its current key and
// stores the order as changed by r.
func (p *Plant) confirm(who platform.Caller, r *pb.ChangeRecord, o Order, now time.Time) {
	done := 0
	var sfcs []string
	for _, id := range o.SFCs {
		sfcs = append(sfcs, string(id))
		if s, _ := platform.Get[SFC](who, string(id)); s.State == "done" {
			done++
		}
	}
	yield := o.Quantity * done / len(o.SFCs)
	c := Confirmation{Order: o.ID, Planned: o.Planned, Product: o.Product, Quantity: o.Quantity, Yield: yield, Scrap: o.Quantity - yield, SFCs: sfcs}
	if n, _ := who.Emit(EffectConfirmation, o.key(), OrderType+"/"+o.ID, c, now); n > 0 {
		o.ERP = "sent"
	}
	who.Put(r, o)
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
	order, known := platform.Get[Order](c, id)
	if !known {
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
	c.PutAt(now, "erp answer", order)
	if a.State != "confirmed" {
		c.Notify(platform.Notification{Title: fmt.Sprintf("ERP %s the confirmation of %s", a.State, order.ID), Body: a.Detail,
			Ref: OrderType + "/" + order.ID, Key: "erp:" + e.ID + ":" + strconv.Itoa(e.Attempts)}, now, p.supervisorsOfOrder(order))
	}
	return nil
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

// fixer is the plant's agent for refused confirmations (ADR-0021): it reads the
// planned orders and proposes the one the order fulfils; it takes no action,
// the flow asks a supervisor.
func (p *Plant) fixer() platform.Agent {
	return platform.Agent{Name: "erp-fixer", Title: "ERP correction",
		Instructions: `The ERP refused a shop order's confirmation, usually because it names no planned order, or the wrong one. Find the ERP planned order the shop order fulfils: the same product and quantity, and not fulfilled by another shop order. Read the planned orders first. Finish with the result as JSON: {"planned": "<ERP planned order ID>"}, or {"planned": ""} when none fits.`,
		Tools:        []string{"read:planned-orders"},
		Budget:       platform.Budget{Steps: 6, Actions: 1},
		To: func(c platform.Caller, r platform.AgentRun) []platform.Recipient {
			id := strings.TrimPrefix(r.Ref, OrderType+"/")
			o, _ := platform.Get[Order](c, id)
			return []platform.Recipient{p.supervisorsOfOrder(o)}
		}}
}

// supervisorsOfOrder are the supervisors of the order's line.
func (p *Plant) supervisorsOfOrder(o Order) platform.Recipient {
	return platform.Recipient{Structure: SiteStructure, Role: string(Supervisor), Unit: p.orderLine(o)}
}

// orderLine is the line where the order's routing starts.
func (p *Plant) orderLine(o Order) string {
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
