package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type KernelFactory interface {
	Changes(SchemaRegistry) ChangeLog
	Schemas([]*pb.SchemaRef) SchemaRegistry
	Authorities(string) Authorities
	Works() Works
	Connectors() Connectors
	Identity([]*pb.EntityRef) Identity
	Facts(SchemaRegistry) Facts
	Receive(ChangeLog, Authorities, kernel.Policy, kernel.Caller, *pb.Submission, time.Time, func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error)
}

type SchemaRegistry interface {
	Accepts(*pb.SchemaRef) bool
	Learn(...*pb.SchemaRef) *kernel.Error
}
type Authorities interface {
	Declare(*pb.AuthorityDeclaration) *kernel.Error
	Authorize(*pb.Submission) *kernel.Error
}
type ChangeLog interface {
	ForkLog() ChangeLog
	SetFacts(func(string, string) bool)
	Records(string) []*pb.ChangeRecord
	Last(string) *pb.ChangeRecord
	Accepted(string, string) *pb.ChangeRecord
	Compact(kernel.ChangeArchive)
	ApplyAccepted(*pb.ChangeRecord) (bool, error)
	SubmitChecked(*pb.Submission, time.Time, func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error)
	Restore(string, []*pb.ChangeRecord)
	Adopt([]*pb.ChangeRecord) *kernel.Error
}
type Works interface {
	Start(string, string) (uint32, string, *kernel.Error)
	Checkpoint(string, uint32, uint32, string) *kernel.Error
	Finish(string, uint32, bool) *kernel.Error
	Cancel(string) *kernel.Error
	CloseOwner(string)
	Get(string) (*pb.Work, *kernel.Error)
	All() []*pb.Work
	Restore([]*pb.Work)
}
type Connectors interface {
	Register(*pb.ConnectorDescriptor) *kernel.Error
	Deliver(string, string, string, string, string, time.Time) *kernel.Error
	Heartbeat(string, string, time.Time) *kernel.Error
	Status(string, string, time.Time) (*pb.ConnectorStatus, *kernel.Error)
	Descriptors(string) []*pb.ConnectorDescriptor
	State() ([]*pb.ConnectorDescriptor, []kernel.ConnectorMark)
	Restore([]*pb.ConnectorDescriptor, []kernel.ConnectorMark)
}

type Identity interface {
	Create(*pb.EntityRef) *kernel.Error
	AddRedirect(*pb.Redirect) *kernel.Error
	Resolve(*pb.EntityRef) ([]kernel.Ref, *kernel.Error)
	State() kernel.IdentityState
	Restore(kernel.IdentityState)
}

type Facts interface {
	Record(*pb.Fact, time.Time) (*pb.FactRecord, *kernel.Error)
	CurrentClaims(string, *pb.EntityRef, string) []*pb.FactRecord
	Accepted(string, string) *pb.FactRecord
	Records(string) []*pb.FactRecord
	Restore(string, []*pb.FactRecord)
}

type KernelRuntime interface{ Kernel() KernelFactory }

func (c Caller) Kernel() KernelFactory {
	if runtime, ok := c.rt.(KernelRuntime); ok {
		return runtime.Kernel()
	}
	return NativeKernel{}
}

type NativeKernel struct{}
type nativeChanges struct{ *kernel.ChangeLog }

func (n nativeChanges) ForkLog() ChangeLog                   { return nativeChanges{n.Fork()} }
func (n nativeChanges) SetFacts(f func(string, string) bool) { n.Facts = f }
func (NativeKernel) Schemas(refs []*pb.SchemaRef) SchemaRegistry {
	return kernel.NewSchemaRegistry(refs, nil)
}
func (NativeKernel) Changes(s SchemaRegistry) ChangeLog         { return nativeChanges{kernel.NewChangeLog(s)} }
func (NativeKernel) Authorities(id string) Authorities          { return kernel.NewAuthorities(id) }
func (NativeKernel) Works() Works                               { return kernel.NewWorks() }
func (NativeKernel) Connectors() Connectors                     { return kernel.NewConnectors() }
func (NativeKernel) Identity(entities []*pb.EntityRef) Identity { return kernel.NewIdentity(entities) }
func (NativeKernel) Facts(schema SchemaRegistry) Facts {
	return kernel.NewFactLog(schema)
}
func (NativeKernel) Receive(log ChangeLog, authorities Authorities, policy kernel.Policy, caller kernel.Caller, s *pb.Submission, at time.Time, domain func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	r := kernel.Receiver{Changes: log, Authorities: authorities, Policy: policy}
	return r.Receive(caller, s, at, domain)
}

type Ledger struct {
	mu           sync.Mutex
	tenant       string
	authority    string
	declarations []*pb.AuthorityDeclaration
	archive      kernel.ChangeArchive
	changes      ChangeLog
	authorities  Authorities
	kernel       KernelFactory
	schemaRefs   []*pb.SchemaRef
	schemas      SchemaRegistry
	Catalog      *Catalog
}

func NewLedger(tenant, authority string, catalog *Catalog, classes ...string) *Ledger {
	return NewLedgerWithKernel(NativeKernel{}, tenant, authority, catalog, classes...)
}

func NewLedgerWithKernel(engine KernelFactory, tenant, authority string, catalog *Catalog, classes ...string) *Ledger {
	var schemas []*pb.SchemaRef
	for _, a := range catalog.actions {
		for family := uint32(1); family <= schemaFamily(a); family++ {
			schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: family})
		}
	}
	registry := engine.Schemas(schemas)
	l := &Ledger{tenant: tenant, authority: authority, changes: engine.Changes(registry), schemas: registry,
		kernel: engine, schemaRefs: slices.Clone(schemas), authorities: engine.Authorities(authority), Catalog: catalog}
	for _, class := range classes {
		d := &pb.AuthorityDeclaration{TenantId: tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: authority, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return l
}

func (l *Ledger) BindKernel(engine KernelFactory) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.changes.Last(l.tenant) != nil {
		return fmt.Errorf("kernel binding requires an unstarted ledger")
	}
	registry := engine.Schemas(l.schemaRefs)
	changes, authorities := engine.Changes(registry), engine.Authorities(l.authority)
	for _, declaration := range l.declarations {
		if err := authorities.Declare(declaration); err != nil {
			return err
		}
	}
	l.kernel, l.schemas, l.changes, l.authorities = engine, registry, changes, authorities
	return nil
}

func (l *Ledger) Extend(classes []string, actions []Action) error {
	var schemas []*pb.SchemaRef
	for _, a := range actions {
		if a.Schema == "" || a.Target == "" || a.Title == "" || len(a.Roles) == 0 && !a.Automation {
			return fmt.Errorf("action %q lacks a schema, target, title or roles", a.Schema)
		}
		if a.SchemaVersion != nil {
			if err := a.SchemaVersion.Check(); err != nil {
				return err
			}
		}
		for family := uint32(1); family <= schemaFamily(a); family++ {
			schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: family})
		}
	}
	if err := l.schemas.Learn(schemas...); err != nil {
		return err
	}
	l.schemaRefs = append(l.schemaRefs, schemas...)
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

func schemaFamily(action Action) uint32 {
	if action.SchemaVersion != nil {
		return action.SchemaVersion.Family
	}
	return 1
}

func (l *Ledger) HistoricalSchemas(schemas ...*pb.SchemaRef) *kernel.Error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.schemas.Learn(schemas...); err != nil {
		return err
	}
	l.schemaRefs = append(l.schemaRefs, schemas...)
	return nil
}

func (l *Ledger) Declarations() []*pb.AuthorityDeclaration { return l.declarations }

func (l *Ledger) ApplyAcceptedChange(record *pb.ChangeRecord) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	changed, err := l.changes.ApplyAccepted(record)
	if err == nil && l.archive != nil {
		l.changes.Compact(l.archive)
	}
	return changed, err
}

func (l *Ledger) UseChangeArchive(archive kernel.ChangeArchive) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.archive = archive
	l.changes.Compact(archive)
}

func (l *Ledger) ForkAcceptedChanges() ChangeLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.ForkLog()
}

func (l *Ledger) RecordsFor(tenant string) []*pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Records(tenant)
}

func (l *Ledger) LastAccepted(tenant string) *pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Last(tenant)
}

func (l *Ledger) SetFacts(facts func(tenant, factID string) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.changes.SetFacts(facts)
}

func (l *Ledger) AcceptedFor(tenant, key string) *pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Accepted(tenant, key)
}

func (l *Ledger) Receive(c Caller, s *pb.Submission, now time.Time,
	allowed func() bool, rules func() (func(*pb.ChangeRecord), *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	catalog := l.catalogFor(c)
	if draft, ok := c.rt.(StagedRuntime); ok {
		release, refusal := draft.HoldChanges(l)
		if refusal != nil {
			return nil, refusal
		}
		if release == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		defer release()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !c.Replaying && !catalog.Enabled(s.GetSchema().GetName()) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	rules = l.checked(c, s, now, catalog, rules)
	probing := c.rt != nil && c.rt.Probing()
	refused := false
	var decision AuthzDecision
	changes := l.changes
	if draft, ok := c.rt.(StagedRuntime); ok {
		changes = draft.DraftChanges(l, l.changes.ForkLog)
		if changes == nil || changes == l.changes {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}
	if probing {
		changes = changes.ForkLog()
	}
	policy := func(kernel.Caller, *pb.Submission) bool {
		if !c.Replaying || probing {
			decision = catalog.DecideSubmission(c, s, now)
		}
		ok := c.Replaying && !probing || decision.Allow && (allowed == nil || c.Automation || allowed())
		refused = !ok

		return ok
	}
	var apply func(*pb.ChangeRecord)
	decided := false
	record, err := l.kernel.Receive(changes, l.authorities, policy, kernel.Caller{Tenant: l.tenant, Principal: c.ID}, s, now, func() *kernel.Error {
		decided = true
		if rules == nil {
			return nil
		}
		var err *kernel.Error
		apply, err = rules()
		return err
	})
	if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED && err.Message == "" && refused {
		if !decision.Allow && len(decision.Layers) > 0 {
			err = Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", decision.Layers[len(decision.Layers)-1].Name, decision.Reason)
		} else {
			err = Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not {action} on this record", c.ID, s.GetSchema().GetName())
		}
	}
	if probing {
		if c.Staging() && err == nil && decided && apply != nil {
			apply(record)
		}
		return nil, err
	}
	if err == nil && decided {
		if apply != nil {
			apply(record)
		}
		if c.rt != nil && (apply != nil || c.Staging() || !c.Replaying) {
			c.rt.Publish(c, record)
		}
	}
	return record, err
}

func (l *Ledger) catalogFor(c Caller) *Catalog {
	if bound, ok := c.rt.(DecisionDeclarationRuntime); ok {
		if selected := bound.DecisionCatalog(l); selected != nil {
			return selected
		}
	}
	return l.Catalog
}

func (l *Ledger) checked(c Caller, s *pb.Submission, now time.Time, catalog *Catalog, rules func() (func(*pb.ChangeRecord), *kernel.Error)) func() (func(*pb.ChangeRecord), *kernel.Error) {
	declared, _ := catalog.Action(s.GetSchema().GetName())
	return func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Replaying {
			var payload map[string]any
			json.Unmarshal(s.GetPayload(), &payload)
			fields, resolved := ResolveValueTypes(declared.Payload, c.ResolveValueType)
			if !resolved {
				return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A bound value type is unavailable")
			}
			issues := InputIssues(fields, payload)
			if declared.DeferRequired {
				issues = slices.DeleteFunc(issues, func(issue FieldIssue) bool { return issue.Code == "required" })
			}
			if len(issues) > 0 {
				return nil, c.RefuseFields(issues)
			}
			var previous map[string]any
			if declared.recordEdit && slices.ContainsFunc(declared.Payload, func(f Field) bool { return f.Ref == "enterprise.element" }) {
				if raw, err := c.ReadRecord(declared.Target, s.GetTarget().GetId(), now); err == nil {
					_ = json.Unmarshal(raw, &previous)
				}
			}
			for _, f := range declared.Payload {
				v, ok := payload[f.Name].(string)
				if !ok || v == "" {
					continue
				}
				if f.Ref == "enterprise.element" && previous[f.Name] == v {
					continue
				}
				if len(f.Choices) > 0 && !slices.Contains(f.Choices, v) || f.Ref != "" && c.rt != nil && !c.rt.Readable(c, f.Ref+"/"+v) {
					return nil, c.RefuseFields([]FieldIssue{{Code: "reference", Path: []string{f.Name}, Message: "Choose a readable listed value."}})
				}
				if f.Ref == "enterprise.element" && (f.Stereotype != "" || len(f.Stereotypes) > 0) && c.rt != nil {
					if el, ok := c.rt.Element(c, v, now); !ok || !(f.Stereotype != "" && el.Stereotype == f.Stereotype || slices.Contains(f.Stereotypes, el.Stereotype)) {
						return nil, c.RefuseFields([]FieldIssue{{Code: "reference", Path: []string{f.Name}, Message: "Choose a readable listed value."}})
					}
				}
			}
		}
		if rules == nil {
			return nil, nil
		}
		return rules()
	}
}

func (l *Ledger) Generated(c Caller, s *pb.Submission, now time.Time, allowed func() bool, entities ...Entity) (*pb.ChangeRecord, *kernel.Error, bool) {
	schema := s.GetSchema().GetName()
	for _, e := range entities {
		verb, found := strings.CutPrefix(schema, e.Type+".")
		if !found {
			continue
		}
		if verb == "create" && e.Standard.Create || verb == "edit" && e.Standard.Edit || verb == "archive" && e.Standard.Archive {
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return standard(c, e, verb, s, now)
			})
			return record, err, true
		}
		if e.Lifecycle == nil {
			continue
		}
		if i := slices.IndexFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Name == verb }); i >= 0 {
			tr := e.Lifecycle.Transitions[i]
			if c.Replaying && tr.LegacyReplay != nil {
				tr = *tr.LegacyReplay
			}
			var failure *kernel.Error
			if tr.Txn != nil {
				if !c.Staging() || c.Replaying || tr.ApplyTxn == nil || tr.After != nil {
					return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transactions require a private accepted decision / 业务事务须经私有接受决策"), true
				}
				c.Automation = false
				apply := tr.ApplyTxn
				tr.ApplyTxn = func(c Caller, r *pb.ChangeRecord, at time.Time) *kernel.Error {
					failure = apply(c, r, at)
					return failure
				}
			}
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return transition(c, e, tr, s, now)
			})
			if err == nil && failure != nil {
				return nil, failure, true
			}
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
	pending := t.Approval != nil && t.Approval.Pending != "" && from == t.Approval.Pending && !c.Automation && !c.rt.Probing()
	id := s.GetTarget().GetId()
	if v.Elem().Field(0).Interface().(Record).Archived {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is archived", id)
	}
	if !slices.Contains(t.From, from) && !pending {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is {state}; {transition} takes it only from {states}", id, from, t.Name, strings.Join(t.From, ", "))
	}
	if t.Do != nil {
		if err := t.Do(c, v.Interface(), s.GetPayload(), now); err != nil {
			return nil, err
		}
	}
	if to := status.String(); t.Txn != nil {
	} else if to == from && !slices.Contains(t.To, from) {
		status.SetString(t.To[0])
	} else if !slices.Contains(t.To, to) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := c.PrepareBusinessVersions(v.Interface(), now); err != nil {
		return nil, err
	}
	if e.ComputeAt != nil {
		if err := e.ComputeAt(c, v.Interface(), now); err != nil {
			return nil, err
		}
	}
	if e.Compute != nil {
		e.Compute(v.Interface())
	}
	if e.Validate != nil {
		if err := e.Validate(c, v.Interface()); err != nil {
			return nil, err
		}
	}
	if e.ValidateAt != nil {
		if err := e.ValidateAt(c, v.Interface(), now); err != nil {
			return nil, err
		}
	}
	value := v.Elem().Interface()
	if err := c.rt.Check(c, value); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) {
		if t.Txn != nil {
			t.ApplyTxn(c, r, now)
			return
		}
		c.rt.Put(c, r, value)
		if t.After != nil {
			after := reflect.New(typ)
			after.Elem().Set(reflect.ValueOf(value))
			t.After(c, r, after.Interface(), now)
		}
	}, nil
}

func standard(c Caller, e Entity, verb string, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
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
			if f, ok := info.Field(name); !ok || f.ReadOnly || !c.Replaying && !c.Automation && !f.WritesAny(c.RolesHere()) {
				return nil, invalid
			}
		}
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
	if err := c.PrepareBusinessVersions(v.Interface(), now); err != nil {
		return nil, err
	}
	if e.ComputeAt != nil {
		if err := e.ComputeAt(c, v.Interface(), now); err != nil {
			return nil, err
		}
	}
	if e.Compute != nil {
		e.Compute(v.Interface())
	}
	if e.Validate != nil {
		if err := e.Validate(c, v.Interface()); err != nil {
			return nil, err
		}
	}
	if e.ValidateAt != nil {
		if err := e.ValidateAt(c, v.Interface(), now); err != nil {
			return nil, err
		}
	}
	value := v.Elem().Interface()
	if verb != "create" || e.CreateTxn == nil || c.Replaying {
		if err := c.rt.Check(c, value); err != nil {
			return nil, err
		}
	}
	return func(r *pb.ChangeRecord) {
		c.rt.Put(c, r, value)
		if verb == "create" && e.CreateTxn != nil && e.ApplyCreateTxn != nil && !c.Replaying {
			if c.creationDepth >= 16 {
				c.FailDecision(Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Creation transactions exceed their depth limit"))
				return
			}
			created := c.WithTransactionInput(s)
			created.creationDepth++
			if err := e.ApplyCreateTxn(created, r, now); err != nil {
				c.FailDecision(err)
				return
			}
			if final, exists := c.rt.Get(c, t, id); exists {
				if err := c.rt.Check(c, final); err != nil {
					c.FailDecision(err)
				}
			}
		}
	}, nil
}

func inScope(c Caller, typ, id string) bool {
	return c.Replaying || c.Automation || c.rt.Probing() || c.rt.Readable(c, typ+"/"+id)
}
