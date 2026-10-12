package build

import (
	"encoding/json"
	"fmt"
	"math"
	buildapi "platformserver/internal/host/buildapi"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// checkCreates refuses a related record an action could not make: an object of
// another app (whose records change only through its own actions), one this
// tenant has not installed, or a Via that is not its reference to this object.
func (b *Build) checkCreates(o Object) error {
	for _, a := range o.Actions {
		for _, cr := range a.Creates {
			where := fmt.Sprintf("the action %q creates %q", a.Name, cr.Object)
			if !strings.HasPrefix(cr.Object, ID+".") {
				return fmt.Errorf("%s, which is another app's object; change it through that app's actions", where)
			}
			if cr.Object == buildapi.TypeOf(o.Name) {
				return fmt.Errorf("%s, which is this object itself", where)
			}
			if b.host == nil {
				continue
			}
			info, known := b.lookupEntity(cr.Object)
			if !known {
				return fmt.Errorf("%s, which this tenant has not published", where)
			}
			via, ok := info.Field(cr.Via)
			if !ok || via.Type != "reference" || via.Ref != buildapi.TypeOf(o.Name) {
				return fmt.Errorf("%s through %q, which is not its reference to %s", where, cr.Via, buildapi.TypeOf(o.Name))
			}
			for _, set := range cr.Sets {
				if set.Field == cr.Via {
					return fmt.Errorf("%s and sets %q, which the action fills with this record", where, set.Field)
				}
				field, ok := info.Field(set.Field)
				if !ok {
					return fmt.Errorf("%s and sets %q, which it has no field for", where, set.Field)
				}
				for _, input := range a.Inputs {
					if input.Name == set.From && input.Type == "reference" && (field.Type != "reference" || field.Ref != input.Ref) {
						return fmt.Errorf("%s reference input %s does not match field %s", where, input.Name, field.Name)
					}
				}
			}
		}
		for _, u := range a.Updates {
			where := fmt.Sprintf("the action %q updates via %q", a.Name, u.Via)
			var viaField Field
			for _, f := range o.Fields {
				if f.Name == u.Via {
					viaField = f
					break
				}
			}
			if viaField.Type != "reference" || !strings.HasPrefix(viaField.Ref, ID+".") || viaField.Ref == buildapi.TypeOf(o.Name) {
				return fmt.Errorf("%s, which is not a reference field to another defined object of this builder", where)
			}
			if b.host == nil {
				continue
			}
			targetInfo, known := b.lookupEntity(viaField.Ref)
			if !known {
				return fmt.Errorf("%s (%s), which this tenant has not published", where, viaField.Ref)
			}
			if u.To != "" {
				if targetInfo.Lifecycle == nil || !slices.ContainsFunc(targetInfo.Lifecycle.States, func(s platform.State) bool { return s.Name == u.To }) {
					return fmt.Errorf("%s and leaves it in %q, which is not a state of %s", where, u.To, viaField.Ref)
				}
			}
			for _, set := range u.Sets {
				if _, ok := targetInfo.Field(set.Field); !ok {
					return fmt.Errorf("%s and sets %q, which %s has no field for", where, set.Field, viaField.Ref)
				}
			}
		}
	}
	return nil
}

// create builds one related record for the action being taken (ADR-0040 21c
// D2). The transition's Do builds and checks it, so a refusal refuses the
// action; its After stores it under the same change, so the accepted result
// holds both writes or neither. No second submission re-enters the ledger.
// Only this builder's own objects are made: another app's records change
// through its actions, protocols or flows.
func (b *Build) create(c platform.Caller, cr Create, inputs map[string]any, parentType, parent, id string, now time.Time) (any, *kernel.Error) {
	if b.host == nil || !strings.HasPrefix(cr.Object, ID+".") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not an object of this builder", cr.Object)
	}
	related, known := b.installed[cr.Object]
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not published", cr.Object)
	}
	info, err := platform.Describe(ID, related, func(reflect.Type) string { return "" })
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if f, ok := info.Field(cr.Via); !ok || f.Type != "reference" || f.Ref != parentType {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} has no reference {field} to {parent}", cr.Object, cr.Via, parentType)
	}
	// The related object's own create roles decide, as its generated create would.
	creators := related.Standard.CreateRoles
	if creators == nil {
		creators = related.Standard.Roles
	}
	if !related.Standard.Create || !c.Holds(c.App, Builder) && !c.May(c.App, "create", creators).Allow {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not create {object}", c.ID, cr.Object)
	}
	v := reflect.New(reflect.TypeOf(related.Model)).Elem()
	v.Field(0).Set(reflect.ValueOf(platform.Record{ID: id}))
	if err := assign(v.FieldByName(goName(cr.Via)), parent); err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	for _, set := range cr.Sets {
		f, _ := info.Field(set.Field)
		var x any
		switch {
		case set.From == "$me":
			x = c.ID
		case set.From == "$now" && f.Type == "date":
			x = c.Today(now)
		case set.From == "$now":
			x = now.UTC()
		case strings.HasPrefix(set.From, "="):
			x, _ = literal(f.Type, strings.TrimPrefix(set.From, "="))
		default:
			given, ok := inputs[set.From]
			if !ok || given == nil {
				continue
			}
			x = given
		}
		if err := assign(v.FieldByName(goName(set.Field)), x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{object} cannot set {field}: {why}", cr.Object, set.Field, err.Error())
		}
	}
	if related.Lifecycle != nil { // a new record starts where its lifecycle does
		v.FieldByName("State").SetString(related.Lifecycle.Initial)
	}
	value := v.Interface()
	if err := c.Check(value); err != nil {
		return nil, err
	}
	return value, nil
}

// relatedID is the n-th related record's ID for one taking of an action: the
// same in the decision and in its After, and new at each revision.
func relatedID(rec platform.Record, a Action, n int) string {
	return fmt.Sprintf("%s-%s-%d-%d", rec.ID, a.Name, rec.Revision, n+1)
}

func (b *Build) createBatch(c platform.Caller, o Object, a Action, cr Create, ruleIdx int, inputs map[string]any, record any, overlay []any, now time.Time) ([]any, *kernel.Error) {
	if b.host == nil || !strings.HasPrefix(cr.Object, ID+".") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not an object of this builder", cr.Object)
	}
	related, known := b.installed[cr.Object]
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not published", cr.Object)
	}
	info, err := platform.Describe(ID, related, func(reflect.Type) string { return "" })
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	parentType := buildapi.TypeOf(o.Name)
	if f, ok := info.Field(cr.Via); !ok || f.Type != "reference" || f.Ref != parentType {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} has no reference {field} to {parent}", cr.Object, cr.Via, parentType)
	}
	creators := related.Standard.CreateRoles
	if creators == nil {
		creators = related.Standard.Roles
	}
	if !related.Standard.Create || !c.Holds(c.App, Builder) && !c.May(c.App, "create", creators).Allow {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not create {object}", c.ID, cr.Object)
	}
	rec := reflect.ValueOf(record).Elem().Field(0).Interface().(platform.Record)
	count := 1
	var foreachItems []map[string]any
	if cr.Foreach != "" {
		items, err := b.resolveCollectionMaps(c, o, a, cr.Foreach, inputs, record, overlay, now)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "%s", err.Error())
		}
		foreachItems = items
		count = len(items)
	} else if cr.Count != "" {
		countVal, _, refusal := b.evalSource(c, o, a, Field{Type: "integer"}, cr.Count, inputs, record, overlay, 1, 1, nil, now)
		if refusal != nil {
			return nil, refusal
		}
		n, numErr := buildapi.ValueNumber(countVal)
		count = int(math.Round(n))
		if numErr != nil || count < 1 || count > 64 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Create count must be between 1 and 64")
		}
	}
	out := make([]any, 0, count)
	for k := 1; k <= count; k++ {
		id := relatedID(rec, a, ruleIdx)
		if a.New && len(a.Creates) == 1 && (cr.Count != "" || cr.Foreach != "") {
			id = fmt.Sprintf("%s-%03d", rec.ID, k)
		} else if count > 1 || cr.Count != "" || cr.Foreach != "" {
			id = fmt.Sprintf("%s-%s-%d-%d-%03d", rec.ID, a.Name, rec.Revision, ruleIdx+1, k)
		}
		var loopItem map[string]any
		if k-1 < len(foreachItems) {
			loopItem = foreachItems[k-1]
		}
		v := reflect.New(reflect.TypeOf(related.Model)).Elem()
		v.Field(0).Set(reflect.ValueOf(platform.Record{ID: id}))
		if err := assign(v.FieldByName(goName(cr.Via)), rec.ID); err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		if related.Lifecycle != nil {
			v.FieldByName("State").SetString(related.Lifecycle.Initial)
		}
		for _, set := range cr.Sets {
			f, _ := info.Field(set.Field)
			targetField := Field{Name: f.Name, Type: f.Type, Ref: f.Ref, Choices: strings.Join(f.Choices, ",")}
			x, present, refusal := b.evalSource(c, o, a, targetField, set.From, inputs, record, overlay, k, count, loopItem, now)
			if refusal != nil {
				return nil, refusal
			}
			if !present {
				continue
			}
			if err := assign(v.FieldByName(goName(set.Field)), x); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{object} cannot set {field}: {why}", cr.Object, set.Field, err.Error())
			}
		}
		if related.Compute != nil {
			related.Compute(v.Addr().Interface())
		}
		value := v.Interface()
		if !a.New {
			if err := c.Check(value); err != nil {
				return nil, err
			}
		} else {
			for _, f := range info.Fields {
				fv := v.FieldByIndex(f.Index)
				if f.Required && fv.IsZero() {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
			}
		}
		out = append(out, value)
	}
	return out, nil
}

func (b *Build) updateRelated(c platform.Caller, o Object, a Action, u UpdateEffect, inputs map[string]any, record any, overlay []any, now time.Time) (any, bool, *kernel.Error) {
	if b.host == nil {
		return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Related updates need the host")
	}
	var viaField Field
	for _, f := range o.Fields {
		if f.Name == u.Via {
			viaField = f
			break
		}
	}
	if viaField.Type != "reference" || !strings.HasPrefix(viaField.Ref, ID+".") {
		return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Update via %s is not a reference field", u.Via)
	}
	rv := reflect.ValueOf(record)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	targetID := buildapi.ValueTextOf(rv.FieldByName(goName(u.Via)).Interface())
	if targetID == "" {
		return nil, false, nil
	}
	targetEntity, known := b.installed[viaField.Ref]
	if !known {
		return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not published", viaField.Ref)
	}
	existing, ok := b.host.Record(viaField.Ref + "/" + targetID)
	if !ok {
		return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Referenced record %s/%s was not found", viaField.Ref, targetID)
	}
	tv := reflect.New(reflect.TypeOf(targetEntity.Model)).Elem()
	if raw, err := json.Marshal(existing); err == nil {
		_ = json.Unmarshal(raw, tv.Addr().Interface())
	}
	info, err := platform.Describe(ID, targetEntity, func(reflect.Type) string { return "" })
	if err != nil {
		return nil, false, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	targetObj := Object{Name: strings.TrimPrefix(viaField.Ref, ID+".")}
	for _, f := range info.Fields {
		if f.Name != "state" {
			targetObj.Fields = append(targetObj.Fields, Field{Name: f.Name, Title: f.Title, Type: f.Type, Ref: f.Ref, Inverse: f.Inverse, Choices: strings.Join(f.Choices, ",")})
		}
	}
	if info.Lifecycle != nil {
		for _, s := range info.Lifecycle.States {
			targetObj.States = append(targetObj.States, State{Name: s.Name, Title: s.Title})
		}
	}
	for _, cond := range u.When {
		held, refusal := evaluateConditionOnRecord(c, b, targetObj, a, cond, inputs, tv.Addr().Interface(), overlay, b.conditionLookup, now)
		if refusal != nil {
			return nil, false, refusal
		}
		if !held {
			return nil, false, nil
		}
	}
	for _, set := range u.Sets {
		f, _ := info.Field(set.Field)
		targetField := Field{Name: f.Name, Type: f.Type, Ref: f.Ref, Choices: strings.Join(f.Choices, ",")}
		x, present, refusal := b.evalSource(c, targetObj, a, targetField, set.From, inputs, tv.Addr().Interface(), overlay, 1, 1, nil, now)
		if refusal != nil {
			return nil, false, refusal
		}
		if !present {
			continue
		}
		if err := assign(tv.FieldByName(goName(set.Field)), x); err != nil {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{object} cannot set {field}: {why}", viaField.Ref, set.Field, err.Error())
		}
	}
	if u.To != "" && targetEntity.Lifecycle != nil {
		tv.FieldByName("State").SetString(u.To)
	}
	if targetEntity.Compute != nil {
		targetEntity.Compute(tv.Addr().Interface())
	}
	updated := tv.Interface()
	if err := c.Check(updated); err != nil {
		return nil, false, err
	}
	return updated, true, nil
}
