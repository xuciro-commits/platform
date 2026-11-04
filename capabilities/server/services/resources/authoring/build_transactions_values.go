package build

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"platformserver/platform"
)

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
