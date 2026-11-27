package records

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
)

// ReconcileLedger is deliberately independent of ledgerProjection.apply.
// It sums stored facts with a plain map and compares with both the published
// balance rows and the private index, without changing either.
func (s *Store) ReconcileLedger(typ string) ([]string, error) {
	s.Lock()
	defer s.Unlock()
	source := s.types[typ]
	if source == nil || source.info.Ledger == nil {
		return nil, fmt.Errorf("ledger unavailable")
	}
	if err := source.ensureLedger(); err != nil {
		return nil, err
	}
	var mismatches []string
	for _, b := range source.ledgerPlan.balances {
		type sum struct {
			n        *big.Rat
			currency string
		}
		expected := map[string]map[string]sum{}
		for _, row := range source.rows.All() {
			f, err := ledgerRecord(row.Value)
			if err != nil {
				return nil, err
			}
			keys := make([]json.RawMessage, 0, len(b.keys))
			for _, key := range b.keys {
				keys = append(keys, f.fields[key])
			}
			key := ledgerKey(b.name, keys)
			if expected[key] == nil {
				expected[key] = map[string]sum{}
			}
			for _, m := range b.measures {
				amount, currency, err := ledgerNumber(f.fields[m.name])
				if err != nil {
					return nil, err
				}
				previous := expected[key][m.name]
				if previous.n == nil {
					previous = sum{new(big.Rat), currency}
				}
				if previous.currency != currency {
					return nil, fmt.Errorf("reconciliation currency mismatch")
				}
				previous.n.Add(previous.n, amount)
				expected[key][m.name] = previous
			}
		}
		target := s.types[typ+"."+b.name]
		if target == nil {
			return nil, fmt.Errorf("balance unavailable")
		}
		ids := map[string]bool{}
		for key, values := range expected {
			h := sha256.Sum256([]byte(key))
			id := hex.EncodeToString(h[:])
			ids[id] = true
			row := target.rows.Get(id)
			got := source.ledgerImage.current.Get(key)
			mismatch := row == nil
			for name, want := range values {
				n, c, e := ledgerNumber(got[name])
				if e != nil || n.Cmp(want.n) != 0 || c != want.currency {
					mismatch = true
				}
				if row != nil {
					field, ok := target.info.Field(name)
					if !ok {
						return nil, fmt.Errorf("balance field missing")
					}
					v := row.Value.FieldByIndex(field.Index)
					if v.Kind() == reflect.Pointer && v.IsNil() {
						mismatch = true
						continue
					}
					raw := ledgerJSON(v.Interface())
					n, c, e = ledgerNumber(raw)
					if e != nil || n.Cmp(want.n) != 0 || c != want.currency {
						mismatch = true
					}
				}
			}
			if mismatch {
				mismatches = append(mismatches, key)
			}
		}
		for id := range target.rows.All() {
			if !ids[id] {
				mismatches = append(mismatches, b.name+"/unexpected/"+id)
			}
		}
		for key := range source.ledgerImage.current.All() {
			if strings.HasPrefix(key, b.name+"/") {
				if _, ok := expected[key]; !ok {
					mismatches = append(mismatches, key)
				}
			}
		}
	}
	slices.Sort(mismatches)
	return slices.Compact(mismatches), nil
}
