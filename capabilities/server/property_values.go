package platformserver

import (
	"encoding/json"
	"platformserver/platform"
	"reflect"
	"strconv"
	"strings"
)

// Projection runs after the original record mask and uses the member's field
// descriptions. Numeric tokens are retained before the browser parses JSON.
func propertyValues(info platform.EntityInfo, record any) (map[string]any, map[string]string) {
	values, errors := map[string]any{}, map[string]string{}
	row := reflect.ValueOf(record)
	for _, field := range info.Fields {
		typ := platform.PropertyValueType(field)
		if typ == "" {
			continue
		}
		value := row.FieldByIndex(field.Index)
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				continue
			}
			value = value.Elem()
		}
		if typ == "decimal" {
			raw, err := json.Marshal(value.Interface())
			if err != nil {
				errors[field.Name] = "Property value is unavailable."
				continue
			}
			text := string(raw)
			if at := strings.IndexAny(text, "eE"); at >= 0 {
				exponent, err := strconv.Atoi(text[at+1:])
				if err != nil || exponent < -308 || exponent > 308 {
					errors[field.Name] = "Property value exceeds its budget."
					continue
				}
				mantissa := text[:at]
				sign := ""
				if strings.HasPrefix(mantissa, "-") {
					sign = "-"
					mantissa = mantissa[1:]
				}
				parts := strings.Split(mantissa, ".")
				digits := strings.Join(parts, "")
				point := len(parts[0]) + exponent
				if point <= 0 {
					text = sign + "0." + strings.Repeat("0", -point) + digits
				} else if point >= len(digits) {
					text = sign + digits + strings.Repeat("0", point-len(digits))
				} else {
					text = sign + digits[:point] + "." + digits[point:]
				}
			}
			parsed, err := platform.ParseDecimal(text)
			if err != nil {
				errors[field.Name] = "Property value exceeds its budget."
			} else {
				values[field.Name] = parsed
			}
			continue
		}
		if typ == "string" && value.Kind() == reflect.String {
			values[field.Name] = value.String()
		} else if typ == "boolean" && value.Kind() == reflect.Bool {
			values[field.Name] = value.Bool()
		} else {
			errors[field.Name] = "Property value is unavailable."
		}
	}
	return values, errors
}
