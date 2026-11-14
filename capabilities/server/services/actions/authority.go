package actions

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type authority struct {
	mu           sync.Mutex
	tenant       string
	authority    string
	declarations []*pb.AuthorityDeclaration
	archive      platform.ChangeArchive
	changes      *protocol.ChangeLog
	authorities  *protocol.Authorities
	schemaRefs   []*pb.SchemaRef
	schemas      *protocol.SchemaRegistry
	catalog      *platform.Catalog
}

func NewAuthority(tenant, id string, catalog *platform.Catalog, classes ...string) platform.Authority {
	var schemas []*pb.SchemaRef
	for _, a := range catalog.All() {
		for family := uint32(1); family <= schemaFamily(a); family++ {
			schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: family})
		}
	}
	registry := protocol.NewSchemaRegistry(schemas)
	l := &authority{tenant: tenant, authority: id, changes: protocol.NewChangeLog(registry), schemas: registry,
		schemaRefs: slices.Clone(schemas), authorities: protocol.NewAuthorities(), catalog: catalog}
	for _, class := range classes {
		d := &pb.AuthorityDeclaration{TenantId: tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: id, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return l
}

func (l *authority) Extend(classes []string, actions []platform.Action) error {
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
	l.catalog.Add(actions...)
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

func schemaFamily(action platform.Action) uint32 {
	if action.SchemaVersion != nil {
		return action.SchemaVersion.Family
	}
	return 1
}

func (l *authority) Declarations() []*pb.AuthorityDeclaration { return l.declarations }

func (l *authority) ApplyAcceptedChange(record *pb.ChangeRecord) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	changed, err := l.changes.ApplyAccepted(record)
	if err == nil && l.archive != nil {
		l.changes.Compact(l.archive)
	}
	return changed, err
}

func (l *authority) UseChangeArchive(archive platform.ChangeArchive) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.archive = archive
	l.changes.Compact(archive)
}

func (l *authority) ForkAcceptedChanges() platform.ChangeLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Fork()
}

func (l *authority) ForkRead() (platform.Authority, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	registry := protocol.NewSchemaRegistry(l.schemaRefs)
	fork := &authority{tenant: l.tenant, authority: l.authority, declarations: slices.Clone(l.declarations),
		schemaRefs: slices.Clone(l.schemaRefs), schemas: registry,
		changes: l.changes.ForkWithSchemas(registry), archive: l.archive,
		authorities: protocol.NewAuthorities(), catalog: platform.NewCatalog(l.catalog.All()...)}
	for _, declaration := range fork.declarations {
		if err := fork.authorities.Declare(declaration); err != nil {
			return nil, err
		}
	}
	return fork, nil
}

func (l *authority) RecordsFor(tenant string) []*pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Records(tenant)
}

func (l *authority) LastAccepted(tenant string) *pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Last(tenant)
}

func (l *authority) AcceptedFor(tenant, key string) *pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Accepted(tenant, key)
}

func (l *authority) Receive(c platform.Caller, s *pb.Submission, now time.Time,
	allowed func() bool, rules func() (func(*pb.ChangeRecord), *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	catalog := l.catalogFor(c)
	if draft := c.StagedDecision(); draft != nil {
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
	probing := c.Probing()
	refused := false
	var decision platform.AuthzDecision
	var changes platform.ChangeLog = l.changes
	if draft := c.StagedDecision(); draft != nil {
		changes = draft.DraftChanges(l, l.changes.Fork)
		if changes == nil || changes == l.changes {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}
	if probing {
		changes = changes.Fork()
	}
	policy := func(protocol.Caller, *pb.Submission) bool {
		if !c.Replaying || probing {
			decision = catalog.DecideSubmission(c, s, now)
		}
		ok := c.Replaying && !probing || decision.Allow && (allowed == nil || c.Automation || allowed())
		refused = !ok

		return ok
	}
	var apply func(*pb.ChangeRecord)
	decided := false
	receiver := protocol.Receiver{Changes: changes, Authorities: l.authorities, Policy: policy}
	record, err := receiver.Receive(protocol.Caller{Tenant: l.tenant, Principal: c.ID}, s, now, func() *kernel.Error {
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
			err = platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", decision.Layers[len(decision.Layers)-1].Name, decision.Reason)
		} else {
			err = platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not {action} on this record", c.ID, s.GetSchema().GetName())
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
		if c.Runtime() != nil && (apply != nil || c.Staging() || !c.Replaying) {
			c.Runtime().Publish(c, record)
		}
	}
	return record, err
}

func (l *authority) catalogFor(c platform.Caller) *platform.Catalog {
	if draft := c.StagedDecision(); draft != nil {
		if selected := draft.DecisionCatalog(l); selected != nil {
			return selected
		}
	}
	return l.catalog
}

func (l *authority) checked(c platform.Caller, s *pb.Submission, now time.Time, catalog *platform.Catalog, rules func() (func(*pb.ChangeRecord), *kernel.Error)) func() (func(*pb.ChangeRecord), *kernel.Error) {
	declared, _ := catalog.Action(s.GetSchema().GetName())
	return func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Replaying {
			var payload map[string]any
			json.Unmarshal(s.GetPayload(), &payload)
			fields, resolved := platform.ResolveValueTypes(declared.Payload, c.ResolveValueType)
			if !resolved {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A bound value type is unavailable")
			}
			issues := platform.InputIssues(fields, payload)
			if declared.DeferRequired {
				issues = slices.DeleteFunc(issues, func(issue platform.FieldIssue) bool { return issue.Code == "required" })
			}
			if len(issues) > 0 {
				return nil, c.RefuseFields(issues)
			}
			var previous map[string]any
			if declared.EditsRecord() && slices.ContainsFunc(declared.Payload, func(f platform.Field) bool { return f.Ref == "enterprise.element" }) {
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
				if len(f.Choices) > 0 && !slices.Contains(f.Choices, v) || f.Ref != "" && c.Runtime() != nil && !c.Runtime().Readable(c, f.Ref+"/"+v) {
					return nil, c.RefuseFields([]platform.FieldIssue{{Code: "reference", Path: []string{f.Name}, Message: "Choose a readable listed value."}})
				}
				if f.Ref == "enterprise.element" && (f.Stereotype != "" || len(f.Stereotypes) > 0) && c.Runtime() != nil {
					if el, ok := c.Runtime().Element(c, v, now); !ok || !(f.Stereotype != "" && el.Stereotype == f.Stereotype || slices.Contains(f.Stereotypes, el.Stereotype)) {
						return nil, c.RefuseFields([]platform.FieldIssue{{Code: "reference", Path: []string{f.Name}, Message: "Choose a readable listed value."}})
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

func (l *authority) Generated(c platform.Caller, s *pb.Submission, now time.Time, allowed func() bool, entities ...platform.Entity) (*pb.ChangeRecord, *kernel.Error, bool) {
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
		if i := slices.IndexFunc(e.Lifecycle.Transitions, func(t platform.Transition) bool { return t.Name == verb }); i >= 0 {
			tr := e.Lifecycle.Transitions[i]
			var failure *kernel.Error
			if tr.Txn != nil {
				if !c.Staging() || c.Replaying || tr.ApplyTxn == nil || tr.After != nil {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transactions require a private accepted decision / 业务事务须经私有接受决策"), true
				}
				apply := tr.ApplyTxn
				tr.ApplyTxn = func(c platform.Caller, r *pb.ChangeRecord, at time.Time) *kernel.Error {
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
		for _, suffix := range []string{platform.ApprovalHeld, platform.ApprovalRejected, platform.ApprovalReturned} {
			name, found := strings.CutSuffix(verb, suffix)
			i := slices.IndexFunc(e.Lifecycle.Transitions, func(t platform.Transition) bool {
				return t.Name == name && t.Approval != nil && t.Approval.Pending != ""
			})
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

func approvalMove(t platform.Transition, suffix string) platform.Transition {
	back := t.From[0]
	switch suffix {
	case platform.ApprovalHeld:
		return platform.Transition{Name: t.Name + suffix, From: t.From, To: []string{t.Approval.Pending}}
	case platform.ApprovalRejected:
		if t.Approval.Rejected != "" {
			back = t.Approval.Rejected
		}
	}
	return platform.Transition{Name: t.Name + suffix, From: []string{t.Approval.Pending}, To: []string{back}}
}

func transition(c platform.Caller, e platform.Entity, t platform.Transition, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.Runtime() == nil {
		return nil, notFound()
	}
	typ := reflect.TypeOf(e.Model)
	existing, known := c.Runtime().Get(c, typ, s.GetTarget().GetId())
	if !known || !inScope(c, e.Type, s.GetTarget().GetId()) {
		return nil, notFound()
	}
	info, _ := platform.Describe("", e, func(reflect.Type) string { return "?" })
	f, _ := info.Field(e.Lifecycle.Field)
	v := reflect.New(typ)
	v.Elem().Set(reflect.ValueOf(existing))
	status := v.Elem().FieldByIndex(f.Index)
	from := status.String()
	pending := t.Approval != nil && t.Approval.Pending != "" && from == t.Approval.Pending && !c.Automation && !c.Probing()
	id := s.GetTarget().GetId()
	if v.Elem().Field(0).Interface().(platform.Record).Archived {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is archived", id)
	}
	if !slices.Contains(t.From, from) && !pending {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is {state}; {transition} takes it only from {states}", id, from, t.Name, strings.Join(t.From, ", "))
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
	value := v.Elem().Interface()
	if err := c.Runtime().Check(c, value); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) {
		if t.Txn != nil {
			t.ApplyTxn(c, r, now)
			return
		}
		c.Runtime().Put(c, r, value)
		if t.After != nil {
			after := reflect.New(typ)
			after.Elem().Set(reflect.ValueOf(value))
			t.After(c, r, after.Interface(), now)
		}
	}, nil
}

func standard(c platform.Caller, e platform.Entity, verb string, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	if c.Runtime() == nil {
		return nil, notFound()
	}
	t := reflect.TypeOf(e.Model)
	id := s.GetTarget().GetId()
	existing, known := c.Runtime().Get(c, t, id)
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
		v.Elem().Field(0).Addr().Interface().(*platform.Record).Archived = true
	} else {
		var given map[string]json.RawMessage
		if json.Unmarshal(s.GetPayload(), &given) != nil {
			return nil, invalid
		}
		info, _ := platform.Describe("", e, func(reflect.Type) string { return "?" })
		for name := range given {
			if f, ok := info.Field(name); !ok || f.ReadOnly || !c.Replaying && !c.Automation && !f.WritesAny(c.RolesHere(), c.Rules()) {
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
		v.Elem().Field(0).Addr().Interface().(*platform.Record).ID = id
	}
	if c.RestoreSource() {
		v.Elem().Field(0).Addr().Interface().(*platform.Record).Archived = false
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
	value := v.Elem().Interface()
	if verb != "create" || e.CreateTxn == nil || c.Replaying || c.SourceProjection() {
		if err := c.Runtime().Check(c, value); err != nil {
			return nil, err
		}
	}
	return func(r *pb.ChangeRecord) {
		c.Runtime().Put(c, r, value)
		if verb == "create" && e.CreateTxn != nil && e.ApplyCreateTxn != nil && !c.Replaying && !c.SourceProjection() {
			if c.CreationDepth() >= 16 {
				c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Creation transactions exceed their depth limit"))
				return
			}
			created := c.WithTransactionInput(s)
			created = created.Deeper()
			if err := e.ApplyCreateTxn(created, r, now); err != nil {
				c.FailDecision(err)
				return
			}
			if final, exists := c.Runtime().Get(c, t, id); exists {
				if err := c.Runtime().Check(c, final); err != nil {
					c.FailDecision(err)
				}
			}
		}
	}, nil
}

func inScope(c platform.Caller, typ, id string) bool {
	return c.Replaying || c.Automation || c.Probing() || c.Runtime().Readable(c, typ+"/"+id)
}

func (l *authority) ID() string                 { return l.authority }
func (l *authority) Catalog() *platform.Catalog { return l.catalog }

func notFound() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} }
