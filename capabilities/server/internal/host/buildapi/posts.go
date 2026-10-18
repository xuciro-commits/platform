package buildapi

import (
	"encoding/json"
	"fmt"

	"strconv"
	"strings"

	"platformserver/platform"
)

// Postings (ADR-0039、ADR-0038 block E3): an action adds to or subtracts from
// a running balance kept on another object of this builder - a stock balance
// per item and location, a received quantity per order line, a spent amount
// per budget. The balance record's identity is the posting's match fields, so
// two actions that name the same item and location meet the same balance; it
// is created on first posting and changed under the same decision as the
// action (like Creates, ADR-0047 21c). A reversal is a posting of the other
// sign; there is no delete.
type Post struct {
	Object string `json:"object" field:"required" help:"A defined object of this builder that keeps the balance" example:"build.stockbalance"`
	// Match names the balance's identity: its fields and where each value
	// comes from (an input, record.<field> of this record, $me, or =<literal>).
	Match []Set `json:"match" title:"Balance identified by"`
	// Field is the balance's integer, decimal or money field the amount goes to.
	Field string `json:"field" field:"required" help:"The balance's integer, decimal or money field" example:"onhand"`
	// Amount is an input's name, record.<field>, or =<number>.
	Amount string `json:"amount" field:"required" example:"quantity"`
	// Subtract posts the negative amount.
	Subtract bool `json:"subtract,omitempty" title:"Subtract"`
	// Floor, when set, refuses a posting that would take the balance below zero.
	Floor bool `json:"floor,omitempty" title:"Never below zero"`
}

// number reads an input or field value as the amount it posts: a number, a
// numeric string, or money (its minor units).
func ValueNumber(x any) (float64, error) {
	switch n := x.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	case map[string]any:
		if amount, ok := n["amount"]; ok {
			return ValueNumber(amount)
		}
	case platform.Money:
		return float64(n.Amount), nil
	}
	return 0, fmt.Errorf("not a number")
}
