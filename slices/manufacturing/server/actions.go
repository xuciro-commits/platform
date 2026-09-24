package mes

import "platformserver"

// Actions is the plant's action catalog (ADR-0008): screens, integrations and AI
// agents receive the part their role may call; line conditions stay in allowed.
func Actions() *platformserver.Catalog {
	roles := func(r ...Role) []string {
		out := make([]string, len(r))
		for i, x := range r {
			out[i] = string(x)
		}
		return out
	}
	resource := platformserver.Field{Name: "resource", Type: "string", Required: true, Description: "Resource of the operation's work center"}
	return platformserver.NewCatalog(
		platformserver.Action{Schema: SchemaRelease, Target: OrderType, Capability: "orders", Title: "Release shop order",
			Description: "Release a shop order for a product; it splits into SFCs that start at the routing's first operation. Name the ERP planned order it fulfils, with that claim as evidence.",
			Payload: []platformserver.Field{{Name: "product", Type: "string", Required: true, Description: "Product ID"},
				{Name: "quantity", Type: "integer", Required: true, Description: "Units to make"},
				{Name: "sfcs", Type: "integer", Required: true, Description: "Number of SFCs (lots), at most the quantity"},
				{Name: "planned", Type: "string", Description: "ERP planned order ID"}},
			Roles: roles(Supervisor)},
		platformserver.Action{Schema: SchemaStart, Target: SFCType, Capability: "execution", Title: "Start operation",
			Description: "Start the SFC's current operation on a resource of its work center.", Payload: []platformserver.Field{resource},
			Roles: roles(Operator)},
		platformserver.Action{Schema: SchemaComplete, Target: SFCType, Capability: "execution", Title: "Complete operation",
			Description: "Complete the SFC's active operation; it moves to the next operation or is done.", Payload: []platformserver.Field{},
			Roles: roles(Operator)},
		platformserver.Action{Schema: SchemaNC, Target: SFCType, Capability: "quality", Title: "Log nonconformance",
			Description: "Log a nonconformance at the current operation; the SFC is held until quality signs a disposition.",
			Payload:     []platformserver.Field{{Name: "code", Type: "string", Required: true, Description: "NC code: POROSITY, DIMENSION, SURFACE, LEAK"}},
			Roles:       roles(Operator, Quality)},
		platformserver.Action{Schema: SchemaSign, Target: SFCType, Capability: "quality", Title: "Sign disposition",
			Description: "Sign the disposition of a held SFC (electronic signature with meaning).",
			Payload: []platformserver.Field{{Name: "action", Type: "string", Required: true, Description: "rework, scrap or use-as-is"},
				{Name: "meaning", Type: "string", Required: true, Description: "reviewed or approved"},
				{Name: "reworkStep", Type: "integer", Description: "Operation step to rework from"}},
			Roles: roles(Quality)},
		platformserver.Action{Schema: SchemaReason, Target: DowntimeType, Capability: "downtime-reasons", Title: "Assign downtime reason",
			Description: "Assign the reason of a downtime event on a resource of your lines.",
			Payload:     []platformserver.Field{{Name: "reason", Type: "string", Required: true, Description: "Tool change, Setup, Material shortage, Breakdown or Quality issue"}},
			Roles:       roles(Supervisor, Operator, Assistant)},
	)
}
