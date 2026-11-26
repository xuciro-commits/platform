package expr

import (
	"errors"
	"testing"
)

func TestFormulaErrorsPointAtTheText(t *testing.T) {
	fields := map[string]string{"price": "decimal", "qty": "integer"}
	for _, c := range []struct{ text, at, message string }{
		{"price * qtty", "qtty", "undeclared formula field qtty"},
		{"  price * (qty", "(", "a parenthesis is not closed"},
		{"price * 1.2.3", "1.2.3", `"1.2.3" is not a number`},
		{"price #", "#", `unexpected "#" in the formula`},
		{"price *", "", "the formula ends too early"},
	} {
		_, err := LowerFormula(c.text, fields)
		var at *FormulaError
		if !errors.As(err, &at) || at.Message != c.message {
			t.Fatalf("%q: got %v, want %q", c.text, err, c.message)
		}
		if got := c.text[at.Offset:at.End]; got != c.at {
			t.Fatalf("%q: points at %q, want %q", c.text, got, c.at)
		}
	}
}
