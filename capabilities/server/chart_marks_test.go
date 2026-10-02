package platformserver

import (
	"platformserver/platform"
	"testing"
)

func TestChartMarksCheckTypedTrendAndComparablePie(t *testing.T) {
	info := platform.EntityInfo{Fields: []platform.FieldInfo{{Name: "bucket", Type: "text"}, {Name: "date", Type: "date"}, {Name: "qty", Type: "integer"}, {Name: "amount", Type: "money"}}}
	for _, s := range []platform.Section{{Widget: "chart", Group: "bucket", Measure: "count"}, {Widget: "chart", Mark: "line", Group: "date:month", Measure: "avg:qty"}, {Widget: "chart", Mark: "area", Group: "created:day", Measure: "sum:amount"}, {Widget: "chart", Mark: "arc", Group: "bucket", Measure: "sum:qty"}} {
		if !checkAggregateSection(s, info) {
			t.Fatalf("supported chart rejected: %+v", s)
		}
	}
	for _, s := range []platform.Section{{Widget: "chart", Mark: "vega", Group: "bucket", Measure: "count"}, {Widget: "chart", Mark: "line", Group: "bucket", Measure: "count"}, {Widget: "chart", Mark: "area", Group: "date", Measure: "count"}, {Widget: "chart", Mark: "arc", Group: "bucket", Measure: "avg:qty"}, {Widget: "chart", Mark: "arc", Group: "bucket", Measure: "sum:amount"}} {
		if checkAggregateSection(s, info) {
			t.Fatalf("misleading chart accepted: %+v", s)
		}
	}
}
