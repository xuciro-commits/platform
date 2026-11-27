package records

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// AggregateLedgerPeriod uses the existing aggregation machinery for each
// measure projection. It does not scan facts or bypass the caller's row view.
func (s *Store) AggregateLedgerPeriod(et *Type, q AggregateQuery, rows []reflect.Value, from, to string) (Aggregate, *platform.Error) {
	invalid := func(err error) (Aggregate, *platform.Error) {
		return Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if len(q.Groups) > 2 || q.Window != nil || q.Histogram != nil {
		return invalid(fmt.Errorf("ledger period needs at most two group keys and no histogram/window"))
	}
	if et.info.BalanceOf == "" {
		return invalid(fmt.Errorf("ledger period requires a balance object"))
	}
	source := s.types[et.info.BalanceOf]
	if source == nil {
		return invalid(fmt.Errorf("ledger unavailable"))
	}
	if err := source.ensureLedger(); err != nil {
		return invalid(err)
	}
	name := strings.TrimPrefix(et.info.Type, et.info.BalanceOf+".")
	var plan ledgerBalancePlan
	for _, b := range source.ledgerPlan.balances {
		if b.name == name {
			plan = b
			break
		}
	}
	if plan.name == "" {
		return invalid(fmt.Errorf("unknown ledger balance"))
	}
	if len(q.Measures) == 0 {
		for _, m := range plan.measures {
			q.Measures = append(q.Measures, "sum:"+m.name)
		}
	}
	for _, g := range q.Groups {
		found := false
		for _, key := range plan.keys {
			if key == g {
				found = true
			}
		}
		if !found {
			return invalid(fmt.Errorf("ledger period group %s must be a balance key", g))
		}
	}
	for _, m := range q.Measures {
		op, field, _ := strings.Cut(m, ":")
		if op != "sum" {
			return invalid(fmt.Errorf("ledger period measures must be sums"))
		}
		found := false
		for _, measure := range plan.measures {
			if measure.name == field {
				found = true
			}
		}
		if !found {
			return invalid(fmt.Errorf("unknown ledger measure %s", field))
		}
	}
	var projected [4][]reflect.Value
	for _, v := range rows {
		period, err := s.LedgerPeriodFor(et.info, v, from, to)
		if err != nil {
			return invalid(err)
		}
		for i, totals := range []ledgerTotals{period.Opening, period.Debit, period.Credit, period.Closing} {
			copy := Copy(et.info.Go, v.Interface())
			for _, m := range plan.measures {
				field, ok := et.info.Field(m.name)
				if !ok {
					return invalid(fmt.Errorf("balance measure is withheld"))
				}
				if err := json.Unmarshal(totals[m.name], copy.FieldByIndex(field.Index).Addr().Interface()); err != nil {
					return invalid(err)
				}
			}
			projected[i] = append(projected[i], copy)
		}
	}
	out := Aggregate{Rows: []map[string]any{}}
	for i, prefix := range []string{"opening", "debit", "credit", "closing"} {
		a, refusal := s.Aggregate(et, q, projected[i], true)
		if refusal != nil {
			return Aggregate{}, refusal
		}
		if i == 0 {
			for _, c := range a.Columns {
				if c.Kind == "group" {
					out.Columns = append(out.Columns, c)
				}
			}
			for _, row := range a.Rows {
				dest := map[string]any{}
				for _, c := range a.Columns {
					if c.Kind == "group" {
						dest[c.Name] = row[c.Name]
					}
				}
				out.Rows = append(out.Rows, dest)
			}
		}
		if len(a.Rows) != len(out.Rows) {
			return invalid(fmt.Errorf("ledger period groups differ"))
		}
		for _, c := range a.Columns {
			if c.Kind != "measure" {
				continue
			}
			old := c.Name
			c.Name = prefix + ":" + c.Field
			c.Title = prefix + " · " + c.Title
			out.Columns = append(out.Columns, c)
			for j, row := range a.Rows {
				out.Rows[j][c.Name] = row[old]
			}
		}
	}
	return out, nil
}
