package records

import (
	"platformserver/platform"
	"reflect"
	"strconv"
	"testing"
)

func BenchmarkRecordsAggregate10k(b *testing.B) {
	type row struct {
		platform.Record
		Label  string
		Amount int64
	}
	rows := make([]reflect.Value, 10000)
	for i := range rows {
		item := &row{Label: strconv.Itoa(i % 50), Amount: int64(i)}
		item.ID = strconv.Itoa(10000 + i)
		rows[i] = reflect.ValueOf(item).Elem()
	}
	et := &Type{info: platform.RecordTypeInfo{Fields: []platform.FieldInfo{{Name: "label", Type: "text", Index: []int{1}}, {Name: "amount", Type: "integer", Index: []int{2}}}}}
	s := NewStore()
	for _, tc := range []struct {
		name string
		q    AggregateQuery
	}{{"count", AggregateQuery{Groups: []string{"label"}}}, {"histogram", AggregateQuery{Histogram: &HistogramQuery{Field: "amount", Bins: 10}}}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.Aggregate(et, tc.q, rows, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
