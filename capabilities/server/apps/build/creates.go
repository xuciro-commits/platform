package build

import (
	"fmt"
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
			if cr.Object == TypeOf(o.Name) {
				return fmt.Errorf("%s, which is this object itself", where)
			}
			if b.host == nil {
				continue
			}
			info, known := b.host.Entity(cr.Object)
			if !known {
				return fmt.Errorf("%s, which this tenant has not published", where)
			}
			via, ok := info.Field(cr.Via)
			if !ok || via.Type != "reference" || via.Ref != TypeOf(o.Name) {
				return fmt.Errorf("%s through %q, which is not its reference to %s", where, cr.Via, TypeOf(o.Name))
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
	if !related.Standard.Create || c.Role() != Builder && !slices.Contains(creators, c.Role()) {
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
			x = now.UTC().Format(time.DateOnly)
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
