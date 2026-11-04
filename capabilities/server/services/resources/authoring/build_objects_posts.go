package build

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func (b *Build) checkPosts(o Object) error {
	var lookup func(string) (platform.EntityInfo, bool)
	if b.host != nil {
		lookup = b.lookupEntity
	}
	return ontology.CheckActionPostings(o, ID, lookup)
}

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
		keys = append(keys, platform.TextOf(source(m.From)))
	}
	id := platform.BalanceID(p.Object, keys)
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
			x, _ = platform.Literal(f.Type, s)
		}
		if err := assign(v.FieldByName(ontology.GoName(m.Field)), x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The posting cannot match {field}: {why}", m.Field, err.Error())
		}
	}
	amount, amountErr := ValueNumber(source(p.Amount))
	if amountErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The posting's amount {amount} is not a number", p.Amount)
	}
	if p.Subtract {
		amount = -amount
	}
	f, _ := info.Field(p.Field)
	target := v.FieldByName(ontology.GoName(p.Field))
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

func validateOf(o Object, model any, lookup func(string) (platform.EntityInfo, bool)) func(c platform.Caller, record any) *kernel.Error {
	if o.Extends == "" {
		return nil
	}
	typ := reflect.TypeOf(model)
	return func(c platform.Caller, record any) *kernel.Error {
		if !c.Replaying && lookup != nil {
			base, exists := lookup(o.Extends)
			if !exists {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Extension base {type} is unavailable", o.Extends)
			}
			shape := platform.EntityInfo{Type: TypeOf(o.Name), Extends: o.Extends, ExtensionSource: o.ExtensionSource}
			for _, field := range o.Fields {
				shape.Fields = append(shape.Fields, platform.FieldInfo{Name: field.Name, Type: field.Type, Ref: field.Ref, Required: field.Required, ReadOnly: field.ReadOnly, AliasOf: field.AliasOf})
			}
			if err := platform.CheckExtension(shape, base); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		v := reflect.ValueOf(record).Elem()
		base := v.FieldByName(ontology.GoName(BaseField))
		if !base.IsValid() || base.String() == "" {
			return nil
		}
		id := v.Field(0).Interface().(platform.Record).ID
		domain, _ := json.Marshal([][]any{{BaseField, "=", base.String()}})
		found, _, err := c.FindOf(typ, platform.Query{Domain: domain})
		if err != nil {
			return nil
		}
		for _, f := range found {
			if other := reflect.ValueOf(f).Field(0).Interface().(platform.Record); other.ID != id && !other.Archived {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{base} already has its {object} record", base.String(), o.Title)
			}
		}
		return nil
	}
}
