package mes

import "platformserver/platform"

// Actions is the plant's action catalog (ADR-0008): screens, integrations and AI
// agents receive the part their role may call; line conditions stay in allowed.
func Actions() *platform.Catalog {
	roles := func(r ...Role) []string {
		out := make([]string, len(r))
		for i, x := range r {
			out[i] = string(x)
		}
		return out
	}
	return platform.NewCatalog(append(append([]platform.Action{
		{Schema: SchemaRelease, Target: OrderType, New: true, Capability: "orders", Title: "Release shop order",
			Description: "Release a shop order for a product; it splits into SFCs that start at the routing's first operation. Name the ERP planned order it fulfils.",
			Payload: []platform.Field{{Name: "product", Type: "string", Required: true, Description: "Product ID"},
				{Name: "quantity", Type: "integer", Required: true, Description: "Units to make"},
				{Name: "sfcs", Type: "integer", Required: true, Description: "Number of SFCs (lots), at most the quantity"},
				{Name: "planned", Type: "string", Description: "ERP planned order ID"}},
			Roles: roles(Supervisor)},
	}, platform.EntityActions(Entities(nil)[1])...),
		platform.Action{Schema: SchemaReason, Target: DowntimeType, Capability: "downtime-reasons", Title: "Assign downtime reason",
			Description: "Assign the reason of a downtime event on a resource of your lines.",
			Payload: []platform.Field{{Name: "reason", Type: "string", Required: true, Description: "Tool change, Setup, Material shortage, Breakdown or Quality issue",
				Choices: []string{"Tool change", "Setup", "Material shortage", "Breakdown", "Quality issue"}}},
			Roles: roles(Supervisor, Operator, Assistant)},
		platform.Action{Schema: SchemaConfirm, Target: OrderType, Capability: "orders", Title: "Confirm order to the ERP",
			Description: "Confirm a completed order's yield and scrap to the ERP; the plant's confirmation flow does it when the order's last SFC ends.",
			Payload:     []platform.Field{}, Roles: roles(Supervisor)},
		platform.Action{Schema: SchemaAnswer, Target: OrderType, Capability: "orders", Title: "Record the ERP's answer",
			Description: "Record on a sent order how the ERP answered its confirmation: the platform does it with the ERP's answer, and the plant's confirmation flow when an ERP outside answers later.",
			Payload:     platform.AnswerFields(), Roles: roles(Supervisor)},
		platform.Action{Schema: SchemaResend, Target: OrderType, Capability: "orders", Title: "Resend confirmation to the ERP",
			Description: "Correct an order whose confirmation the ERP refused, or that never arrived, and confirm it again: name the ERP planned order it fulfils when that was missing or wrong.",
			Payload:     []platform.Field{{Name: "planned", Type: "string", Description: "ERP planned order ID (a planned order the ERP sent)"}},
			Roles:       roles(Supervisor, Assistant)},
	)...)
}
