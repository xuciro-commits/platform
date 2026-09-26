// Package production is the production order protocol (ADR-0024 D7): what an
// ERP provides and a plant consumes, without either knowing the other. It
// follows ISA-95 and SAP's integration of production orders with an MES: the
// ERP releases orders for the plant to execute and receives confirmations of
// what was made (yield) and lost (scrap), accepting or refusing each.
package production

import "platformserver/platform"

const ID = "production.orders/1"

// Order is the protocol's read shape: a production order and where it stands.
type Order struct {
	ID       string  `json:"id"`
	Number   string  `json:"number"`
	Product  string  `json:"product"`
	Quantity float64 `json:"quantity"`
	Due      string  `json:"due,omitempty"`
	State    string  `json:"state"` // released: the plant may execute it; confirmed: done
}

// Confirmation is the payload of confirm.
type Confirmation struct {
	ShopOrder string  `json:"shopOrder"`
	Yield     float64 `json:"yield"`
	Scrap     float64 `json:"scrap"`
}

func Protocol() platform.Protocol {
	return platform.Protocol{Name: "production.orders", Version: 1,
		Actions: []platform.Action{{Schema: "confirm", Title: "Confirm production order",
			Description: "Confirm what a released production order made and lost; refused when the provider cannot accept it.",
			Payload: []platform.Field{{Name: "shopOrder", Type: "string", Required: true, Description: "The plant's order that executed it"},
				{Name: "yield", Type: "number", Required: true, Description: "Good units made"},
				{Name: "scrap", Type: "number", Required: true, Description: "Units scrapped"}}}},
		Reads:  []string{"orders"},
		Events: []platform.ProtocolEvent{{Name: "released", Title: "Production order released"}},
	}
}
