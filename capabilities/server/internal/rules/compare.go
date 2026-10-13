package rules

// CompareValues shares the evaluator's scalar ordering with declared sets.
func CompareValues(a, b Value) (int, error) {
	left, err := validateValue(a, "order")
	if err != nil {
		return 0, err
	}
	right, err := validateValue(b, "order")
	if err != nil {
		return 0, err
	}
	if a.Status != "value" || b.Status != "value" {
		return 0, fail("missing", "order", "set ordering requires present values")
	}
	return compare(left, right, "order")
}
