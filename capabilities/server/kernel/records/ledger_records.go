package records

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"platformserver/platform"
)

func compileLedgerPlan(info EntityInfo, engine platform.RuleEngine) (ledgerPlan, error) {
	if info.Ledger == nil {
		return ledgerPlan{}, nil
	}
	plan := ledgerPlan{closedBefore: info.Ledger.ClosedBefore}
	for _, b := range info.Ledger.Balances {
		next := ledgerBalancePlan{name: b.Name, keys: b.Keys}
		schema := platform.ExprSchema{Refs: map[string]platform.ExprType{}}
		for _, name := range b.Keys {
			f, ok := info.Field(name)
			if !ok {
				return plan, fmt.Errorf("ledger key %s is not declared", name)
			}
			schema.Refs[name] = platform.ExprType{Kind: platform.RuleFieldKind(f.Type), Object: f.Ref}
		}
		for _, name := range b.Measures {
			f, ok := info.Field(name)
			if !ok {
				return plan, fmt.Errorf("ledger measure %s is not declared", name)
			}
			next.measures = append(next.measures, ledgerMeasurePlan{name: name, kind: f.Type})
			schema.Refs[name] = platform.ExprType{Kind: platform.RuleFieldKind(f.Type)}
		}
		for _, rule := range b.Constraints {
			if rule.Expr == nil {
				return plan, fmt.Errorf("ledger constraint %s has no expression", rule.Name)
			}
			program, err := engine.CompileExpr(*rule.Expr, schema)
			if err != nil {
				return plan, err
			}
			next.constraints = append(next.constraints, ledgerCheck{schema: schema, program: program, message: rule.Message})
		}
		plan.balances = append(plan.balances, next)
	}
	return plan, nil
}

func ledgerRecord(v reflect.Value) (ledgerFact, error) {
	raw, err := json.Marshal(v.Interface())
	if err != nil {
		return ledgerFact{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ledgerFact{}, err
	}
	var f ledgerFact
	f.id = Header(v).ID
	if f.id == "" {
		f.id = "_probe"
	}
	f.fields = fields
	_ = json.Unmarshal(fields["on"], &f.on)
	_ = json.Unmarshal(fields["reverses"], &f.reverses)
	return f, nil
}

func (et *Type) ensureLedger() error {
	if et.info.Ledger == nil || et.ledgerReady {
		return nil
	}
	plan, err := compileLedgerPlan(et.info, et.store.rules)
	if err != nil {
		return err
	}
	// Rebuilding from unordered rows must not impose contemporary close dates
	// or intermediate balance constraints on already accepted historical facts.
	rebuild := plan.replayPlan()
	var image ledgerProjection
	for _, row := range et.rows.All() {
		f, err := ledgerRecord(row.Value)
		if err != nil {
			return err
		}
		image, err = image.apply(rebuild, f)
		if err != nil {
			return err
		}
	}
	et.ledgerPlan, et.ledgerImage, et.ledgerReady = plan, image, true
	return nil
}

func (et *Type) ledgerTrial(v reflect.Value, prior *Row) (ledgerProjection, error) {
	if et.info.BalanceOf != "" {
		return ledgerProjection{}, fmt.Errorf("ledger balance is a read-only projection")
	}
	if et.info.Ledger == nil {
		return ledgerProjection{}, nil
	}
	if prior != nil || Header(v).Archived {
		return ledgerProjection{}, fmt.Errorf("ledger facts are immutable")
	}
	if err := et.ensureLedger(); err != nil {
		return ledgerProjection{}, err
	}
	f, err := ledgerRecord(v)
	if err != nil {
		return ledgerProjection{}, err
	}
	if f.reverses != "" {
		original := et.rows.Get(f.reverses)
		if original == nil {
			return ledgerProjection{}, fmt.Errorf("ledger reversal needs an original fact")
		}
		prior, err := ledgerRecord(original.Value)
		if err != nil {
			return ledgerProjection{}, err
		}
		if err := checkLedgerReversal(et.ledgerPlan, prior, f); err != nil {
			return ledgerProjection{}, err
		}
	}
	return et.ledgerImage.apply(et.ledgerPlan, f)
}

// CheckLedgerRow evaluates a trial projection before ordinary acceptance.
// Replay consumes accepted facts without re-evaluating business constraints.
func (et *Type) CheckLedgerRow(v reflect.Value, prior *Row) error {
	_, err := et.ledgerTrial(v, prior)
	return err
}

func (et *Type) updateLedgerBalanceRows(f ledgerFact) error {
	if et.info.Ledger == nil {
		return nil
	}
	for _, b := range et.ledgerPlan.balances {
		keys := make([]json.RawMessage, 0, len(b.keys))
		for _, key := range b.keys {
			keys = append(keys, f.fields[key])
		}
		typ := et.info.Type + "." + b.name
		target := et.store.types[typ]
		if target == nil {
			continue
		}
		if err := target.writeLedgerBalance(b, keys, et.ledgerImage.balance(b.name, keys)); err != nil {
			return err
		}
	}
	return nil
}

func (et *Type) writeLedgerBalance(b ledgerBalancePlan, keys []json.RawMessage, totals ledgerTotals) error {
	if et.info.BalanceOf == "" {
		return fmt.Errorf("ledger projection target is not a balance")
	}
	v := reflect.New(et.info.Go).Elem()
	key := ledgerKey(b.name, keys)
	digest := sha256.Sum256([]byte(key))
	id := hex.EncodeToString(digest[:])
	Header(v).ID = id
	for i, name := range b.keys {
		field, _ := et.info.Field(name)
		if err := json.Unmarshal(keys[i], v.FieldByIndex(field.Index).Addr().Interface()); err != nil {
			return err
		}
	}
	for name, raw := range totals {
		field, ok := et.info.Field(name)
		if !ok {
			return fmt.Errorf("unknown balance field %s", name)
		}
		if err := json.Unmarshal(raw, v.FieldByIndex(field.Index).Addr().Interface()); err != nil {
			return err
		}
	}
	row := &Row{Value: v}
	// Deliberately do not use TrackWrite/Put: balances never enter the accepted
	// row set or authority journal. This is the only generated row writer.
	et.indexes.ReplaceReferences(et.info, et.rows, id, row)
	et.rows.Set(id, row)
	et.store.changed(et.info.Type)
	et.store.LiveTouched(et.info.Type)
	return nil
}

func (s *Store) rebuildLedgerBalanceLocked(target *Type) error {
	if target.info.BalanceOf == "" {
		return nil
	}
	source := s.types[target.info.BalanceOf]
	if source == nil {
		return fmt.Errorf("balance has no ledger")
	}
	if err := source.ensureLedger(); err != nil {
		return err
	}
	name := strings.TrimPrefix(target.info.Type, source.info.Type+".")
	target.rows = Rows{}
	target.indexes.Reset()
	for _, b := range source.ledgerPlan.balances {
		if b.name != name {
			continue
		}
		for key, totals := range source.ledgerImage.current.All() {
			prefix := b.name + "/"
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			var keys []json.RawMessage
			if err := json.Unmarshal([]byte(strings.TrimPrefix(key, prefix)), &keys); err != nil {
				return err
			}
			if err := target.writeLedgerBalance(b, keys, totals); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unknown balance %s", target.info.Type)
}

func (s *Store) RebuildLedgerBalances() error {
	s.Lock()
	defer s.Unlock()
	for _, et := range s.types {
		if et.info.Ledger != nil {
			et.ledgerReady = false
		}
	}
	for _, et := range s.types {
		if et.info.BalanceOf != "" {
			if err := s.rebuildLedgerBalanceLocked(et); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReverseLedgerValue constructs a new immutable fact; the original is not edited.
func ReverseLedgerValue(e platform.RecordType, original any, id, day string) (any, error) {
	if e.Ledger == nil {
		return nil, fmt.Errorf("not a ledger")
	}
	v := Copy(reflect.TypeOf(original), original)
	before, err := ledgerRecord(v)
	if err != nil {
		return nil, err
	}
	if before.reverses != "" {
		return nil, fmt.Errorf("a reversal cannot itself be reversed")
	}
	*Header(v) = platform.Record{ID: id}
	v.FieldByName("On").SetString(day)
	v.FieldByName("Reverses").SetString(before.id)
	info, err := Describe("", e, func(reflect.Type) string { return "" })
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, b := range e.Ledger.Balances {
		for _, name := range b.Measures {
			if seen[name] {
				continue
			}
			seen[name] = true
			field, _ := info.Field(name)
			raw, err := negateLedgerAmount(field.Type, before.fields[name])
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, v.FieldByIndex(field.Index).Addr().Interface()); err != nil {
				return nil, err
			}
		}
	}
	return v.Interface(), nil
}

func (s *Store) LedgerReversed(typ, id string) (bool, error) {
	s.Lock()
	defer s.Unlock()
	et := s.types[typ]
	if et == nil || et.info.Ledger == nil {
		return false, fmt.Errorf("ledger unavailable")
	}
	if err := et.ensureLedger(); err != nil {
		return false, err
	}
	return et.ledgerImage.reversed.Has(id), nil
}

func (et *Type) CheckLedgerReceipt(v reflect.Value, schema string) error {
	if et.info.Ledger == nil {
		return nil
	}
	f, err := ledgerRecord(v)
	if err != nil {
		return err
	}
	if (f.reverses != "") != (schema == et.info.Type+".reverse") {
		return fmt.Errorf("only the system reverse action can post a reversal")
	}
	return nil
}
