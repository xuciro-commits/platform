package build

import "testing"

func TestTypedConditionsDoNotFallBackToTextOrder(t *testing.T) {
	if holds(nil, "integer", "<", "500", "") {
		t.Fatal("an absent value passed an ordered condition")
	}
	if holds("twenty", "integer", "<", "500", "") {
		t.Fatal("malformed numeric data passed by lexical order")
	}
	if !holds("2026-10-01T09:00:00Z", "date", "=", "2026-10-01", "") {
		t.Fatal("a stored timestamp did not compare with its authored calendar date")
	}
}
