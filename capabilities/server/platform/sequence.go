package platform

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Sequence numbers an app's documents without gaps (ADR-0024 D3), as SAP's
// number ranges and Odoo's journal sequences do: a number is taken only by a
// decision that is accepted, so a refused one leaves no hole, and replay takes
// the same numbers again.
type Sequence struct {
	Name string // unique in the app: "entry.general"
	// Pattern writes a number: {n} is the counter, {n:5} padded to five digits,
	// {year} the year of the document's date. Default "{n}".
	Pattern string
	Yearly  bool // the counter starts again at 1 in each year of the document's date
}

var sequencePattern = regexp.MustCompile(`\{(n|n:[1-9]|year)\}`)

// Check tells whether the sequence is well declared.
func (s Sequence) Check() error {
	rest := sequencePattern.ReplaceAllString(s.Pattern, "")
	if s.Name == "" || strings.ContainsAny(rest, "{}") || s.Pattern != "" && !strings.Contains(s.Pattern, "{n") {
		return fmt.Errorf("sequence %q: a name, and a pattern with {n} and no unknown placeholder", s.Name)
	}
	return nil
}

// Format writes the n-th number of year.
func (s Sequence) Format(year, n int) string {
	pattern := s.Pattern
	if pattern == "" {
		pattern = "{n}"
	}
	return sequencePattern.ReplaceAllStringFunc(pattern, func(p string) string {
		switch {
		case p == "{year}":
			return strconv.Itoa(year)
		case p == "{n}":
			return strconv.Itoa(n)
		}
		return fmt.Sprintf("%0*d", int(p[3]-'0'), n)
	})
}

// Next takes the next number of the app's sequence for the accepted decision
// r, dated date (its year counts for a yearly sequence). Call it only in the
// function a decision applies, never in its rules.
func (c Caller) Next(r *pb.ChangeRecord, sequence string, date time.Time) (string, *kernel.Error) {
	if c.rt == nil {
		return "", notFound()
	}
	return c.rt.Next(c, r, sequence, date)
}
