package mes

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"production"
)

// Confirming to the ERP (#101, ADR-0024 7d): when the last SFC of an order is
// done or scrapped, the plant confirms the order to the ERP, as SAP's production
// order confirmation does, through production.orders/1 (production.go). A
// refusal reaches the line's supervisors, and the confirmation flow asks them,
// or the plant's agent, to correct and resend it.

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
	verdict := func(c platform.Caller, r *platform.Run) (string, string) {
		o := order(c, r)
		if o.ERP == "confirmed" {
			return "", "the ERP confirmed it as " + o.Confirmation
		}
		return "propose", "the ERP " + o.ERP + " it: " + o.ERPDetail
	}
	return platform.Flow{Name: "erp-confirmation", Title: "Confirm to the ERP", Version: 1, Owners: []string{string(Supervisor)},
		Start: platform.Start{On: []string{SchemaComplete, SchemaSign}, Begin: func(c platform.Caller, e platform.Event) (string, any, bool) {
			sfc, _ := platform.Get[SFC](c, e.Record.GetSubmission().GetTarget().GetId())
			o, known := platform.Get[Order](c, string(sfc.Order))
			_, erp := erpOrders(c)
			return o.ID, nil, erp && known && o.Status == "completed" && o.ERP == ""
		}},
		Steps: []platform.Step{
			{Name: "confirm", Title: "Confirm the order", Act: &platform.Act{Action: SchemaConfirm, Target: id}, Next: "answer"},
			{Name: "answer", Title: "Wait for the ERP's answer", Timeout: time.Hour, OnTimeout: "silent",
				Wait: &platform.Wait{Until: func(c platform.Caller, r *platform.Run) bool {
					o := order(c, r)
					_, answered := awaited(c, o)
					return answered || o.ERP != "" && o.ERP != "sent"
				}},
				Choose: func(c platform.Caller, r *platform.Run) (string, string) {
					if order(c, r).ERP == "sent" {
						return "record", "the ERP answered"
					}
					return verdict(c, r)
				}},
			{Name: "record", Title: "Record the ERP's answer", Act: &platform.Act{Action: SchemaAnswer, Target: id}, Choose: verdict},
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
					if proposal.Planned == "" {
						return "correct", "the agent found no planned order"
					}
					if why := p.fits(c, order(c, r), proposal.Planned); why != "" {
						return "correct", "the agent's proposal does not fit: " + why
					}
					r.Set(map[string]string{"planned": proposal.Planned})
					return "approve", "the agent proposes " + proposal.Planned
				}},
			{Name: "approve", Title: "A supervisor approves the correction", Ask: &platform.Ask{To: supervisors, Answers: []string{"resend", "correct myself"}, Reviews: "propose",
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

// EffectLeadTime asks a supplier's agent, over A2A, how soon it can deliver.
const EffectLeadTime = "lead-time"

// planner is the plant's planning agent (ADR-0022 D9 (3)): it asks the
// supplier's agent — an external A2A agent the tenant binds to the lead-time
// effect — and answers with what it said.
func (p *Plant) planner() platform.Agent {
	return platform.Agent{Name: "planner", Title: "Material planner",
		Description:  "Answers planning questions about the plant's products and planned orders, asking suppliers' agents for lead times.",
		Instructions: `You answer a supervisor's planning question. Read the planned orders when the question is about them. For a supplier's lead time, ask the supplier's agent with emit_lead_time, naming the product, and wait for its answer. Finish with the answer as JSON: {"product": "...", "leadTimeDays": n, "supplier": "..."} when you have one, else in a sentence.`,
		Tools:        []string{"emit:" + EffectLeadTime, "read:planned-orders"},
		Budget:       platform.Budget{Steps: 6, Actions: 2}}
}

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
