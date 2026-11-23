package expr

import "testing"

func TestAggregateOverDifferentCurrenciesHasNoTotal(t *testing.T) {
	money := func(amount, currency string) Value {
		return PresentRaw("money", []byte(`{"amount":`+amount+`,"currency":"`+currency+`"}`))
	}
	for _, op := range []string{"sum", "avg", "min", "max"} {
		mixed, err := NewNumericAggregate(op, "money", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []Value{money("100", "USD"), money("200", "CNY"), money("300", "USD")} {
			if err := mixed.Add(v); err != nil {
				t.Fatalf("%s: adding a value in another currency must not fail: %v", op, err)
			}
		}
		if got, err := mixed.Result(); err != nil || got.Status != "null" {
			t.Fatalf("%s over USD and CNY must have no total, got %+v %v", op, got, err)
		}
	}
	same, _ := NewNumericAggregate("sum", "money", "")
	for _, v := range []Value{money("100", "USD"), money("250", "USD")} {
		if err := same.Add(v); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := same.Result(); err != nil || got.Status != "value" {
		t.Fatalf("a sum in one currency keeps its total, got %+v %v", got, err)
	}
}
