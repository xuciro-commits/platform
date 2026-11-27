package records

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"platformserver/platform"
)

// LedgerPeriod is a read-only projection of signed facts. Credit is the
// absolute magnitude of negative facts, so closing = opening + debit - credit.
type LedgerPeriod struct {
	Opening ledgerTotals
	Debit   ledgerTotals
	Credit  ledgerTotals
	Closing ledgerTotals
}

func (p ledgerProjection) period(b ledgerBalancePlan, keys []json.RawMessage, from, to string) (LedgerPeriod, error) {
	out := LedgerPeriod{Opening: ledgerTotals{}, Debit: ledgerTotals{}, Credit: ledgerTotals{}, Closing: ledgerTotals{}}
	for _, d := range []string{from, to} {
		if d != "" {
			if _, err := time.Parse(time.DateOnly, d); err != nil {
				return out, fmt.Errorf("invalid ledger period date %s", d)
			}
		}
	}
	if from != "" && to != "" && from > to {
		return out, fmt.Errorf("ledger period start follows end")
	}
	key := ledgerKey(b.name, keys)
	prefix := key + "/"
	current := p.current.Get(key)
	zero := func(m ledgerMeasurePlan) json.RawMessage {
		if m.kind == "money" {
			_, currency, _ := ledgerNumber(current[m.name])
			return ledgerJSON(platform.Money{Currency: currency})
		}
		return json.RawMessage("0")
	}
	for _, m := range b.measures {
		out.Opening[m.name] = zero(m)
		out.Debit[m.name] = zero(m)
		out.Credit[m.name] = zero(m)
		out.Closing[m.name] = zero(m)
	}
	add := func(dest ledgerTotals, values ledgerTotals, negative bool) error {
		for _, m := range b.measures {
			raw := values[m.name]
			if len(raw) == 0 {
				continue
			}
			var err error
			if negative {
				raw, err = negateLedgerAmount(m.kind, raw)
				if err != nil {
					return err
				}
			}
			dest[m.name], err = addLedgerAmount(m.kind, dest[m.name], raw)
			if err != nil {
				return err
			}
		}
		return nil
	}
	apply := func(day string, debit, credit ledgerTotals) error {
		if to != "" && day > to {
			return nil
		}
		if from != "" && day < from {
			if err := add(out.Opening, debit, false); err != nil {
				return err
			}
			return add(out.Opening, credit, true)
		}
		if err := add(out.Debit, debit, false); err != nil {
			return err
		}
		return add(out.Credit, credit, false)
	}
	// The month index carries all complete-month totals. Only boundary months
	// need the daily index; no accepted facts are scanned on the read path.
	for k, debit := range p.debitMonths.After(prefix) {
		if !strings.HasPrefix(k, prefix) {
			break
		}
		month := strings.TrimPrefix(k, prefix)
		if len(month) != 7 {
			continue
		}
		if to != "" && month > to[:7] {
			break
		}
		boundary := from != "" && month == from[:7] || to != "" && month == to[:7]
		if !boundary {
			if err := apply(month+"-01", debit, p.creditMonths.Get(k)); err != nil {
				return out, err
			}
			continue
		}
		start := prefix + month + "-"
		for d, values := range p.days.After(start) {
			if !strings.HasPrefix(d, start) {
				break
			}
			if err := apply(strings.TrimPrefix(d, prefix), values, p.credits.Get(d)); err != nil {
				return out, err
			}
		}
	}
	out.Closing = cloneLedgerTotals(out.Opening)
	if err := add(out.Closing, out.Debit, false); err != nil {
		return out, err
	}
	if err := add(out.Closing, out.Credit, true); err != nil {
		return out, err
	}
	return out, nil
}

// LedgerPeriodFor reads the exact key of an authorized balance row. The
// caller holds the store lock and must select rows through normal visibility.
func (s *Store) LedgerPeriodFor(info EntityInfo, value reflect.Value, from, to string) (LedgerPeriod, error) {
	source := s.types[info.BalanceOf]
	if source == nil {
		return LedgerPeriod{}, fmt.Errorf("ledger unavailable")
	}
	if err := source.ensureLedger(); err != nil {
		return LedgerPeriod{}, err
	}
	name := strings.TrimPrefix(info.Type, info.BalanceOf+".")
	for _, b := range source.ledgerPlan.balances {
		if b.name != name {
			continue
		}
		keys := make([]json.RawMessage, 0, len(b.keys))
		for _, key := range b.keys {
			f, ok := info.Field(key)
			if !ok {
				return LedgerPeriod{}, fmt.Errorf("balance key is withheld")
			}
			keys = append(keys, ledgerJSON(value.FieldByIndex(f.Index).Interface()))
		}
		for _, m := range b.measures {
			if _, ok := info.Field(m.name); !ok {
				return LedgerPeriod{}, fmt.Errorf("balance measure is withheld")
			}
		}
		return source.ledgerImage.period(b, keys, from, to)
	}
	return LedgerPeriod{}, fmt.Errorf("unknown balance declaration")
}
