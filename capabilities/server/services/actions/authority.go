package actions

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
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
		authorities: protocol.NewAuthorities(), catalog: l.catalog.CloneActions()}
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
	allowed func() bool, rules func() (func(*pb.ChangeRecord), *platform.Error)) (*pb.ChangeRecord, *platform.Error) {
	catalog := l.catalogFor(c)
	if draft := c.StagedDecision(); draft != nil {
		release, refusal := draft.HoldChanges(l)
		if refusal != nil {
			return nil, refusal
		}
		if release == nil {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		defer release()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !c.Replaying && !catalog.Enabled(s.GetSchema().GetName()) {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	rules = l.checked(c, s, now, catalog, rules)
	probing := c.Probing()
	refused := false
	var decision platform.AuthzDecision
	var changes platform.ChangeLog = l.changes
	if draft := c.StagedDecision(); draft != nil {
		changes = draft.DraftChanges(l, l.changes.Fork)
		if changes == nil || changes == l.changes {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
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
	record, err := receiver.Receive(protocol.Caller{Tenant: l.tenant, Principal: c.ID}, s, now, func() *platform.Error {
		decided = true
		if rules == nil {
			return nil
		}
		var err *platform.Error
		apply, err = rules()
		return err
	})
	if err != nil && err.CurrentRevision != nil && !c.Readable(s.GetTarget().GetType()+"/"+s.GetTarget().GetId()) {
		err = &platform.Error{Code: err.Code}
	}
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
		if c.HasRuntime() && (apply != nil || c.Staging() || !c.Replaying) {
			c.Publish(record)
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

func (l *authority) checked(c platform.Caller, s *pb.Submission, now time.Time, catalog *platform.Catalog, rules func() (func(*pb.ChangeRecord), *platform.Error)) func() (func(*pb.ChangeRecord), *platform.Error) {
	declared, _ := catalog.Action(s.GetSchema().GetName())
	return func() (func(*pb.ChangeRecord), *platform.Error) {
		if !c.Replaying {
			var payload map[string]any
			json.Unmarshal(s.GetPayload(), &payload)
			fields, resolved := platform.ResolveValueTypes(declared.Payload, c.ResolveValueType)
			if !resolved {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A bound value type is unavailable")
			}
			issues := protocol.InputIssues(fields, payload)
			if declared.DeferRequired {
				issues = slices.DeleteFunc(issues, func(issue platform.FieldIssue) bool { return issue.Code == "required" })
			}
			if len(issues) > 0 {
				return nil, c.RefuseFields(issues)
			}
			var previous map[string]any
			if declared.RecordEdit && slices.ContainsFunc(declared.Payload, func(f platform.Field) bool { return f.Ref == "enterprise.element" }) {
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
				if len(f.Choices) > 0 && !slices.Contains(f.Choices, v) || f.Ref != "" && c.HasRuntime() && !c.Readable(f.Ref+"/"+v) {
					return nil, c.RefuseFields([]platform.FieldIssue{{Code: "reference", Path: []string{f.Name}, Message: "Choose a readable listed value."}})
				}
				if f.Ref == "enterprise.element" && (f.Stereotype != "" || len(f.Stereotypes) > 0) && c.HasRuntime() {
					if el, ok := c.Element(v, now); !ok || !(f.Stereotype != "" && el.Stereotype == f.Stereotype || slices.Contains(f.Stereotypes, el.Stereotype)) {
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

func (l *authority) Generated(c platform.Caller, s *pb.Submission, now time.Time, allowed func() bool, entities ...platform.RecordType) (*pb.ChangeRecord, *platform.Error, bool) {
	schema := s.GetSchema().GetName()
	for _, e := range entities {
		action, found := recordAction(e, schema)
		if !found {
			continue
		}
		var failure *platform.Error
		for i, rule := range action.Rules {
			if rule.Transaction == nil {
				continue
			}
			if !c.Staging() || c.Replaying || rule.Transaction.Run == nil || len(action.Effects) > 0 || slices.ContainsFunc(action.Rules, func(r platform.ActionRule) bool { return r.Receipt != nil }) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transactions require a private accepted decision / 业务事务须经私有接受决策"), true
			}
			action.Rules = slices.Clone(action.Rules)
			txn := *rule.Transaction
			apply := txn.Run
			txn.Run = func(c platform.Caller, r *pb.ChangeRecord, at time.Time) *platform.Error {
				failure = apply(c, r, at)
				return failure
			}
			action.Rules[i].Transaction = &txn
		}
		record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *platform.Error) {
			return executeAction(c, e, action, s, now)
		})
		if err == nil && failure != nil {
			return nil, failure, true
		}
		return record, err, true
	}
	return nil, nil, false
}

func recordAction(e platform.RecordType, schema string) (platform.ActionType, bool) {
	verb, matches := strings.CutPrefix(schema, e.Type+".")
	if !matches {
		return platform.ActionType{}, false
	}
	if action, found := builtinAction(e, verb); found || e.Lifecycle == nil {
		return action, found
	}
	for _, transition := range e.Lifecycle.Actions {
		if transition.Name == verb {
			return transition, true
		}
		if transition.Approval != nil && transition.Approval.Pending != "" {
			for _, suffix := range []string{platform.ApprovalHeld, platform.ApprovalRejected, platform.ApprovalReturned} {
				if verb == transition.Name+suffix {
					return approvalMove(transition, suffix), true
				}
			}
		}
	}
	return platform.ActionType{}, false
}

func when(f platform.FieldInfo) string {
	if f.When == nil {
		return ""
	}
	return f.When.Field
}

func builtinAction(e platform.RecordType, verb string) (platform.ActionType, bool) {
	if verb == "create" && e.Standard.Create || verb == "edit" && e.Standard.Edit || verb == "archive" && e.Standard.Archive || verb == "restore" && e.Standard.Restore {
		return platform.ActionType{Name: verb}, true
	}
	return platform.ActionType{}, false
}

func approvalMove(t platform.ActionType, suffix string) platform.ActionType {
	from := t.Criteria.State.In
	back := from[0]
	destination := back
	switch suffix {
	case platform.ApprovalHeld:
		destination = t.Approval.Pending
	case platform.ApprovalRejected:
		if t.Approval.Rejected != "" {
			destination = t.Approval.Rejected
		}
	}
	if suffix != platform.ApprovalHeld {
		from = []string{t.Approval.Pending}
	}
	return platform.ActionType{Name: t.Name + suffix, Criteria: platform.ActionCriteria{State: &platform.FieldCondition{Field: t.Criteria.State.Field, In: from}},
		Rules: []platform.ActionRule{{Edit: &platform.PropertyRule{Field: t.Criteria.State.Field, Allowed: []string{destination}}}}}
}

func executeAction(c platform.Caller, e platform.RecordType, t platform.ActionType, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	if !c.HasRuntime() {
		return nil, notFound()
	}
	typ := reflect.TypeOf(e.Model)
	id := s.GetTarget().GetId()
	if t.Name == "create" && id == "" && e.Naming == "" {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	var existing any
	var known bool
	if id != "" {
		existing, known = c.RecordValue(typ, id)
	}
	v := reflect.New(typ)
	if t.Name == "create" {
		if known {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	} else {
		if !known || !inScope(c, e.Type, id) {
			return nil, notFound()
		}
		v.Elem().Set(reflect.ValueOf(existing))
	}
	switch t.Name {
	case "create", "edit", "archive", "restore":
		if t.Name == "archive" || t.Name == "restore" {
			header := v.Elem().Field(0).Addr().Interface().(*platform.Record)
			if t.Name == "restore" {
				if refusal := restoreRefusal(header.Archived); refusal != nil {
					return nil, refusal
				}
			}
			header.Archived = t.Name == "archive"
		} else {
			invalid := &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			var given map[string]json.RawMessage
			if json.Unmarshal(s.GetPayload(), &given) != nil {
				return nil, invalid
			}
			info, _ := records.Describe("", e, func(reflect.Type) string { return "?" })
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
			v = reflect.New(typ)
			if err != nil || json.Unmarshal(raw, v.Interface()) != nil {
				return nil, invalid
			}
			v.Elem().Field(0).Addr().Interface().(*platform.Record).ID = id
			for _, f := range info.Fields {
				on, found := info.Field(when(f))
				if _, set := given[f.Name]; set || !found {
					continue
				}
				if _, moved := given[on.Name]; moved && !f.When.Holds(fmt.Sprint(v.Elem().FieldByIndex(on.Index).Interface()), c.Rules()) {
					v.Elem().FieldByIndex(f.Index).SetZero()
				}
			}
		}
		if c.RestoreSource() {
			v.Elem().Field(0).Addr().Interface().(*platform.Record).Archived = false
		}
	default:
		info, _ := records.Describe("", e, func(reflect.Type) string { return "?" })
		f, _ := info.Field(e.Lifecycle.Field)
		if refusal := transitionRefusal(c, t, e.Type, id, v.Elem().FieldByIndex(f.Index).String(), v.Elem().Field(0).Interface().(platform.Record).Archived, now); refusal != nil {
			return nil, refusal
		}
		for _, rule := range t.Rules {
			if rule.Function != nil {
				if err := rule.Function(c, v.Interface(), s.GetPayload(), now); err != nil {
					return nil, err
				}
			}
			if rule.Edit == nil {
				continue
			}
			field, _ := info.Field(rule.Edit.Field)
			value := v.Elem().FieldByIndex(field.Index)
			original := reflect.ValueOf(existing).FieldByIndex(field.Index).String()
			if to := value.String(); to == original && !slices.Contains(rule.Edit.Allowed, original) {
				value.SetString(rule.Edit.Allowed[0])
			} else if !slices.Contains(rule.Edit.Allowed, to) {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
		}
	}
	if t.Name != "archive" {
		if err := c.PrepareBusinessVersions(v.Interface(), now); err != nil {
			return nil, err
		}
		if e.ComputeAt != nil {
			var previous any
			if t.Name != "create" {
				previous = existing
			}
			if err := e.ComputeAt(c, v.Interface(), previous, now); err != nil {
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
	}
	value := v.Elem().Interface()
	if t.Name != "archive" && (t.Name != "create" || e.CreateTxn == nil || c.Replaying || c.SourceProjection()) {
		if err := c.Check(value); err != nil {
			return nil, err
		}
	}
	if t.Name == "create" && id == "" {
		if c.Probing() {
			id = "_probe"
		} else {
			assigned, err := c.Next(&pb.ChangeRecord{ChangeId: "alloc", Submission: s}, e.Type, now)
			if err != nil {
				return nil, err
			}
			id = assigned
		}
		s.Target.Id = id
		v.Elem().Field(0).Addr().Interface().(*platform.Record).ID = id
		value = v.Elem().Interface()
	}
	return func(r *pb.ChangeRecord) {
		for _, rule := range t.Rules {
			if rule.Transaction != nil {
				rule.Transaction.Run(c, r, now)
				return
			}
		}
		c.Put(r, value)
		if e.Follow != nil && !c.Replaying && !c.SourceProjection() {
			if c.CreationDepth() >= 16 {
				c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Creation transactions exceed their depth limit"))
				return
			}
			created := c.WithTransactionInput(s)
			created = created.Deeper()
			if err := e.Follow(created, r, t.Name, existing, now); err != nil {
				c.FailDecision(err)
				return
			}
			if final, exists := c.RecordValue(typ, id); exists && t.Name == "create" && e.CreateTxn != nil {
				if err := c.Check(final); err != nil {
					c.FailDecision(err)
				}
			}
		}
		for _, rule := range t.Rules {
			if rule.Receipt != nil {
				after := reflect.New(typ)
				after.Elem().Set(reflect.ValueOf(value))
				rule.Receipt(c, r, after.Interface(), now)
			}
		}
		for _, effect := range t.Effects {
			after := reflect.New(typ)
			after.Elem().Set(reflect.ValueOf(value))
			effect(c, r, after.Interface(), now)
		}
	}, nil
}

func inScope(c platform.Caller, typ, id string) bool {
	return c.Replaying || c.Automation || c.Probing() || c.Readable(typ+"/"+id)
}

func (l *authority) ID() string                 { return l.authority }
func (l *authority) Catalog() *platform.Catalog { return l.catalog }

func notFound() *platform.Error { return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND} }
