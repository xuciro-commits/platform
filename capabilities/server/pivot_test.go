package platformserver

import (
	"platformserver/platform"
	"testing"
)

func TestPivotAxesUseTheOriginalAggregateFieldTypes(t *testing.T) {
	info := platform.EntityInfo{Fields: []platform.FieldInfo{{Name: "row", Type: "text"}, {Name: "column", Type: "boolean"}, {Name: "qty", Type: "integer"}}}
	valid := platform.Section{Widget: "pivot", Group: "row", ColumnGroup: "column", Measure: "sum:qty"}
	if !checkAggregateSection(valid, info) {
		t.Fatal("valid two-axis aggregate rejected")
	}
	for _, s := range []platform.Section{
		{Widget: "pivot", ColumnGroup: "column", Measure: "count"},
		{Widget: "pivot", Group: "row", ColumnGroup: "row", Measure: "count"},
		{Widget: "pivot", Group: "row", ColumnGroup: "hidden", Measure: "count"},
		{Widget: "pivot", Group: "row", ColumnGroup: "column:month", Measure: "count"},
		{Widget: "pivot", Group: "row", ColumnGroup: "column", Measure: "sum:row"},
	} {
		if checkAggregateSection(s, info) {
			t.Fatalf("invalid axis/measure accepted: %+v", s)
		}
	}
}
