package platformserver

import (
	"encoding/json"
	"fmt"
	"math/big"
	"platformserver/platform"
	"reflect"
)

func exactDecimalCondition(field platform.FieldInfo, op string, value platform.DecimalValue, read func(reflect.Value) any) (func(reflect.Value) bool, error) {
	if field.Type != "integer" && field.Type != "decimal" {
		return nil, fmt.Errorf("decimal condition needs a numeric field")
	}
	if op != "=" && op != "!=" && op != "<" && op != "<=" && op != ">" && op != ">=" {
		return nil, fmt.Errorf("unsupported decimal condition")
	}
	right := value.Rat()
	return func(record reflect.Value) bool {
		raw, err := json.Marshal(read(record))
		if err != nil {
			return false
		}
		left, ok := new(big.Rat).SetString(string(raw))
		if !ok {
			return false
		}
		cmp := left.Cmp(right)
		switch op {
		case "=":
			return cmp == 0
		case "!=":
			return cmp != 0
		case "<":
			return cmp < 0
		case "<=":
			return cmp <= 0
		case ">":
			return cmp > 0
		case ">=":
			return cmp >= 0
		}
		return false
	}, nil
}
