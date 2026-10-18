package production

import "platformserver/platform"

// BatchesID leaves the original whole-order protocol available to its existing
// providers and histories. Each batch is an independent provider decision.
const BatchesID = "production.orders/2"

type Start struct {
	ShopOrder string `json:"shoporder"`
}

type BatchConfirmation struct {
	ShopOrder string  `json:"shoporder"`
	Batch     string  `json:"batch"`
	Yield     float64 `json:"yield"`
	Scrap     float64 `json:"scrap"`
	Final     bool    `json:"final"`
}

// BatchOrder is the bounded orders read. Yield and scrap accumulate across
// shop orders; final explicitly closes the upstream order, retaining any shortage.
type BatchOrder struct {
	ID        string  `json:"id"`
	Number    string  `json:"number"`
	Product   string  `json:"product"`
	Quantity  float64 `json:"quantity"`
	Yield     float64 `json:"yield"`
	Scrap     float64 `json:"scrap"`
	Remaining float64 `json:"remaining"`
	State     string  `json:"state"`
}

func BatchesProtocol() platform.Protocol {
	return platform.Protocol{Name: "production.orders", Version: 2,
		Actions: []platform.Action{
			{Schema: "start", Title: "Start production order", Description: "Record the first shop-order start independently of later batch confirmations.", Payload: []platform.Field{{Name: "shoporder", Type: "string", Required: true}}},
			{Schema: "confirm", Title: "Confirm production batch", Description: "Accept one stable batch identity and its yield and scrap; reject cumulative overflow and retain shortages on explicit final closure.", Payload: []platform.Field{{Name: "shoporder", Type: "string", Required: true}, {Name: "batch", Type: "string", Required: true}, {Name: "yield", Type: "number", Required: true}, {Name: "scrap", Type: "number", Required: true}, {Name: "final", Type: "boolean", Required: true}}},
		}, Reads: []string{"orders"}, Events: []platform.ProtocolEvent{{Name: "released", Title: "Production order released"}},
	}
}
