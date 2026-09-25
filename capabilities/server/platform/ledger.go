package platform

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Ledger is one business package's decisions in one tenant: the kernel's change
// log and authority for the package's data classes, received in the kernel's
// order with the package's action catalog as the role check (ADR-0008). The
// package supplies attribute conditions and its rules; it never wires the kernel.
type Ledger struct {
	mu           sync.Mutex
	tenant       string
	declarations []*pb.AuthorityDeclaration
	Changes      *kernel.ChangeLog
	authorities  *kernel.Authorities
	Catalog      *Catalog
}

// NewLedger declares authority as the tenant server for classes and accepts the
// catalog's actions (version 1 of each schema).
func NewLedger(tenant, authority string, catalog *Catalog, classes ...string) *Ledger {
	var schemas []*pb.SchemaRef
	for _, a := range catalog.actions {
		schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: 1})
	}
	l := &Ledger{tenant: tenant, Changes: kernel.NewChangeLog(kernel.NewSchemaRegistry(schemas, nil)),
		authorities: kernel.NewAuthorities(authority), Catalog: catalog}
	for _, class := range classes {
		d := &pb.AuthorityDeclaration{TenantId: tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: authority, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return l
}

// Declarations are the package's authority declarations, for edges (K5 A9).
func (l *Ledger) Declarations() []*pb.AuthorityDeclaration { return l.declarations }

// Receive accepts s from c or refuses it: an action outside the enabled catalog
// is UNKNOWN_SCHEMA; c's role must be granted and allowed (attribute conditions,
// may be nil) must hold, except in a replay (ADR-0008); rules (may be nil)
// returns how to apply the decision, which runs with the new record only when
// it is accepted (an idempotent replay applies nothing); a new decision is then
// published to the tenant's subscribers (ADR-0010).
func (l *Ledger) Receive(c Caller, s *pb.Submission, now time.Time,
	allowed func() bool, rules func() (func(*pb.ChangeRecord), *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !c.Replaying && !l.Catalog.Enabled(s.GetSchema().GetName()) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	if c.rt != nil && c.rt.Probing() { // a request for approval: policy and rules, nothing recorded or applied (ADR-0017 D3)
		if !c.Automation && !(l.Catalog.Permits(c.Role(), s.GetSchema().GetName()) && (allowed == nil || allowed())) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		if rules != nil {
			if _, err := rules(); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	receiver := kernel.Receiver{Changes: l.Changes, Authorities: l.authorities,
		Policy: func(kernel.Caller, *pb.Submission) bool {
			return c.Replaying || c.Automation || l.Catalog.Permits(c.Role(), s.GetSchema().GetName()) && (allowed == nil || allowed())
		}}
	var apply func(*pb.ChangeRecord)
	record, err := receiver.Receive(kernel.Caller{Tenant: l.tenant, Principal: c.ID}, s, now, func() *kernel.Error {
		if rules == nil {
			return nil
		}
		var err *kernel.Error
		apply, err = rules()
		return err
	})
	if err == nil && apply != nil {
		apply(record)
		if c.rt != nil {
			c.rt.Publish(c, record)
		}
	}
	return record, err
}

// Generated decides the actions entity declarations generate: standard
// create, edit and archive (ADR-0016 D5) and lifecycle transitions (ADR-0017
// D1); ok is false for any other schema. allowed (may be nil) adds the app's
// conditions to the catalog's role check.
func (l *Ledger) Generated(c Caller, s *pb.Submission, now time.Time, allowed func() bool, entities ...Entity) (*pb.ChangeRecord, *kernel.Error, bool) {
	schema := s.GetSchema().GetName()
	for _, e := range entities {
		verb, found := strings.CutPrefix(schema, e.Type+".")
		if !found {
			continue
		}
		if verb == "create" && e.Standard.Create || verb == "edit" && e.Standard.Edit || verb == "archive" && e.Standard.Archive {
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return standard(c, e, verb, s)
			})
			return record, err, true
		}
		if e.Lifecycle == nil {
			continue
		}
		if i := slices.IndexFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Name == verb }); i >= 0 {
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return transition(c, e, e.Lifecycle.Transitions[i], s, now)
			})
			return record, err, true
		}
	}
	return nil, nil, false
}

// transition moves a record along its lifecycle, through the transition's Do.
func transition(c Caller, e Entity, t Transition, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.rt == nil {
		return nil, notFound()
	}
	typ := reflect.TypeOf(e.Model)
	existing, known := c.rt.Get(c, typ, s.GetTarget().GetId())
	if !known {
		return nil, notFound()
	}
	info, _ := Describe("", e, func(reflect.Type) string { return "?" })
	f, _ := info.Field(e.Lifecycle.Field)
	v := reflect.New(typ)
	v.Elem().Set(reflect.ValueOf(existing))
	status := v.Elem().FieldByIndex(f.Index)
	from := status.String()
	if v.Elem().Field(0).Interface().(Record).Archived || !slices.Contains(t.From, from) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT} // not in a state the transition leaves
	}
	if t.Do != nil {
		if err := t.Do(c, v.Interface(), s.GetPayload(), now); err != nil {
			return nil, err
		}
	}
	if to := status.String(); to == from && !slices.Contains(t.To, from) {
		status.SetString(t.To[0])
	} else if !slices.Contains(t.To, to) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT} // Do chose a status the transition does not reach
	}
	value := v.Elem().Interface()
	if err := c.rt.Check(c, value); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) {
		c.rt.Put(c, r, value)
		if t.After != nil {
			after := reflect.New(typ)
			after.Elem().Set(reflect.ValueOf(value))
			t.After(c, r, after.Interface(), now)
		}
	}, nil
}

func standard(c Caller, e Entity, verb string, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	if c.rt == nil {
		return nil, notFound()
	}
	t := reflect.TypeOf(e.Model)
	id := s.GetTarget().GetId()
	existing, known := c.rt.Get(c, t, id)
	v := reflect.New(t)
	switch {
	case verb == "create" && known:
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	case verb != "create" && !known:
		return nil, notFound()
	case verb != "create":
		v.Elem().Set(reflect.ValueOf(existing))
	}
	if verb == "archive" {
		v.Elem().Field(0).Addr().Interface().(*Record).Archived = true
	} else {
		var given map[string]json.RawMessage
		if json.Unmarshal(s.GetPayload(), &given) != nil {
			return nil, invalid
		}
		info, _ := Describe("", e, func(reflect.Type) string { return "?" })
		for name := range given {
			if f, ok := info.Field(name); !ok || f.ReadOnly {
				return nil, invalid // unknown or read-only fields are never set by a generated action
			}
		}
		if json.Unmarshal(s.GetPayload(), v.Interface()) != nil {
			return nil, invalid
		}
		v.Elem().Field(0).Addr().Interface().(*Record).ID = id
	}
	value := v.Elem().Interface()
	if err := c.rt.Check(c, value); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { c.rt.Put(c, r, value) }, nil
}
