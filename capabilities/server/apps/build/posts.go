package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Postings (ADR-0063, ADR-0057 block E3): an action adds to or subtracts from
// a running balance kept on another object of this builder - a stock balance
// per item and location, a received quantity per order line, a spent amount
// per budget. The balance record's identity is the posting's match fields, so
// two actions that name the same item and location meet the same balance; it
// is created on first posting and changed under the same decision as the
// action (like Creates, ADR-0040 21c). A reversal is a posting of the other
// sign; there is no delete.
type Post struct {
	Object string `json:"object" field:"required" help:"A defined object of this builder that keeps the balance" example:"build.stockbalance"`
	// Match names the balance's identity: its fields and where each value
	// comes from (an input, record.<field> of this record, $me, or =<literal>).
	Match []Set `json:"match" title:"Balance identified by"`
	// Field is the balance's integer, decimal or money field the amount goes to.
	Field string `json:"field" field:"required" help:"The balance's integer, decimal or money field" example:"onhand"`
	// Amount is an input's name, record.<field>, or =<number>.
	Amount string `json:"amount" field:"required" example:"quantity"`
	// Subtract posts the negative amount.
	Subtract bool `json:"subtract,omitempty" title:"Subtract"`
	// Floor, when set, refuses a posting that would take the balance below zero.
	Floor bool `json:"floor,omitempty" title:"Never below zero"`
}

// BalanceID is the balance record for one set of match values: stable, so the
// same identity always meets the same record.
func BalanceID(object string, values []string) string {
	sum := sha256.Sum256([]byte(object + "\x00" + strings.Join(values, "\x00")))
	return "bal-" + hex.EncodeToString(sum[:12])
}

func (b *Build) checkPosts(o Object) error {
	for _, a := range o.Actions {
		for _, p := range a.Posts {
			where := fmt.Sprintf("the action %q posts to %q", a.Name, p.Object)
			if !strings.HasPrefix(p.Object, ID+".") {
				return fmt.Errorf("%s, which is another app's object; change it through that app's actions", where)
			}
			if p.Object == TypeOf(o.Name) {
				return fmt.Errorf("%s, which is this object itself", where)
			}
			if len(p.Match) == 0 {
				return fmt.Errorf("%s without a field that identifies the balance", where)
			}
			for _, m := range p.Match {
				if err := checkSource(o, a, m.From); err != nil {
					return fmt.Errorf("%s, match %q: %w", where, m.Field, err)
				}
			}
			if err := checkSource(o, a, p.Amount); err != nil {
				return fmt.Errorf("%s, amount: %w", where, err)
			}
			if b.host == nil {
				continue
			}
			info, known := b.lookupEntity(p.Object)
			if !known {
				return fmt.Errorf("%s, which this tenant has not published", where)
			}
			for _, m := range p.Match {
				if _, ok := info.Field(m.Field); !ok {
					return fmt.Errorf("%s and matches %q, which it has no field for", where, m.Field)
				}
			}
			f, ok := info.Field(p.Field)
			if !ok || f.Type != "integer" && f.Type != "decimal" && f.Type != "money" {
				return fmt.Errorf("%s into %q, which is not an integer, decimal or money field of it", where, p.Field)
			}
		}
	}
	return nil
}

// checkSource is where a posting's value comes from: an input of the action,
// a field of this record, the member, or a literal.
func checkSource(o Object, a Action, from string) error {
	switch {
	case from == "$me", strings.HasPrefix(from, "="):
		return nil
	case strings.HasPrefix(from, "record."):
		name := strings.TrimPrefix(from, "record.")
		for _, f := range o.Fields {
			if f.Name == name {
				return nil
			}
		}
		return fmt.Errorf("%q is not a field of this object", name)
	}
	for _, in := range a.Inputs {
		if in.Name == from {
			return nil
		}
	}
	return fmt.Errorf("%q is not an input of the action", from)
}

// post builds the balance record after this posting; take checks it, After
// stores it (the same shape as create).
func (b *Build) post(c platform.Caller, p Post, inputs map[string]any, record any, now time.Time) (any, *kernel.Error) {
	if b.host == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Postings need the host")
	}
	balance, known := b.installed[p.Object]
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not published", p.Object)
	}
	info, err := platform.Describe(ID, balance, func(reflect.Type) string { return "" })
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	own := map[string]any{}
	if raw, marshalErr := json.Marshal(record); marshalErr == nil {
		json.Unmarshal(raw, &own)
	}
	source := func(from string) any {
		switch {
		case from == "$me":
			return c.ID
		case strings.HasPrefix(from, "="):
			return strings.TrimPrefix(from, "=")
		case strings.HasPrefix(from, "record."):
			return own[strings.TrimPrefix(from, "record.")]
		}
		return inputs[from]
	}
	keys := make([]string, 0, len(p.Match))
	for _, m := range p.Match {
		keys = append(keys, textOf(source(m.From)))
	}
	id := BalanceID(p.Object, keys)
	v := reflect.New(reflect.TypeOf(balance.Model)).Elem()
	if existing, ok := b.host.Record(p.Object + "/" + id); ok {
		if raw, marshalErr := json.Marshal(existing); marshalErr == nil {
			json.Unmarshal(raw, v.Addr().Interface())
		}
	} else {
		v.Field(0).Set(reflect.ValueOf(platform.Record{ID: id}))
	}
	for _, m := range p.Match {
		f, _ := info.Field(m.Field)
		x := source(m.From)
		if s, isText := x.(string); isText && strings.HasPrefix(m.From, "=") {
			x, _ = literal(f.Type, s)
		}
		if err := assign(v.FieldByName(goName(m.Field)), x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The posting cannot match {field}: {why}", m.Field, err.Error())
		}
	}
	amount, amountErr := number(source(p.Amount))
	if amountErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The posting's amount {amount} is not a number", p.Amount)
	}
	if p.Subtract {
		amount = -amount
	}
	f, _ := info.Field(p.Field)
	target := v.FieldByName(goName(p.Field))
	if !target.IsValid() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The balance has no field {field}", p.Field)
	}
	var next float64
	switch f.Type {
	case "integer":
		next = float64(target.Int()) + amount
		if next != math.Trunc(next) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} counts whole units", p.Field)
		}
		target.SetInt(int64(next))
	case "decimal":
		next = target.Float() + amount
		target.SetFloat(next)
	case "money":
		m := target.Interface().(platform.Money)
		m.Amount += int64(math.Round(amount))
		if m.Currency == "" {
			if given, ok := source(p.Amount).(map[string]any); ok {
				m.Currency, _ = given["currency"].(string)
			}
		}
		next = float64(m.Amount)
		target.Set(reflect.ValueOf(m))
	default:
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is not a number field", p.Field)
	}
	if p.Floor && next < 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{field} would fall below zero", p.Field)
	}
	value := v.Interface()
	if err := c.Check(value); err != nil {
		return nil, err
	}
	return value, nil
}

// number reads an input or field value as the amount it posts: a number, a
// numeric string, or money (its minor units).
func number(x any) (float64, error) {
	switch n := x.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	case map[string]any:
		if amount, ok := n["amount"]; ok {
			return number(amount)
		}
	case platform.Money:
		return float64(n.Amount), nil
	}
	return 0, fmt.Errorf("not a number")
}
