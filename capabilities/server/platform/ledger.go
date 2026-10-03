package platform

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

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
	authority    string
	declarations []*pb.AuthorityDeclaration
	changes      *kernel.ChangeLog
	authorities  *kernel.Authorities
	// schemas is the change log's registry, kept so the package can teach it a
	// schema that did not exist when it started (K7 S7, ADR-0034).
	schemas *kernel.SchemaRegistry
	Catalog *Catalog
}

// NewLedger declares authority as the tenant server for classes and accepts the
// catalog's actions (version 1 of each schema).
func NewLedger(tenant, authority string, catalog *Catalog, classes ...string) *Ledger {
	var schemas []*pb.SchemaRef
	for _, a := range catalog.actions {
		schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: 1})
	}
	registry := kernel.NewSchemaRegistry(schemas, nil)
	l := &Ledger{tenant: tenant, authority: authority, changes: kernel.NewChangeLog(registry), schemas: registry,
		authorities: kernel.NewAuthorities(authority), Catalog: catalog}
	for _, class := range classes {
		d := &pb.AuthorityDeclaration{TenantId: tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: authority, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return l
}

// Extend declares data classes and actions after composition: what an object a
// tenant defined and published needs of its app's ledger (ADR-0034 D2). The
// kernel takes the new schemas and the authority over the new classes.
// It is called from inside a decision of this ledger — the transition that
// publishes the definition — so the ledger's own lock is already held and is
// not taken again; the catalog it extends carries its own lock for the readers
// outside.
func (l *Ledger) Extend(classes []string, actions []Action) error {
	var schemas []*pb.SchemaRef
	for _, a := range actions {
		if a.Schema == "" || a.Target == "" || a.Title == "" || len(a.Roles) == 0 && !a.Automation {
			return fmt.Errorf("action %q lacks a schema, target, title or roles", a.Schema)
		}
		schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: 1})
	}
	if err := l.schemas.Learn(schemas...); err != nil { // K7 S7
		return err
	}
	l.Catalog.Add(actions...)
	for _, class := range classes {
		if slices.ContainsFunc(l.declarations, func(d *pb.AuthorityDeclaration) bool { return d.GetDataClass() == class }) {
			continue
		}
		d := &pb.AuthorityDeclaration{TenantId: l.tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: l.authority, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return nil
}

// Declarations are the package's authority declarations, for edges (K5 A9).
func (l *Ledger) Declarations() []*pb.AuthorityDeclaration { return l.declarations }

// ApplyAcceptedChange advances the kernel log from a durable result. It does
// not run current catalog policy, rules or a generated action (ADR-0038 19a).
// The boolean says whether this result was new to the log, so the host can
// avoid applying its record images twice after a retry or partial recovery.
func (l *Ledger) ApplyAcceptedChange(record *pb.ChangeRecord) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.ApplyAccepted(record)
}

// ForkAcceptedChanges validates a complete host batch without advancing the
// authoritative log. The tenant commit lock still owns promotion.
func (l *Ledger) ForkAcceptedChanges() *kernel.ChangeLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Fork()
}

// RecordsFor returns a defensive copy of tenant's accepted records,
// safe to call concurrently with Receive.
func (l *Ledger) RecordsFor(tenant string) []*pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Records(tenant)
}

// SetFacts supplies the change log's tenant fact-check callback (K2, K4 C11).
func (l *Ledger) SetFacts(facts func(tenant, factID string) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.changes.Facts = facts
}

// AcceptedFor returns a saved receipt for a key without running application
// decision code. The host uses this before staging a generated action.
func (l *Ledger) AcceptedFor(tenant, key string) *pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.changes.Records(tenant) {
		if record.GetSubmission().GetIdempotencyKey() == key {
			return proto.Clone(record).(*pb.ChangeRecord)
		}
	}
	return nil
}

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
	rules = l.checked(c, s, rules)
	probing := c.rt != nil && c.rt.Probing()
	refused := false // the kernel's policy step said no
	changes := l.changes
	// ADR-0038 19a: a host-owned decision view may use a private change log.
	// Ordinary callers and replay retain the existing authoritative path.
	if draft, ok := c.rt.(interface {
		DraftChanges(*Ledger, func() *kernel.ChangeLog) *kernel.ChangeLog
	}); ok {
		changes = draft.DraftChanges(l, l.changes.Fork)
	}
	if probing {
		// Probes validate kernel identity, key conflicts and expected revisions
		// too. Only their private log advances; their apply callback never runs.
		changes = changes.Fork()
	}
	receiver := kernel.Receiver{Changes: changes, Authorities: l.authorities,
		Policy: func(kernel.Caller, *pb.Submission) bool {
			ok := c.Replaying && !probing || c.Automation || l.Catalog.Permits(c.Role(), s.GetSchema().GetName()) && (allowed == nil || allowed())
			refused = !ok
			return ok
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
	if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED && err.Message == "" && refused {
		err = l.denied(c, s) // the kernel's policy step refused: say whose role does not reach
	}
	if probing {
		return nil, err
	}
	if err == nil && apply != nil {
		apply(record)
		if c.rt != nil {
			c.rt.Publish(c, record)
		}
	}
	return record, err
}

// denied is a refusal of the catalog's role check, with why (F-23): the
// member's role in the app, or that they hold none, does not include the action.
func (l *Ledger) denied(c Caller, s *pb.Submission) *kernel.Error {
	action := s.GetSchema().GetName()
	if declared, ok := l.Catalog.Action(action); ok && declared.Title != "" {
		action = declared.Title
	}
	if c.Role() == "" {
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} holds no role in {app}, so may not {action}", c.ID, c.App, action)
	}
	if l.Catalog.Permits(c.Role(), s.GetSchema().GetName()) {
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not {action} on this record", c.ID, action)
	}
	return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The role {role} in {app} may not {action}", c.Role(), c.App, action)
}

// checked puts before rules the check of the payload's declared choices and
// references (ADR-0028 D5): after the policy, as the kernel's order has it
// (K6 T2); a replay does not check again.
func (l *Ledger) checked(c Caller, s *pb.Submission, rules func() (func(*pb.ChangeRecord), *kernel.Error)) func() (func(*pb.ChangeRecord), *kernel.Error) {
	declared, _ := l.Catalog.Action(s.GetSchema().GetName())
	return func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Replaying {
			var payload map[string]any
			json.Unmarshal(s.GetPayload(), &payload)
			for _, f := range declared.Payload {
				v, ok := payload[f.Name].(string)
				if !ok || v == "" {
					continue
				}
				if len(f.Choices) > 0 && !slices.Contains(f.Choices, v) || f.Ref != "" && c.rt != nil && !c.rt.Readable(c, f.Ref+"/"+v) {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
			}
		}
		if rules == nil {
			return nil, nil
		}
		return rules()
	}
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
		for _, suffix := range []string{ApprovalHeld, ApprovalRejected, ApprovalReturned} {
			name, found := strings.CutSuffix(verb, suffix)
			i := slices.IndexFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Name == name && t.Approval != nil && t.Approval.Pending != "" })
			if !found || i < 0 {
				continue
			}
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return transition(c, e, approvalMove(e.Lifecycle.Transitions[i], suffix), s, now)
			})
			return record, err, true
		}
	}
	return nil, nil, false
}

// approvalMove is the move of a record whose transition t waits for approval
// (F-38): into its pending state, or out of it on a rejection or a return.
func approvalMove(t Transition, suffix string) Transition {
	back := t.From[0]
	switch suffix {
	case ApprovalHeld:
		return Transition{Name: t.Name + suffix, From: t.From, To: []string{t.Approval.Pending}}
	case ApprovalRejected:
		if t.Approval.Rejected != "" {
			back = t.Approval.Rejected
		}
	}
	return Transition{Name: t.Name + suffix, From: []string{t.Approval.Pending}, To: []string{back}}
}

// transition moves a record along its lifecycle, through the transition's Do.
// A transition that waits in a pending state also leaves it, once approved.
func transition(c Caller, e Entity, t Transition, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.rt == nil {
		return nil, notFound()
	}
	typ := reflect.TypeOf(e.Model)
	existing, known := c.rt.Get(c, typ, s.GetTarget().GetId())
	if !known || !inScope(c, e.Type, s.GetTarget().GetId()) {
		return nil, notFound()
	}
	info, _ := Describe("", e, func(reflect.Type) string { return "?" })
	f, _ := info.Field(e.Lifecycle.Field)
	v := reflect.New(typ)
	v.Elem().Set(reflect.ValueOf(existing))
	status := v.Elem().FieldByIndex(f.Index)
	from := status.String()
	pending := t.Approval != nil && t.Approval.Pending != "" && from == t.Approval.Pending && !c.Automation && !c.rt.Probing() // run by its approval, not asked again
	id := s.GetTarget().GetId()
	if v.Elem().Field(0).Interface().(Record).Archived {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is archived", id)
	}
	if !slices.Contains(t.From, from) && !pending { // not in a state the transition leaves
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is {state}; {transition} takes it only from {states}", id, from, t.Name, strings.Join(t.From, ", "))
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
	if known && verb != "create" && !inScope(c, e.Type, id) {
		return nil, notFound()
	}
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
			if f, ok := info.Field(name); !ok || f.ReadOnly || !c.Replaying && !c.Automation && !f.Writes(c.Role()) {
				return nil, invalid // unknown or read-only fields are never set by a generated action
			}
		}
		// A provided field is a replacement, including a slice/map/struct.
		// Decoding a patch into the old struct reuses slice elements and maps,
		// retaining nested values the caller explicitly removed.
		base, err := json.Marshal(v.Interface())
		var merged map[string]json.RawMessage
		if err != nil || json.Unmarshal(base, &merged) != nil {
			return nil, invalid
		}
		for name, value := range given {
			merged[name] = value
		}
		raw, err := json.Marshal(merged)
		v = reflect.New(t)
		if err != nil || json.Unmarshal(raw, v.Interface()) != nil {
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

// inScope says whether the caller may act on a record: only one they may read
// (ADR-0037 18b). A generated action on a record outside the member's scope is
// refused as if it were not there, as a read is. Replay, the app's own
// automation and an approval taking a held step act for the app, not a reader.
func inScope(c Caller, typ, id string) bool {
	return c.Replaying || c.Automation || c.rt.Probing() || c.rt.Readable(c, typ+"/"+id)
}
