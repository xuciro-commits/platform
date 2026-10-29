package records

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestSortedPageKeepsRequestedPrefixAndExactTotal(t *testing.T) {
	type item struct {
		platform.Record
		Number int    `json:"number"`
		Name   string `json:"name"`
	}
	info, err := platform.Describe("example", platform.Entity{Type: "example.item", Model: item{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	order, err := CompileOrder(info, []string{"-number", "name"})
	if err != nil {
		t.Fatal(err)
	}
	var values []item
	for i := range 2000 {
		values = append(values, item{Record: platform.Record{ID: fmt.Sprintf("%04d", 1999-i)}, Number: i % 7, Name: fmt.Sprint(i % 3)})
	}
	expected := slices.Clone(values)
	slices.SortFunc(expected, func(a, b item) int {
		if a.Number != b.Number {
			return b.Number - a.Number
		}
		if n := strings.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, window := range []struct{ offset, limit int }{{0, 1}, {7, 3}, {-2, 5}, {5000, 4}, {0, 0}, {10, -1}, {2, int(^uint(0) >> 1)}} {
		t.Run(fmt.Sprintf("%d/%d", window.offset, window.limit), func(t *testing.T) {
			page := order.Page(window.offset, window.limit)
			for _, value := range values {
				page.Add(Copy(info.Go, value))
				if window.limit > 0 && window.offset < 2000 && window.limit < 2000 && len(page.rows) > max(0, window.offset)+window.limit {
					t.Fatal("page retained unrelated matching rows")
				}
			}
			got, total := page.Result()
			if total != len(values) {
				t.Fatal("paging changed total", total)
			}
			start := min(max(window.offset, 0), len(expected))
			end := len(expected)
			if window.limit > 0 {
				end = start + min(window.limit, end-start)
			}
			if len(got) != end-start {
				t.Fatal("page length differs", len(got), end-start)
			}
			for i, row := range got {
				if !reflect.DeepEqual(row.Interface().(item), expected[start+i]) {
					t.Fatalf("incorrect row at %d: %+v, want %+v", i, row.Interface(), expected[start+i])
				}
			}
		})
	}
}
