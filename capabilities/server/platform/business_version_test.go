package platform

import "testing"

func TestBusinessEffectiveTimeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		kind, from, until, at string
		active                bool
	}{
		{"date", "2026-01-01", "2026-02-01", "2026-01-01", true},
		{"date", "2026-01-01", "2026-02-01", "2026-02-01", false},
		{"datetime", "2026-01-01T08:00:00+08:00", "2026-01-01T09:00:00+08:00", "2026-01-01T00:30:00Z", true},
		{"datetime", "2026-01-01T08:00:00+08:00", "2026-01-01T09:00:00+08:00", "2026-01-01T01:00:00Z", false},
	} {
		got, err := EffectiveAt(tc.kind, tc.from, tc.until, tc.at)
		if err != nil || got != tc.active {
			t.Fatalf("%+v: %v %v", tc, got, err)
		}
		if tc.kind == "date" && got != ActiveOn(tc.from, tc.until, tc.at) {
			t.Fatal("enterprise and record business dates differ")
		}
	}
	if _, err := EffectiveAt("date", "2026-02-01", "2026-01-01", "2026-01-01"); err == nil {
		t.Fatal("inverted validity interval accepted")
	}
}
