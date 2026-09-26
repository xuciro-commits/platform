package mes

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
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
