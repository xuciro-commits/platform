package build

import (
	"slices"

	"platformserver/platform"
)

// Markings (ADR-0075, ADR-0069 Ⅰ-I): a bounded classification that travels
// with data. A connection or dataset is marked internal, confidential or
// restricted; what is loaded or run from it can only raise the marking of
// where it lands, never lower it. There is no second permission engine: a
// marking compiles to what already exists - an object field fed with
// confidential or restricted data must name the roles that read it (ADR-0028
// D3), and restricted datasets never leave as CSV.
const (
	Integrator = "integrator" // connects, maps and runs integrations; never designs objects or pages
)

// Markings in ascending order; "" is unmarked.
var Markings = []string{"", "internal", "confidential", "restricted"}

func MarkingRank(m string) int { return max(slices.Index(Markings, m), 0) }

// HigherMarking is the stricter of the two.
func HigherMarking(a, b string) string {
	if MarkingRank(b) > MarkingRank(a) {
		return b
	}
	return a
}

// Guarded says whether data of this marking may only reach fields with named readers.
func Guarded(marking string) bool { return MarkingRank(marking) >= MarkingRank("confidential") }

// UnguardedFields are the object fields a pipeline would write that every
// role reads - what a guarded marking refuses to pour into.
func UnguardedFields(info platform.EntityInfo, columns []string) []string {
	var open []string
	for _, f := range info.Fields {
		if len(f.Read) == 0 && (columns == nil || slices.Contains(columns, f.Name)) {
			open = append(open, f.Name)
		}
	}
	return open
}
