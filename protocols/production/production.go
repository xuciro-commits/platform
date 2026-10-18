// Package production is the production order protocol (ADR-0026): what an
// ERP provides and a plant consumes, without either knowing the other. It
// follows ISA-95 and SAP's integration of production orders with an MES: the
// ERP releases orders for the plant to execute and receives confirmations of
// what was made (yield) and lost (scrap), accepting or refusing each. A
// provider that decides at once (the ERP app) confirms or refuses in the
// consumer's decision; one that asks an ERP outside (the adapter, 7d) takes the
// confirmation as sent and shows the ERP's answer on the order when it comes.
package production

import "platformserver/platform"

const ID = "production.orders/1"

// Order is the protocol's read shape: a production order and where it stands.
type Order struct {
	ID       string  `json:"id" source:"record" from:"Source"`
	Number   string  `json:"number" source:"record" from:"Source#number"`
	Product  string  `json:"product" source:"record" from:"Source#product"`
	Quantity float64 `json:"quantity" source:"record" from:"Source#quantity"`
	Due      string  `json:"due,omitempty" source:"record" from:"Source#due"`
	// State: released, the plant may execute it; sent, a confirmation is on its
	// way to the ERP; confirmed, done; refused or failed, the last confirmation
	// was refused or never arrived, and the order may be confirmed again.
	State              string `json:"state" source:"record" from:"Source#state"`
	ShopOrder          string `json:"shopOrder,omitempty" source:"record" from:"Source#shopOrder"`             // the consumer's order its last confirmation named
	Confirmation       string `json:"confirmation,omitempty" source:"record" from:"ConfirmationSource#number"` // the ERP's number for the confirmation
	Detail             string `json:"detail,omitempty"`                                                        // why it was refused or failed
	Source             string `json:"-"`
	ConfirmationSource string `json:"-"`
}

// Answered reports whether the ERP has answered shop order's confirmation of o.
func (o Order) Answered(shopOrder string) bool {
	return o.ShopOrder == shopOrder && (o.State == "confirmed" || o.State == "refused" || o.State == "failed")
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
