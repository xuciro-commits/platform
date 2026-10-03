package platformserver

import (
	"math"
	"math/big"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const HistogramMaxBins = 64

type HistogramQuery struct {
	Field string `json:"field"`
	Bins  int    `json:"bins"`
}
type HistogramBucket struct {
	Lower          string `json:"lower"`
	Upper          string `json:"upper"`
	UpperInclusive bool   `json:"upperInclusive"`
	Count          int    `json:"count"`
}
type HistogramResult struct {
	Field         string            `json:"field"`
	RequestedBins int               `json:"requestedBins"`
	Valid         int               `json:"valid"`
	Missing       int               `json:"missing"`
	Minimum       string            `json:"minimum"`
	Maximum       string            `json:"maximum"`
	Buckets       []HistogramBucket `json:"buckets"`
}

func histogramNumber(v reflect.Value) (*big.Rat, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, true
		}
		v = v.Elem()
	}
	var text string
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		text = strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		text = strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		x := v.Float()
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, false
		}
		text = strconv.FormatFloat(x, 'g', -1, v.Type().Bits())
	default:
		return nil, false
	}
	value, ok := new(big.Rat).SetString(text)
	return value, ok
}
func histogramBound(value *big.Rat) string {
	denominator := new(big.Int).Set(value.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	a, b := 0, 0
	for new(big.Int).Mod(denominator, two).Sign() == 0 {
		denominator.Div(denominator, two)
		a++
	}
	for new(big.Int).Mod(denominator, five).Sign() == 0 {
		denominator.Div(denominator, five)
		b++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return value.RatString()
	}
	text := value.FloatString(max(a, b))
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text
}
func (s *recordStore) histogram(et *entityType, q AggregateQuery, visible func(reflect.Value) bool) (Aggregate, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	h := q.Histogram
	field, ok := et.info.Field(h.Field)
	if !ok || !slices.Contains([]string{"integer", "decimal"}, field.Type) || h.Bins < 1 || h.Bins > HistogramMaxBins || len(q.Groups) > 0 || len(q.Measures) > 0 && !slices.Equal(q.Measures, []string{"count"}) || q.MaxRows > 0 && q.MaxRows < h.Bins {
		return Aggregate{}, invalid
	}
	rows, err := s.matchingQuery(et, platform.Query{Domain: q.Domain, Search: q.Search, Set: q.Set, Archived: q.Archived}, visible)
	if err != nil {
		return Aggregate{}, err
	}
	result := &HistogramResult{Field: h.Field, RequestedBins: h.Bins, Buckets: []HistogramBucket{}}
	values := make([]*big.Rat, 0, len(rows))
	var minimum, maximum *big.Rat
	for _, row := range rows {
		value, ok := histogramNumber(row.FieldByIndex(field.Index))
		if !ok {
			return Aggregate{}, invalid
		}
		if value == nil {
			result.Missing++
			continue
		}
		values = append(values, value)
		if minimum == nil || value.Cmp(minimum) < 0 {
			minimum = new(big.Rat).Set(value)
		}
		if maximum == nil || value.Cmp(maximum) > 0 {
			maximum = new(big.Rat).Set(value)
		}
	}
	result.Valid = len(values)
	out := Aggregate{Columns: []Column{}, Rows: []map[string]any{}, Histogram: result}
	if minimum == nil {
		return out, nil
	}
	result.Minimum = histogramBound(minimum)
	result.Maximum = histogramBound(maximum)
	if minimum.Cmp(maximum) == 0 {
		result.Buckets = []HistogramBucket{{Lower: result.Minimum, Upper: result.Maximum, UpperInclusive: true, Count: len(values)}}
		return out, nil
	}
	span := new(big.Rat).Sub(maximum, minimum)
	step := new(big.Rat).Quo(span, new(big.Rat).SetInt64(int64(h.Bins)))
	edges := make([]*big.Rat, h.Bins+1)
	for i := range edges {
		edges[i] = new(big.Rat).Add(minimum, new(big.Rat).Mul(step, new(big.Rat).SetInt64(int64(i))))
	}
	result.Buckets = make([]HistogramBucket, h.Bins)
	for i := range result.Buckets {
		result.Buckets[i] = HistogramBucket{Lower: histogramBound(edges[i]), Upper: histogramBound(edges[i+1]), UpperInclusive: i == h.Bins-1}
	}
	for _, value := range values {
		ratio := new(big.Rat).Quo(new(big.Rat).Sub(value, minimum), step)
		index := new(big.Int).Quo(ratio.Num(), ratio.Denom()).Int64()
		if index == int64(h.Bins) {
			index--
		}
		if index < 0 || index >= int64(h.Bins) {
			return Aggregate{}, invalid
		}
		result.Buckets[index].Count++
	}
	return out, nil
}
