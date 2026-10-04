package platformserver

import (
	"math"
	"math/big"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const AggregateWindowMaxRows = 100000

type AggregateWindowQuery struct {
	TimeField string `json:"timeField"`
	Field     string `json:"field"`
	Rows      int    `json:"rows"`
	Threshold string `json:"threshold"`
}
type AggregateWindowEdge struct {
	ID       string    `json:"id"`
	Revision uint32    `json:"revision"`
	Time     time.Time `json:"time"`
}
type AggregateWindowResult struct {
	TimeField     string               `json:"timeField"`
	Field         string               `json:"field"`
	RequestedRows int                  `json:"requestedRows"`
	Threshold     string               `json:"threshold"`
	Generation    string               `json:"generation"`
	Total         int                  `json:"total"`
	Timed         int                  `json:"timed"`
	MissingTime   int                  `json:"missingTime"`
	Count         int                  `json:"count"`
	Valid         int                  `json:"valid"`
	Missing       int                  `json:"missing"`
	Above         int                  `json:"above"`
	Min           *float64             `json:"min,omitempty"`
	Mean          *float64             `json:"mean,omitempty"`
	Max           *float64             `json:"max,omitempty"`
	First         *AggregateWindowEdge `json:"first,omitempty"`
	Last          *AggregateWindowEdge `json:"last,omitempty"`
}

var windowDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func windowThreshold(text string) (*big.Rat, bool) {
	if len(text) < 1 || len(text) > 80 || !windowDecimal.MatchString(text) {
		return nil, false
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return nil, false
	}
	// Check the exponent before constructing a rational with an unbounded denominator.
	if _, exponent, ok := strings.Cut(strings.ToLower(text), "e"); ok {
		n, err := strconv.Atoi(exponent)
		if err != nil || n < -324 || n > 308 {
			return nil, false
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || number == 0 && value.Sign() != 0 {
		return nil, false
	}
	return value, true
}
func windowTime(value reflect.Value) (time.Time, bool, bool) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return time.Time{}, false, true
		}
		value = value.Elem()
	}
	instant, ok := value.Interface().(time.Time)
	if !ok {
		return time.Time{}, false, false
	}
	return instant, true, true
}

// The original member projection/predicate compiler has already run. The caller holds s.mu.
func (s *recordStore) aggregateWindow(et *entityType, q AggregateQuery, visible func(reflect.Value) bool) (Aggregate, *kernel.Error) {
	invalid := func(message string) (Aggregate, *kernel.Error) {
		return Aggregate{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: message}
	}
	w := q.Window
	when, hasTime := et.info.Field(w.TimeField)
	field, hasField := et.info.Field(w.Field)
	threshold, valid := windowThreshold(w.Threshold)
	if !hasTime || when.Type != "datetime" || !hasField || !slices.Contains([]string{"integer", "decimal"}, field.Type) || w.Rows < 1 || w.Rows > AggregateWindowMaxRows || !valid || len(q.Groups) > 0 || len(q.Measures) > 0 || q.Histogram != nil || q.MaxRows != 0 {
		return invalid("Window statistics need readable event time and numeric fields, a bounded row count and decimal threshold; other aggregate modes cannot be mixed.")
	}
	rows, issue := s.matchingQuery(et, platform.Query{Domain: q.Domain, Search: q.Search, Set: q.Set, Archived: q.Archived}, visible)
	if issue != nil {
		return Aggregate{}, issue
	}
	type timedRow struct {
		value reflect.Value
		at    time.Time
		id    string
	}
	timed := make([]timedRow, 0, len(rows))
	out := &AggregateWindowResult{TimeField: w.TimeField, Field: w.Field, RequestedRows: w.Rows, Threshold: w.Threshold, Generation: strconv.FormatUint(s.generation, 10), Total: len(rows)}
	for _, row := range rows {
		at, hasTime, valid := windowTime(row.FieldByIndex(when.Index))
		if !valid {
			return invalid("The original event time has an incompatible runtime type.")
		}
		if !hasTime {
			out.MissingTime++
			continue
		}
		timed = append(timed, timedRow{row, at, recordOf(row).ID})
	}
	slices.SortFunc(timed, func(a, b timedRow) int {
		if a.at.Before(b.at) {
			return 1
		}
		if a.at.After(b.at) {
			return -1
		}
		return strings.Compare(a.id, b.id)
	})
	out.Timed = len(timed)
	timed = timed[:min(len(timed), w.Rows)]
	out.Count = len(timed)
	edge := func(row timedRow) *AggregateWindowEdge {
		record := recordOf(row.value)
		return &AggregateWindowEdge{ID: record.ID, Revision: record.Revision, Time: row.at}
	}
	if len(timed) > 0 {
		out.First = edge(timed[0])
		out.Last = edge(timed[len(timed)-1])
	}
	sum := new(big.Rat)
	var minimum, maximum *big.Rat
	safe := new(big.Rat).SetInt64(9007199254740991)
	for _, row := range timed {
		value, valid := histogramNumber(row.value.FieldByIndex(field.Index))
		if !valid {
			return invalid("The original numeric window has an incompatible or non-finite value.")
		}
		if value == nil {
			out.Missing++
			continue
		}
		if field.Type == "integer" && (!value.IsInt() || new(big.Rat).Abs(value).Cmp(safe) > 0) {
			return invalid("Window integer values exceed the exact JSON number range.")
		}
		out.Valid++
		if value.Cmp(threshold) > 0 {
			out.Above++
		}
		sum.Add(sum, value)
		if minimum == nil || value.Cmp(minimum) < 0 {
			minimum = new(big.Rat).Set(value)
		}
		if maximum == nil || value.Cmp(maximum) > 0 {
			maximum = new(big.Rat).Set(value)
		}
	}
	if out.Valid > 0 {
		mean := new(big.Rat).Quo(sum, new(big.Rat).SetInt64(int64(out.Valid)))
		low, _ := minimum.Float64()
		avg, _ := mean.Float64()
		high, _ := maximum.Float64()
		if math.IsInf(low, 0) || math.IsInf(avg, 0) || math.IsInf(high, 0) || math.IsNaN(avg) {
			return invalid("Window statistics cannot be represented as finite JSON numbers.")
		}
		out.Min = &low
		out.Mean = &avg
		out.Max = &high
	}
	return Aggregate{Window: out, Columns: []Column{}, Rows: []map[string]any{}}, nil
}
