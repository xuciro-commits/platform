package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

type creator interface {
	create(c platform.Caller, cr Create, inputs map[string]any, parentType, parent, id string, now time.Time) (any, *kernel.Error)
	post(c platform.Caller, p Post, inputs map[string]any, record any, now time.Time) (any, *kernel.Error)
	businessRuntime() platform.TransactionExecutor
}

func lifecycle(o Object, roles []string, creates creator, lookup func(string) (platform.EntityInfo, bool)) *platform.Lifecycle {
	if len(o.States) == 0 {
		return nil
	}
	l := &platform.Lifecycle{Field: "state", Initial: o.States[0].Name}
	for _, s := range o.States {
		l.States = append(l.States, platform.State{Name: s.Name, Title: s.Title, Tone: s.Tone, Description: s.Description})
	}
	for _, a := range o.Actions {
		to := a.To
		if to == "" {
			to = a.From[0]
		}
		reach := []string{to}
		if a.To == "" {
			reach = slices.Clone(a.From)
		}
		if a.ToInput != "" {
			for _, in := range a.Inputs {
				if in.Name == a.ToInput {
					reach = ontology.ValueChoices(in.Choices)
				}
			}
		}
		payload := []platform.Field{}
		for _, in := range a.Inputs {
			payload = append(payload, in.Field())
		}
		action := a
		takers := slices.Clone(roles)
		if len(a.Roles) > 0 {
			takers = append([]string{Builder}, a.Roles...)
		}
		var approval *platform.Approval
		if a.Approval != nil {
			approval = &platform.Approval{Pending: a.Approval.Pending, Rejected: a.Approval.Rejected}
			for _, level := range a.Approval.Levels {
				approval.Levels = append(approval.Levels, platform.ApprovalLevel{Title: level.Title, AppRole: level.Role, All: level.All})
			}
		}
		criteria, _ := actionCriteria(o, a, lookup)
		l.Transitions = append(l.Transitions, platform.Transition{ConfirmationOnly: a.ConfirmationOnly, Adoption: a.Adoption, Criteria: criteria, Name: a.Name, Title: a.Title, Description: a.Description,
			From: slices.Clone(a.From), To: reach, ToInput: a.ToInput, Roles: takers, Capability: o.Name, Payload: payload, Approval: approval, DeferRequired: true,
			Do: func(c platform.Caller, record any, raw json.RawMessage, now time.Time) *kernel.Error {
				return take(o, action, c, record, raw, now, creates, lookup)
			}, After: stored(o, action, creates)})
		if a.Transaction == nil {
			compiled, err := compileLegacyTransaction(o, a, lookup)
			if err == nil {
				action.Transaction = compiled
			} else {
				t := &l.Transitions[len(l.Transitions)-1]
				legacy := *t
				t.LegacyReplay = &legacy
				t.After = nil
				t.Do = func(platform.Caller, any, json.RawMessage, time.Time) *kernel.Error {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
				}
			}
		}
		if action.Transaction != nil {
			t := &l.Transitions[len(l.Transitions)-1]
			if t.Description == "" {
				t.Description = "Apply this business transaction."
			}
			if a.Transaction == nil {
				legacy := *t
				t.LegacyReplay = &legacy
			}
			t.Txn, t.After, t.Do = action.Transaction, nil, nil
			t.ApplyTxn = func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error {
				if creates == nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transactions need their original owner")
				}
				return creates.businessRuntime().Transact(c, r, action.Transaction, now)
			}
		}
	}
	return l
}

func stored(o Object, a Action, creates creator) func(platform.Caller, *pb.ChangeRecord, any, time.Time) {
	if len(a.Creates) == 0 && len(a.Posts) == 0 || creates == nil {
		return nil
	}
	return func(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
		if !c.Replaying {
			c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Legacy action storage is reserved for historical inputs"))
			return
		}
		var inputs map[string]any
		_ = json.Unmarshal(r.GetSubmission().GetPayload(), &inputs)
		rec := reflect.ValueOf(record).Elem().Field(0).Interface().(platform.Record)
		for i, cr := range a.Creates {
			if value, err := creates.create(c, cr, inputs, TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err == nil {
				c.Put(r, value)
			}
		}
		for _, p := range a.Posts {
			if value, err := creates.post(c, p, inputs, record, now); err == nil {
				c.Put(r, value)
			}
		}
	}
}

func take(o Object, a Action, c platform.Caller, record any, raw json.RawMessage, now time.Time, creates creator, lookup func(string) (platform.EntityInfo, bool)) *kernel.Error {
	if !c.Replaying {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Legacy action execution is reserved for historical inputs")
	}
	var inputs map[string]any
	if len(raw) > 0 && json.Unmarshal(raw, &inputs) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} takes an object of inputs", a.Title)
	}

	v := reflect.ValueOf(record).Elem()
	lookup = ruleLookup(o, lookup)
	criteria, err := actionCriteria(o, a, lookup)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if refusal := criteria.Check(c, record, inputs, now, lookup); refusal != nil {
		return refusal
	}
	if a.ToInput != "" {
		state, ok := inputs[a.ToInput].(string)
		allowed := false
		for _, in := range a.Inputs {
			if in.Name == a.ToInput && slices.Contains(ontology.ValueChoices(in.Choices), state) {
				allowed = true
			}
		}
		if !ok || !allowed {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The selected state is unavailable")
		}
		v.FieldByName("State").SetString(state)
	}

	types := map[string]string{}
	for _, f := range o.Fields {
		types[f.Name] = f.Type
	}
	for _, set := range a.Sets {
		e, failure := platform.AssignmentExpr(types[set.Field], set, ruleSpec(o, a, a.Reads))
		if failure != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, failure.Error())
		}
		x, refusal := platform.RuleAssignment(e, ruleSpec(o, a, a.Reads), c, record, inputs, now, lookup)
		if refusal != nil {
			return refusal
		}

		if err := assign(v.FieldByName(ontology.GoName(set.Field)), x); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{action} cannot set {field}: {why}", a.Title, set.Field, err.Error())
		}
	}
	rec := v.Field(0).Interface().(platform.Record)
	for i, cr := range a.Creates {
		if creates == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot create records here", a.Title)
		}
		if _, err := creates.create(c, cr, inputs, TypeOf(o.Name), rec.ID, relatedID(rec, a, i), now); err != nil {
			return err
		}
	}
	for _, p := range o.EffectivePosts(a) {
		if creates == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{action} cannot post balances here", a.Title)
		}
		if _, err := creates.post(c, p, inputs, v.Addr().Interface(), now); err != nil {
			return err
		}
	}
	return nil
}

func assign(f reflect.Value, x any) error {
	if !f.IsValid() {
		return fmt.Errorf("no such field")
	}
	raw, err := json.Marshal(x)
	if err != nil {
		return err
	}
	target := reflect.New(f.Type())
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return fmt.Errorf("%v does not fit a %s", x, f.Type())
	}
	f.Set(target.Elem())
	return nil
}

func numberingOf(o Object, model any) func(c platform.Caller, record any, now time.Time) *kernel.Error {
	if o.Numbering == nil {
		return nil
	}
	n, typ := *o.Numbering, reflect.TypeOf(model)
	return func(c platform.Caller, record any, now time.Time) *kernel.Error {
		v := reflect.ValueOf(record).Elem()
		field := v.FieldByName(ontology.GoName(n.Field))
		if !field.IsValid() || field.Kind() != reflect.String || field.String() != "" || v.Field(0).Interface().(platform.Record).Revision > 0 {
			return nil
		}
		year := now.UTC().Year()
		_, count, err := c.FindOf(typ, platform.Query{Limit: 1, Archived: true})
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The number range is not readable here")
		}
		seq := count + 1
		if n.Yearly {
			prefix := n.Prefix + strconv.Itoa(year) + "-"
			all, _, _ := c.FindOf(typ, platform.Query{Limit: 100000, Archived: true})
			seq = 1
			for _, r := range all {
				if strings.HasPrefix(reflect.ValueOf(r).FieldByName(ontology.GoName(n.Field)).String(), prefix) {
					seq++
				}
			}
		}
		field.SetString(n.Format(seq, year))
		return nil
	}
}
