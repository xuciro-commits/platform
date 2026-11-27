package authoring

import (
	"encoding/json"
	"fmt"
	"math/big"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	ruleprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
	"reflect"
	"slices"
	"strings"
	"time"
)

type depletionSlice struct {
	origin string
	qty    *big.Rat
	amt    *big.Rat
}

func checkDepletionRules(b *Build, o ontology.Object, lookup func(string) (platform.RecordTypeInfo, bool), engines ...platform.RuleEngine) error {
	d := o.Depletion
	if d == nil {
		return nil
	}
	if d.When != nil {
		s := ruleSpec(o, ontology.Action{}, o.Reads)
		if err := b.checkRuleCollections(s, "depletion when", lookup); err != nil {
			return err
		}
		if _, err := ruleprotocol.RuleSchema(*d.When, s, lookup, engines...); err != nil {
			return fmt.Errorf("depletion when: %w", err)
		}
	}
	target, ok := lookup(d.Ledger)
	if !ok {
		return nil
	}
	return validateDepletionTarget(o, target)
}

func validateDepletionTarget(o ontology.Object, target platform.RecordTypeInfo) error {
	d := o.Depletion
	if d == nil {
		return nil
	}
	if target.Ledger == nil || target.App != ID || target.Type == o.Type() {
		return fmt.Errorf("depletion target %s must be another ledger in the same authority", d.Ledger)
	}
	origField, origOK := target.Field(d.Origin)
	consField, consOK := target.Field(d.Consumer)
	if !origOK || origField.Type != "reference" || origField.Ref != o.Type() || !origField.Required ||
		!consOK || consField.Type != "reference" || consField.Ref != o.Type() {
		return fmt.Errorf("depletion target %s requires origin and consumer references to %s", d.Ledger, o.Type())
	}
	selfFields := map[string]ontology.Field{}
	for _, f := range o.Fields {
		selfFields[f.Name] = f
	}
	var foundBal *platform.LedgerBalance
	for i := range target.Ledger.Balances {
		if target.Ledger.Balances[i].Name == d.Balance {
			foundBal = &target.Ledger.Balances[i]
			break
		}
	}
	if foundBal == nil || !slices.Contains(foundBal.Keys, d.Origin) || !slices.Contains(foundBal.Measures, d.Quantity) || !slices.Contains(foundBal.Measures, d.Amount) {
		return fmt.Errorf("depletion balance %s.%s must key by %s and measure %s and %s", d.Ledger, d.Balance, d.Origin, d.Quantity, d.Amount)
	}
	for _, k := range foundBal.Keys {
		if k == d.Origin {
			continue
		}
		tf, tok := target.Field(k)
		sf, sok := selfFields[k]
		if !tok || !sok || tf.Type != sf.Type || tf.Ref != sf.Ref {
			return fmt.Errorf("depletion key %s must match on %s and %s", k, o.Type(), d.Ledger)
		}
	}
	tqf, qok := target.Field(d.Quantity)
	taf, aok := target.Field(d.Amount)
	if !qok || !aok || tqf.Type != selfFields[d.Quantity].Type || taf.Type != selfFields[d.Amount].Type {
		return fmt.Errorf("depletion measures %s and %s must match types on %s", d.Quantity, d.Amount, d.Ledger)
	}
	return nil
}

func depletionActive(c platform.Caller, o ontology.Object, lookup func(string) (platform.RecordTypeInfo, bool), record any, now time.Time) (bool, *platform.Error) {
	if o.Depletion == nil {
		return false, nil
	}
	if o.Depletion.When == nil {
		return true, nil
	}
	s := ruleSpec(o, ontology.Action{}, o.Reads)
	schema, err := ruleprotocol.RuleSchema(*o.Depletion.When, s, lookup, c.Rules())
	if err != nil {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	ctx, err := derivedContext(c, *o.Depletion.When, s, lookup, record, now)
	if err != nil {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
	}
	prog, err := c.Rules().CompileExpr(*o.Depletion.When, schema)
	if err != nil {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	res, err := prog.Eval(ctx)
	if err != nil {
		return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return res.Value.Status == "value" && string(res.Value.Data) == "true", nil
}

func decodeRat(raw json.RawMessage) (*big.Rat, string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return new(big.Rat), "", true
	}
	if raw[0] == '{' {
		var m platform.Money
		if json.Unmarshal(raw, &m) != nil {
			return nil, "", false
		}
		return new(big.Rat).SetInt64(m.Amount), m.Currency, true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) != nil {
		return nil, "", false
	}
	r, ok := new(big.Rat).SetString(string(n))
	return r, "", ok
}

func roundRatScale(v *big.Rat, scale int) *big.Rat {
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaled := new(big.Rat).Mul(v, new(big.Rat).SetInt(factor))
	sign := scaled.Sign()
	absScaled := new(big.Rat).Abs(scaled)
	q, rem := new(big.Int).QuoRem(absScaled.Num(), absScaled.Denom(), new(big.Int))
	if new(big.Int).Mul(rem, big.NewInt(2)).Cmp(absScaled.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if sign < 0 {
		q.Neg(q)
	}
	return new(big.Rat).Quo(new(big.Rat).SetInt(q), new(big.Rat).SetInt(factor))
}

func encodeRatValue(kind string, v *big.Rat, currency string) any {
	if kind == "integer" {
		return v.Num().Int64()
	}
	if kind == "money" {
		return platform.Money{Amount: v.Num().Int64(), Currency: currency}
	}
	den := new(big.Int).Set(v.Denom())
	two, five, one := big.NewInt(2), big.NewInt(5), big.NewInt(1)
	var a, b int
	for new(big.Int).Mod(den, two).Sign() == 0 {
		den.Quo(den, two)
		a++
	}
	for new(big.Int).Mod(den, five).Sign() == 0 {
		den.Quo(den, five)
		b++
	}
	digits := max(a, b)
	if den.Cmp(one) != 0 || digits > 12 {
		digits = 6
	}
	return json.Number(v.FloatString(digits))
}

func recordMap(v any) map[string]json.RawMessage {
	raw, _ := json.Marshal(v)
	out := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (b *Build) planDepletion(c platform.Caller, o ontology.Object, values map[string]json.RawMessage, afterMainWrite bool) ([]depletionSlice, *big.Rat, string, bool, *platform.Error) {
	d := o.Depletion
	sliceEntity, ok1 := b.installed[d.Ledger]
	balEntity, ok2 := b.installed[d.Ledger+"."+d.Balance]
	selfEntity, ok3 := b.installed[o.Type()]
	if !ok1 || !ok2 || !ok3 || sliceEntity.Ledger == nil {
		return nil, nil, "", false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Depletion target ledger is unavailable")
	}
	var balSpec platform.LedgerBalance
	for _, sb := range sliceEntity.Ledger.Balances {
		if sb.Name == d.Balance {
			balSpec = sb
			break
		}
	}
	var conds [][]any
	sharedKeys := []string{}
	for _, k := range balSpec.Keys {
		if k == d.Origin {
			continue
		}
		sharedKeys = append(sharedKeys, k)
		var kv any
		_ = json.Unmarshal(values[k], &kv)
		conds = append(conds, []any{k, "=", kv})
	}
	var domain json.RawMessage
	if len(conds) > 0 {
		domain, _ = json.Marshal(conds)
	}
	reader := platform.ActingCaller(c, platform.Member{ID: "app:" + ID, Tenant: c.Tenant}, ID, true)
	rows, _, err := reader.FindOf(reflect.TypeOf(balEntity.Model), platform.Query{Domain: domain})
	if err != nil {
		return nil, nil, "", false, err
	}
	type openRow struct {
		origin   string
		originOn string
		qty      *big.Rat
		amt      *big.Rat
		currency string
	}
	open := []openRow{}
	sumSliceQty := new(big.Rat)
	sumSliceAmt := new(big.Rat)
	currency := ""
	for _, row := range rows {
		rm := recordMap(row)
		q, _, qok := decodeRat(rm[d.Quantity])
		a, cur, aok := decodeRat(rm[d.Amount])
		if !qok || !aok || q.Sign() <= 0 {
			continue
		}
		if cur != "" {
			currency = cur
		}
		var origID string
		_ = json.Unmarshal(rm[d.Origin], &origID)
		origOn := ""
		if origRec, exists := reader.RecordValue(reflect.TypeOf(selfEntity.Model), origID); exists {
			_ = json.Unmarshal(recordMap(origRec)["on"], &origOn)
		}
		sumSliceQty.Add(sumSliceQty, q)
		sumSliceAmt.Add(sumSliceAmt, a)
		open = append(open, openRow{origin: origID, originOn: origOn, qty: q, amt: a, currency: cur})
	}
	slices.SortFunc(open, func(a, b openRow) int {
		if a.originOn != b.originOn {
			return strings.Compare(a.originOn, b.originOn)
		}
		return strings.Compare(a.origin, b.origin)
	})

	qtyVal, _, _ := decodeRat(values[d.Quantity])
	amtVal, curIn, _ := decodeRat(values[d.Amount])
	if currency == "" {
		currency = curIn
	}
	need := new(big.Rat).Neg(qtyVal)
	totalAmt := new(big.Rat)
	amtKind := "decimal"
	for _, f := range o.Fields {
		if f.Name == d.Amount {
			amtKind = f.Type
		}
	}
	scale := 2
	if amtKind == "integer" || amtKind == "money" {
		scale = 0
	}

	for _, mb := range o.Balances {
		if slices.Equal(mb.Keys, sharedKeys) && slices.Contains(mb.Measures, d.Quantity) && slices.Contains(mb.Measures, d.Amount) {
			if mainBal, exists := b.installed[o.Type()+"."+mb.Name]; exists {
				mRows, _, mErr := reader.FindOf(reflect.TypeOf(mainBal.Model), platform.Query{Domain: domain})
				if mErr == nil && len(mRows) == 1 {
					mm := recordMap(mRows[0])
					mq, _, _ := decodeRat(mm[d.Quantity])
					ma, mcur, _ := decodeRat(mm[d.Amount])
					if currency == "" {
						currency = mcur
					}
					if afterMainWrite {
						mq.Sub(mq, qtyVal)
						ma.Sub(ma, amtVal)
					}
					unlayeredQty := new(big.Rat).Sub(mq, sumSliceQty)
					unlayeredAmt := new(big.Rat).Sub(ma, sumSliceAmt)
					if unlayeredQty.Sign() > 0 && need.Sign() > 0 {
						take := new(big.Rat).Set(need)
						if take.Cmp(unlayeredQty) > 0 {
							take.Set(unlayeredQty)
						}
						var partAmt *big.Rat
						if take.Cmp(unlayeredQty) == 0 {
							partAmt = new(big.Rat).Set(unlayeredAmt)
						} else {
							partAmt = roundRatScale(new(big.Rat).Quo(new(big.Rat).Mul(unlayeredAmt, take), unlayeredQty), scale)
						}
						totalAmt.Add(totalAmt, partAmt)
						need.Sub(need, take)
					}
				}
			}
			break
		}
	}

	var planned []depletionSlice
	for _, cand := range open {
		if need.Sign() <= 0 {
			break
		}
		take := new(big.Rat).Set(need)
		if take.Cmp(cand.qty) > 0 {
			take.Set(cand.qty)
		}
		var takeAmt *big.Rat
		if take.Cmp(cand.qty) == 0 {
			takeAmt = new(big.Rat).Set(cand.amt)
		} else {
			takeAmt = roundRatScale(new(big.Rat).Quo(new(big.Rat).Mul(cand.amt, take), cand.qty), scale)
		}
		planned = append(planned, depletionSlice{origin: cand.origin, qty: take, amt: takeAmt})
		totalAmt.Add(totalAmt, takeAmt)
		need.Sub(need, take)
	}
	if need.Sign() > 0 {
		return nil, nil, currency, false, nil
	}
	return planned, new(big.Rat).Neg(totalAmt), currency, true, nil
}

func (b *Build) computeDepletion(c platform.Caller, o ontology.Object, lookup func(string) (platform.RecordTypeInfo, bool), record, previous any, now time.Time) *platform.Error {
	if c.Replaying || c.SourceProjection() || previous != nil || o.Depletion == nil {
		return nil
	}
	active, err := depletionActive(c, o, lookup, record, now)
	if err != nil || !active {
		return err
	}
	vm := recordMap(record)
	qty, _, ok := decodeRat(vm[o.Depletion.Quantity])
	if !ok || qty.Sign() >= 0 {
		return nil
	}
	_, negAmt, currency, sufficient, planErr := b.planDepletion(c, o, vm, false)
	if planErr != nil || !sufficient {
		return planErr
	}
	amtKind := "decimal"
	for _, f := range o.Fields {
		if f.Name == o.Depletion.Amount {
			amtKind = f.Type
		}
	}
	target := reflect.ValueOf(record).Elem().FieldByName(ontology.GoName(o.Depletion.Amount))
	if assignErr := assign(target, encodeRatValue(amtKind, negAmt, currency)); assignErr != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, assignErr.Error())
	}
	return nil
}

func (b *Build) applyDepletionSlices(c platform.Caller, r *pb.ChangeRecord, o ontology.Object, lookup func(string) (platform.RecordTypeInfo, bool), now time.Time) *platform.Error {
	if c.Replaying || c.SourceProjection() || o.Depletion == nil {
		return nil
	}
	d := o.Depletion
	selfEntity, ok := b.installed[o.Type()]
	if !ok {
		return nil
	}
	id := c.TransactionInput(r).GetTarget().GetId()
	after, found := c.RecordValue(reflect.TypeOf(selfEntity.Model), id)
	if !found {
		return nil
	}
	active, err := depletionActive(c, o, lookup, after, now)
	if err != nil || !active {
		return err
	}
	sliceEntity, ok := b.installed[d.Ledger]
	if !ok || sliceEntity.Ledger == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Depletion target ledger is unavailable")
	}
	vm := recordMap(after)
	qty, _, _ := decodeRat(vm[d.Quantity])
	var balSpec platform.LedgerBalance
	for _, sb := range sliceEntity.Ledger.Balances {
		if sb.Name == d.Balance {
			balSpec = sb
			break
		}
	}
	baseFields := func() map[string]any {
		m := map[string]any{}
		var on string
		_ = json.Unmarshal(vm["on"], &on)
		m["on"] = on
		for _, k := range balSpec.Keys {
			if k == d.Origin {
				continue
			}
			var kv any
			_ = json.Unmarshal(vm[k], &kv)
			m[k] = kv
		}
		return m
	}
	qtyKind, amtKind := "decimal", "decimal"
	for _, f := range o.Fields {
		if f.Name == d.Quantity {
			qtyKind = f.Type
		}
		if f.Name == d.Amount {
			amtKind = f.Type
		}
	}
	if qty.Sign() > 0 {
		amt, cur, _ := decodeRat(vm[d.Amount])
		payload := baseFields()
		payload[d.Origin] = id
		payload[d.Consumer] = id
		payload[d.Quantity] = encodeRatValue(qtyKind, qty, "")
		payload[d.Amount] = encodeRatValue(amtKind, amt, cur)
		return b.authority.ApplyTxnRecord(c, r, sliceEntity, "create", id+"-1", platform.Raw(payload), now)
	}
	if qty.Sign() < 0 {
		planned, _, cur, sufficient, planErr := b.planDepletion(c, o, vm, true)
		if planErr != nil {
			return planErr
		}
		if !sufficient {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Insufficient remaining slice balance")
		}
		for i, sl := range planned {
			payload := baseFields()
			payload[d.Origin] = sl.origin
			payload[d.Consumer] = id
			payload[d.Quantity] = encodeRatValue(qtyKind, new(big.Rat).Neg(sl.qty), "")
			payload[d.Amount] = encodeRatValue(amtKind, new(big.Rat).Neg(sl.amt), cur)
			if err := b.authority.ApplyTxnRecord(c, r, sliceEntity, "create", fmt.Sprintf("%s-%d", id, i+1), platform.Raw(payload), now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Build) unreversedSlices(c platform.Caller, d *ontology.LedgerDepletion, field, id string) ([]map[string]json.RawMessage, *platform.Error) {
	sliceEntity, ok := b.installed[d.Ledger]
	if !ok {
		return nil, nil
	}
	reader := platform.ActingCaller(c, platform.Member{ID: "app:" + ID, Tenant: c.Tenant}, ID, true)
	domain, _ := json.Marshal([][]any{{field, "=", id}})
	rows, _, err := reader.FindOf(reflect.TypeOf(sliceEntity.Model), platform.Query{Domain: domain})
	if err != nil {
		return nil, err
	}
	all := make([]map[string]json.RawMessage, 0, len(rows))
	reversed := map[string]bool{}
	for _, row := range rows {
		rm := recordMap(row)
		var rev string
		_ = json.Unmarshal(rm["reverses"], &rev)
		if rev != "" {
			reversed[rev] = true
		}
		all = append(all, rm)
	}
	var active []map[string]json.RawMessage
	for _, rm := range all {
		var rid, rev string
		_ = json.Unmarshal(rm["id"], &rid)
		_ = json.Unmarshal(rm["reverses"], &rev)
		if rev == "" && !reversed[rid] {
			active = append(active, rm)
		}
	}
	slices.SortFunc(active, func(a, z map[string]json.RawMessage) int {
		return strings.Compare(string(a["id"]), string(z["id"]))
	})
	return active, nil
}

func (b *Build) checkDepletionReversal(c platform.Caller, o ontology.Object, existing any, _ string, _ time.Time) *platform.Error {
	if o.Depletion == nil {
		return nil
	}
	var id string
	_ = json.Unmarshal(recordMap(existing)["id"], &id)
	active, err := b.unreversedSlices(c, o.Depletion, o.Depletion.Origin, id)
	if err != nil {
		return err
	}
	var consumers []string
	for _, rm := range active {
		var cons string
		_ = json.Unmarshal(rm[o.Depletion.Consumer], &cons)
		if cons != "" && cons != id {
			consumers = append(consumers, cons)
		}
	}
	if len(consumers) > 0 {
		slices.Sort(consumers)
		consumers = slices.Compact(consumers)
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Cannot reverse {id}: consumed by {consumers}; reverse those postings first / 无法冲正 {id}：已被 {consumers} 消耗，请先冲正对应记录", id, strings.Join(consumers, ", "), id, strings.Join(consumers, "、"))
	}
	return nil
}

func (b *Build) reverseDepletionSlices(c platform.Caller, r *pb.ChangeRecord, o ontology.Object, existing any, revOn string, now time.Time) *platform.Error {
	if o.Depletion == nil {
		return nil
	}
	d := o.Depletion
	sliceEntity, ok := b.installed[d.Ledger]
	if !ok {
		return nil
	}
	var id string
	_ = json.Unmarshal(recordMap(existing)["id"], &id)
	opened, err := b.unreversedSlices(c, d, d.Origin, id)
	if err != nil {
		return err
	}
	consumed, err := b.unreversedSlices(c, d, d.Consumer, id)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var targets []string
	for _, rm := range opened {
		var rid, cons string
		_ = json.Unmarshal(rm["id"], &rid)
		_ = json.Unmarshal(rm[d.Consumer], &cons)
		if (cons == "" || cons == id) && rid != "" && !seen[rid] {
			seen[rid] = true
			targets = append(targets, rid)
		}
	}
	for _, rm := range consumed {
		var rid string
		_ = json.Unmarshal(rm["id"], &rid)
		if rid != "" && !seen[rid] {
			seen[rid] = true
			targets = append(targets, rid)
		}
	}
	slices.Sort(targets)
	for _, tid := range targets {
		if err := b.authority.ApplyTxnRecord(c, r, sliceEntity, "reverse", tid, platform.Raw(map[string]any{"on": revOn}), now); err != nil {
			return err
		}
	}
	return nil
}
