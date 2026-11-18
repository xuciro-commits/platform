package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	actioncatalog "platformserver/services/actions/catalog"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func TestInvariantK14PublishedFieldPlacementPreservesActions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k14")
	live.accept(t, "build.object.create", build.ObjectType, "OLD", invariantObject("old", "Old"))
	live.accept(t, "build.object.publish", build.ObjectType, "OLD", nil)
	old, _ := live.Entity("build.old")
	note, _ := old.Field("note")
	if note.Group != "" || note.Aside {
		t.Fatalf("old field gained placement: %+v", note)
	}
	live.accept(t, "build.object.create", build.ObjectType, "PLACED", map[string]any{
		"name": "placed", "title": "Placed", "fields": []map[string]any{
			{"name": "note", "title": "Note", "type": "text", "group": `Sales "East"`},
			{"name": "owner", "title": "Owner", "type": "text", "aside": true, "required": true},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "PLACED", nil)
	_, handler := observationHost(live.Tenant)
	response := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/entities", nil)
	var entities []platform.RecordTypeInfo
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &entities) != nil {
		t.Fatalf("entities: HTTP %d: %s", response.Code, response.Body)
	}
	index := slices.IndexFunc(entities, func(info platform.RecordTypeInfo) bool { return info.Type == "build.placed" })
	if index < 0 {
		t.Fatal("build.placed is absent from entities")
	}
	placed := entities[index]
	note, _ = placed.Field("note")
	owner, _ := placed.Field("owner")
	if note.Group != `Sales "East"` || !owner.Aside {
		t.Fatalf("published field placement: group=%q aside=%t", note.Group, owner.Aside)
	}
	live.accept(t, "build.placed.create", "build.placed", "ROW", map[string]any{"owner": "First"})
	live.accept(t, "build.placed.edit", "build.placed", "ROW", map[string]any{"owner": "Second"})
}

func TestInvariantK15RecordActionAvailabilityUsesTheSubmissionDecision(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k15", protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}})
	object := invariantObject("workitem", "Work item")
	object["states"] = []ontology.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}
	object["access"] = []ontology.Access{{Role: ontology.UserRole, Read: "all"}}
	completion := platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("done")}}
	guard := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{{Ref: "note", Kind: "text"}, {Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("ready")}}}}
	const typ, schema = "build.workitem", "build.workitem.complete"
	action := ontology.Action{Name: "complete", Title: "Complete", From: []string{"open"}, Roles: []string{ontology.BuilderRole},
		Transaction: &platform.BusinessTxn{Subject: typ, Guard: &guard, Message: "The item must be ready", Steps: []platform.TxnStep{{
			Name: "complete", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: &completion}},
		}}}}
	live.accept(t, "build.object.create", build.ObjectType, "ITEM", object)
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "ACTION", map[string]any{"name": schema, "object": typ, "definition": action})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "ACTION", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "ITEM", map[string]any{})
	change := live.accept(t, typ+".create", typ, "WORK", map[string]any{"note": "waiting"})
	_, handler := observationHost(live.Tenant)
	get := func(role string) (objects.RecordAction, bool) {
		t.Helper()
		response := observationRequest(t, handler, live.ID, observationToken(live.ID, role), "/v1/records/"+typ+"/WORK", nil)
		var view objects.RecordView
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil {
			t.Fatalf("%s record: HTTP %d: %s", role, response.Code, response.Body)
		}
		for _, result := range view.Actions {
			if result.Schema == schema {
				return result, true
			}
		}
		return objects.RecordAction{}, false
	}
	check := func(revision uint32, allowed bool, reason string) {
		t.Helper()
		item, exists := get("builder")
		if !exists || item.Allowed != allowed || !strings.Contains(item.Reason, reason) {
			t.Fatalf("availability: %+v, exists=%t, want allowed=%t, reason %q", item, exists, allowed, reason)
		}
		if _, exists := get("other"); exists {
			t.Fatal("a member with read access only received the transition")
		}
		sub := live.submission(schema, typ, "WORK", map[string]any{})
		sub.ExpectedRevision = &revision
		_, refusal := live.Submit(live.member(t), sub, invariantClock)
		if allowed && refusal != nil || !allowed && (refusal == nil || refusal.Message != item.Reason) {
			t.Fatalf("submission differs from availability %+v: %+v", item, refusal)
		}
	}
	check(change.GetRevision(), false, "The item must be ready")
	change = live.accept(t, typ+".edit", typ, "WORK", map[string]any{"note": "ready"})
	check(change.GetRevision(), true, "")
	check(change.GetRevision()+1, false, "takes it only from open")
}

func checkRecordAction(t *testing.T, handler http.Handler, tenant, role, schema, typ, id string) (objects.RecordAction, actions.ActionCheckResult) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/actions/"+schema+"/check", bytes.NewReader(platform.Raw(map[string]any{
		"target": map[string]string{"object": typ, "id": id},
		"inputs": map[string]any{},
	})))
	req.Header.Set("Authorization", "Bearer "+observationToken(tenant, role))
	req.Header.Set(TenantHeader, tenant)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	var checked actions.ActionCheckResult
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &checked) != nil {
		t.Fatalf("check %s: HTTP %d %s", schema, res.Code, res.Body)
	}
	res = observationRequest(t, handler, tenant, observationToken(tenant, role), "/v1/records/"+typ+"/"+id, nil)
	var record objects.RecordView
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &record) != nil {
		t.Fatalf("read %s/%s: HTTP %d %s", typ, id, res.Code, res.Body)
	}
	for _, action := range record.Actions {
		if action.Schema == schema {
			return action, checked
		}
	}
	return objects.RecordAction{}, checked
}

func TestInvariantK15EmptyGuardsAndPendingApprovalAgreeAcrossReads(t *testing.T) {
	const tenant, typ, id = "invariant-action-availability", "build.reviewable", "ITEM"
	live := newInvariantTenantWithApps(t, tenant, []platform.Service{work.New(tenant, actions.NewAuthority)},
		protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}},
		protocol.Seat{Subjects: []string{"reviewer"}, Member: platform.Member{ID: "reviewer", Roles: map[string]string{build.ID: ontology.BuilderRole}}},
	)
	obj := invariantObject("reviewable", "Reviewable")
	obj["fields"] = []ontology.Field{{Name: "note", Type: "text", Title: "Note"}, {Name: "private", Type: "text", Title: "Private", Read: []string{ontology.BuilderRole}}}
	obj["access"] = []ontology.Access{{Role: ontology.UserRole, Read: "all"}}
	obj["states"] = []ontology.State{{Name: "open", Title: "Open"}, {Name: "pending", Title: "Pending"}, {Name: "done", Title: "Done"}}
	live.accept(t, "build.object.create", build.ObjectType, "TYPE", obj)
	done := platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("done")}}
	note := platform.ExprReference("text", "note")
	for _, tc := range []struct {
		name     string
		guard    *platform.Expr
		approval *ontology.ActionApproval
	}{
		{"nonblocked", &platform.Expr{Op: "!=", Kind: "boolean", Args: []platform.Expr{note, {Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("blocked")}}}}, nil},
		{"empty", &platform.Expr{Op: "empty", Kind: "boolean", Args: []platform.Expr{note}}, nil},
		{"hidden", &platform.Expr{Op: "empty", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "private")}}, nil},
		{"review", nil, &ontology.ActionApproval{Pending: "pending", Levels: []ontology.ApproverLevel{{Title: "Review", Role: ontology.BuilderRole}}}},
	} {
		action := ontology.Action{Name: tc.name, Title: tc.name, From: []string{"open"}, Approval: tc.approval,
			Transaction: &platform.BusinessTxn{Subject: typ, Guard: tc.guard, Message: "The note is required",
				Steps: []platform.TxnStep{{Name: "finish", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: &done}}}}},
		}
		live.accept(t, "build.actiontype.create", build.ActionTypeType, tc.name, map[string]any{"name": typ + "." + tc.name, "object": typ, "definition": action})
		live.accept(t, "build.actiontype.publish", build.ActionTypeType, tc.name, map[string]any{})
	}
	live.accept(t, "build.object.publish", build.ObjectType, "TYPE", map[string]any{})
	_, handler := observationHost(live.Tenant)
	live.accept(t, typ+".create", typ, "HIDDEN", map[string]any{"private": "protected value"})
	hidden, checked := checkRecordAction(t, handler, tenant, "other", typ+".hidden", typ, "HIDDEN")
	user, _ := live.Member("other")
	sub := live.submission(typ+".hidden", typ, "HIDDEN", map[string]any{})
	sub.PrincipalId = user.ID
	_, denial := live.Submit(user, sub, invariantClock)
	if hidden.Schema != "" || checked.Allowed || checked.Authorization != nil || checked.Message != "" || denial == nil || denial.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("hidden guard: read=%+v check=%+v submit=%+v", hidden, checked, denial)
	}
	for _, name := range []string{"nonblocked", "empty", "review"} {
		row := id + "-" + name
		live.accept(t, typ+".create", typ, row, map[string]any{})
		schema := typ + "." + name
		item, check := checkRecordAction(t, handler, tenant, "builder", schema, typ, row)
		_, refusal := live.Submit(live.member(t), live.submission(schema, typ, row, map[string]any{}), invariantClock)
		if item.Schema != schema || item.Allowed != (refusal == nil) || check.Allowed != item.Allowed || check.Message != item.Reason || refusal != nil && refusal.Message != item.Reason {
			t.Fatalf("%s: read=%+v check=%+v submit=%+v", name, item, check, refusal)
		}
		if name != "review" {
			continue
		}
		for _, schema := range []string{schema, schema + ".held", schema + ".rejected", schema + ".returned"} {
			item, check := checkRecordAction(t, handler, tenant, "builder", schema, typ, row)
			_, refusal := live.Submit(live.member(t), live.submission(schema, typ, row, map[string]any{}), invariantClock)
			if item.Allowed || check.Allowed || refusal == nil || item.Schema == schema && (check.Message != item.Reason || refusal.Message != item.Reason) || item.Schema == "" && (check.Authorization != nil || check.Message != "") {
				t.Fatalf("pending %s: read=%+v check=%+v submit=%+v", schema, item, check, refusal)
			}
		}
	}
}

type stateVisibilityRecord struct {
	platform.Record
	Title string `json:"title,omitempty"`
	State string `json:"state" field:"readonly" choices:"open,pending,done" read:"writer"`
}

type stateVisibilityOwner struct{ ledger platform.Authority }

func (a stateVisibilityOwner) Authority() platform.Authority { return a.ledger }
func (a stateVisibilityOwner) Fork(ledger platform.Authority) (platform.Service, error) {
	return stateVisibilityOwner{ledger}, nil
}
func (a stateVisibilityOwner) Declarations() []*pb.AuthorityDeclaration {
	return a.ledger.Declarations()
}
func (a stateVisibilityOwner) SnapshotFormat() string             { return "platform.ledger.v1" }
func (a stateVisibilityOwner) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }
func (a stateVisibilityOwner) Restore(raw json.RawMessage) error  { return a.ledger.Restore(raw) }
func (a stateVisibilityOwner) Manifest() platform.Manifest {
	return platform.Manifest{ID: "statevisibility", Title: "State visibility", Version: "1", Roles: []string{"reader", "writer"}, Actions: a.ledger.Catalog(), Entities: []platform.RecordType{stateVisibilityType()}}
}
func (a stateVisibilityOwner) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error) {
	r, err, _ := a.ledger.Generated(c, s, at, nil, stateVisibilityType())
	return r, err
}

func stateVisibilityType() platform.RecordType {
	return platform.RecordType{Type: "statevisibility.item", Title: "Item", Model: stateVisibilityRecord{}, Standard: platform.Standard{Create: true, Roles: []string{"writer"}}, Lifecycle: &platform.Lifecycle{
		Field: "state", Initial: "open", States: []platform.State{{Name: "open"}, {Name: "pending"}, {Name: "done"}},
		Actions: []platform.ActionType{{Name: "finish", Title: "Finish", Roles: []string{"reader", "writer"}, Approval: &platform.Approval{Pending: "pending"},
			Criteria: platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"open"}}},
			Rules:    []platform.ActionRule{{Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"done"}}}}}},
	}}
}

func TestInvariantK15HiddenStateAndApprovalMovesUseTheSameChecks(t *testing.T) {
	const tenant, typ = "invariant-state-visibility", "statevisibility.item"
	catalog := actioncatalog.EntityActions(stateVisibilityType())
	for i := range catalog {
		if catalog[i].Automation {
			catalog[i].Roles = []string{"writer"}
		}
	}
	live := newInvariantTenantWithApps(t, tenant, []platform.Service{stateVisibilityOwner{actions.NewAuthority(tenant, "statevisibility", platform.NewCatalog(catalog...), typ)}},
		protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"statevisibility": "reader"}}},
		protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{"statevisibility": "writer"}}},
	)
	submit := func(who, verb string) *platform.Error {
		t.Helper()
		m, _ := live.Member(who)
		sub := live.submission(typ+"."+verb, typ, "ITEM", map[string]any{})
		sub.Authority, sub.PrincipalId = "statevisibility", who
		_, err := live.Submit(m, sub, invariantClock)
		return err
	}
	if refusal := submit("other", "create"); refusal != nil {
		t.Fatal(refusal)
	}
	_, handler := observationHost(live.Tenant)
	hidden, checked := checkRecordAction(t, handler, tenant, "admin", typ+".finish", typ, "ITEM")
	refusal := submit("admin", "finish")
	if hidden.Schema != "" || checked.Allowed || checked.Authorization != nil || checked.Message != "" || refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("hidden state: read=%+v check=%+v submit=%+v", hidden, checked, refusal)
	}
	for _, step := range []struct {
		verb    string
		allowed bool
	}{{"finish.rejected", false}, {"finish.returned", false}, {"finish.held", true}, {"finish.held", false}, {"finish.returned", true}, {"finish.held", true}, {"finish.rejected", true}} {
		item, checked := checkRecordAction(t, handler, tenant, "other", typ+"."+step.verb, typ, "ITEM")
		refusal := submit("other", step.verb)
		if item.Schema == "" || item.Allowed != step.allowed || checked.Allowed != step.allowed || item.Reason != checked.Message || (refusal == nil) != step.allowed || refusal != nil && item.Reason != refusal.Message {
			t.Fatalf("%s: read=%+v check=%+v submit=%+v", step.verb, item, checked, refusal)
		}
	}
}

func TestInvariantK16ReferencesRespectTargetReadPermissions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k16", protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}})
	for _, kind := range []struct {
		name, read string
		fieldRoles []string
	}{{"partner", "none", nil}, {"customer", "all", nil}, {"private", "all", []string{ontology.BuilderRole}}, {"scoped", "own", nil}} {
		live.accept(t, "build.object.create", build.ObjectType, kind.name, map[string]any{
			"name": kind.name, "title": kind.name, "display": "note", "fields": []ontology.Field{{Name: "note", Type: "text", Title: "Name", Read: kind.fieldRoles}},
			"access": []ontology.Access{{Role: ontology.UserRole, Read: kind.read}},
		})
		live.accept(t, "build.object.publish", build.ObjectType, kind.name, map[string]any{})
		live.accept(t, "build."+kind.name+".create", "build."+kind.name, "TARGET", map[string]any{"note": kind.name + " label"})
	}
	live.accept(t, "build.object.create", build.ObjectType, "ORDER", map[string]any{
		"name": "order", "title": "Order", "access": []ontology.Access{{Role: ontology.UserRole, Read: "all"}},
		"fields": []ontology.Field{{Name: "partner", Type: "reference", Title: "Partner", Ref: "build.partner"}, {Name: "customer", Type: "reference", Title: "Customer", Ref: "build.customer"}, {Name: "hidden", Type: "reference", Title: "Hidden", Ref: "build.customer", Read: []string{ontology.BuilderRole}}, {Name: "private", Type: "reference", Title: "Private display", Ref: "build.private"}, {Name: "scoped", Type: "reference", Title: "Scoped row", Ref: "build.scoped"}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "ORDER", map[string]any{})
	live.accept(t, "build.order.create", "build.order", "ROW", map[string]any{"partner": "TARGET", "customer": "TARGET", "hidden": "TARGET", "private": "TARGET", "scoped": "TARGET"})
	_, handler := observationHost(live.Tenant)
	for _, path := range []string{"/v1/records/build.order", "/v1/records/build.order/ROW"} {
		for _, role := range []string{"builder", "other"} {
			response := observationRequest(t, handler, live.ID, observationToken(live.ID, role), path, nil)
			var view struct{ Refs map[string]map[string]string }
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil {
				t.Fatalf("%s %s: HTTP %d: %s", role, path, response.Code, response.Body)
			}
			if view.Refs["customer"]["TARGET"] != "customer label" || role == "builder" && view.Refs["partner"]["TARGET"] != "partner label" || role == "other" && (view.Refs["hidden"] != nil || view.Refs["private"] != nil || view.Refs["scoped"] != nil || bytes.Contains(response.Body.Bytes(), []byte("partner label")) || bytes.Contains(response.Body.Bytes(), []byte("private label")) || bytes.Contains(response.Body.Bytes(), []byte("scoped label"))) {
				t.Fatalf("%s %s: unexpected labels: %s", role, path, response.Body)
			}
		}
	}
}

func TestInvariantK3RevisionConflictIsLocalizedAndAuthorized(t *testing.T) {
	live := newObservationTenant(t, "invariant-k3-wire")
	change := live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("revisionprobe", "Object"))
	_, handler := observationHost(live.Tenant)
	for _, role := range []string{"builder", "other"} {
		sub := live.submission("build.object.edit", build.ObjectType, "OBJECT", invariantObject("revisionprobe", "Updated"))
		sub.PrincipalId, sub.ExpectedRevision = role, new(uint32)
		raw, err := protojson.Marshal(sub)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+observationToken(live.ID, role))
		request.Header.Set(TenantHeader, live.ID)
		request.Header.Set("Accept-Language", "zh-CN")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var answer SubmissionAnswer
		if json.Unmarshal(response.Body.Bytes(), &answer) != nil || answer.Error == nil {
			t.Fatalf("%s: HTTP %d: %s", role, response.Code, response.Body)
		}
		e := answer.Error
		if role == "builder" && (response.Code != http.StatusConflict || e.Code != pb.ErrorCode_ERROR_CODE_CONFLICT.String() || e.ReasonCode != "stale_revision" || e.CurrentRevision == nil || *e.CurrentRevision != change.GetRevision() || e.Message != "这条记录在你打开后已更改") {
			t.Fatalf("authorized revision conflict: %s", response.Body)
		}
		if role == "other" && (response.Code != http.StatusForbidden || e.ReasonCode != "" || e.CurrentRevision != nil) {
			t.Fatalf("unauthorized revision disclosure: HTTP %d %s", response.Code, response.Body)
		}
	}
}

func TestInvariantK17FormatsUseOnlyReadableFields(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k17", protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}})
	equals := func(field, value string) platform.Expr {
		return platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", field), {Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw(value)}}}}
	}
	live.accept(t, "build.object.create", build.ObjectType, "OPPORTUNITY", map[string]any{
		"name": "opportunity", "title": "Opportunity", "access": []ontology.Access{{Role: ontology.UserRole, Read: "all"}},
		"fields": []ontology.Field{
			{Name: "stage", Title: "Stage", Type: "choice", Choices: "proposal,negotiation", Format: []platform.FormatRule{{When: equals("stage", "negotiation"), Tone: "warning"}}},
			{Name: "note", Title: "Note", Type: "text", Format: []platform.FormatRule{{When: equals("private", "flagged"), Tone: "danger"}}},
			{Name: "private", Title: "Private", Type: "text", Read: []string{ontology.BuilderRole}, Format: []platform.FormatRule{{When: equals("stage", "negotiation"), Tone: "warning"}}},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "OPPORTUNITY", nil)
	live.accept(t, "build.opportunity.create", "build.opportunity", "OPP-001", map[string]any{"stage": "negotiation", "private": "flagged"})
	for _, role := range []string{"builder", "other"} {
		member, _ := live.Member(role)
		page, listErr := live.Records(t.Context(), member, "build.opportunity", platform.Query{}, invariantClock)
		detail, singleErr := live.RecordOf(t.Context(), member, "build.opportunity", "OPP-001", invariantClock)
		formats := page.Formats["OPP-001"]
		if listErr != nil || singleErr != nil || string(platform.Raw(formats)) != string(platform.Raw(detail.Formats)) || formats["stage"].Tone != "warning" {
			t.Fatalf("%s: list=%+v (%v), detail=%+v (%v)", role, formats, listErr, detail.Formats, singleErr)
		}
		if role == "other" && len(formats) != 1 || role == "builder" && len(formats) != 3 {
			t.Fatalf("%s received formats outside its readable fields: %+v", role, formats)
		}
	}
}

func TestInvariantK18UndoUsesAnAcceptedActionWithTheOriginalRevision(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k18")
	const typ = "build.opportunity"
	object := invariantObject("opportunity", "Opportunity")
	object["states"] = []ontology.State{{Name: "open", Title: "Open"}, {Name: "won", Title: "Won"}}
	live.accept(t, "build.object.create", build.ObjectType, "OPPORTUNITY", object)
	for _, move := range []struct{ name, from, to string }{{"win", "open", "won"}, {"reopen", "won", "open"}} {
		state := platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw(move.to)}}
		step := platform.TxnStep{Name: "move", Kind: "update", Target: platform.TxnRef{Type: typ, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: &state}}}
		action := ontology.Action{Name: move.name, Title: move.name, From: []string{move.from}, Transaction: &platform.BusinessTxn{Subject: typ, Steps: []platform.TxnStep{step}}}
		live.accept(t, "build.actiontype.create", build.ActionTypeType, move.name, map[string]any{"name": typ + "." + move.name, "object": typ, "definition": action})
		live.accept(t, "build.actiontype.publish", build.ActionTypeType, move.name, map[string]any{})
	}
	live.accept(t, "build.object.publish", build.ObjectType, "OPPORTUNITY", nil)
	live.accept(t, typ+".create", typ, "OPP-001", map[string]any{"note": "first"})
	_, handler := observationHost(live.Tenant)
	res := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), "/v1/actions", nil)
	var catalog []platform.Action
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &catalog) != nil {
		t.Fatalf("catalog: HTTP %d %s", res.Code, res.Body)
	}
	forward := map[string]string{}
	for _, action := range catalog {
		forward[action.Schema] = action.Reverse
	}
	if forward[typ+".archive"] != typ+".restore" || forward[typ+".restore"] != typ+".archive" || forward[typ+".win"] != typ+".reopen" || forward[typ+".reopen"] != typ+".win" || forward["build.object.publish"] != "" {
		t.Fatalf("invalid reverse declarations: archive=%q restore=%q win=%q reopen=%q publish=%q", forward[typ+".archive"], forward[typ+".restore"], forward[typ+".win"], forward[typ+".reopen"], forward["build.object.publish"])
	}
	archived := live.accept(t, typ+".archive", typ, "OPP-001", map[string]any{})
	item, checked := checkRecordAction(t, handler, live.ID, "builder", typ+".restore", typ, "OPP-001")
	if !item.Allowed || !checked.Allowed {
		t.Fatalf("restore: read=%+v check=%+v", item, checked)
	}
	live.accept(t, typ+".edit", typ, "OPP-001", map[string]any{"note": "changed while archived"})
	sub := live.submission(typ+".restore", typ, "OPP-001", map[string]any{})
	stale := archived.GetRevision()
	sub.ExpectedRevision = &stale
	if _, refusal := live.Submit(live.member(t), sub, invariantClock); refusal == nil || refusal.CurrentRevision == nil || *refusal.CurrentRevision != stale+1 {
		t.Fatalf("stale undo: %+v", refusal)
	}
	live.accept(t, typ+".restore", typ, "OPP-001", map[string]any{})
	view, err := live.Records(t.Context(), live.member(t), typ, platform.Query{}, invariantClock)
	if err != nil || len(view.Records) != 1 || !strings.Contains(string(platform.Raw(view.Records[0])), "changed while archived") {
		t.Fatalf("record was not restored with its accepted edits: %+v %v", view, err)
	}
}

func TestInvariantK19InverseSetTotalsOnlyUseReadableRowsAndFields(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k19", protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}})
	const parent, child = "build.purchase", "build.purchaseline"
	live.accept(t, "build.object.create", build.ObjectType, "PARENT", map[string]any{
		"name": "purchase", "title": "Purchase", "access": []ontology.Access{{Role: ontology.UserRole, Read: "all"}},
		"fields": []ontology.Field{{Name: "name", Type: "text", Title: "Name"}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "PARENT", nil)
	live.accept(t, "build.object.create", build.ObjectType, "CHILD", map[string]any{
		"name": "purchaseline", "title": "Purchase line", "access": []ontology.Access{{Role: ontology.UserRole, Read: "own", Create: true}},
		"fields": []ontology.Field{
			{Name: "purchase", Type: "reference", Title: "Purchase", Ref: parent, Inverse: "lines", Required: true},
			{Name: "amount", Type: "decimal", Title: "Amount"},
			{Name: "private", Type: "integer", Title: "Private", Read: []string{ontology.BuilderRole}},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "CHILD", nil)
	sub := live.submission("build.object.edit", build.ObjectType, "PARENT", map[string]any{"sets": []platform.RecordSet{{Name: "lines", From: "lines", Direction: "inverse", Object: child, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}, Limit: 25}}})
	if _, refusal := live.Submit(live.member(t), sub, invariantClock); refusal != nil {
		t.Fatal(refusal.Message)
	}
	live.accept(t, "build.object.publish", build.ObjectType, "PARENT", nil)
	live.accept(t, parent+".create", parent, "ORDER", map[string]any{"name": "Order"})
	live.accept(t, child+".create", child, "RESTRICTED", map[string]any{"purchase": "ORDER", "amount": 999, "private": 23})
	user, _ := live.Member("other")
	request := live.submission(child+".create", child, "VISIBLE", map[string]any{"purchase": "ORDER", "amount": 12.5})
	request.PrincipalId = user.ID
	_, refusal := live.Submit(user, request, invariantClock)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	latest := live.accept(t, child+".edit", child, "VISIBLE", map[string]any{"private": 2})
	_, handler := observationHost(live.Tenant)
	for _, sample := range []struct {
		role   string
		total  int
		amount string
	}{{"builder", 2, "1011.5"}, {"other", 1, "12.5"}} {
		res := observationRequest(t, handler, live.ID, observationToken(live.ID, sample.role), "/v1/record-sets/"+parent+"/ORDER/lines", nil)
		var page objects.RecordSetResult
		if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &page) != nil || page.Total == nil || *page.Total != sample.total || len(page.Records) != sample.total || string(page.Sums["amount"]) != sample.amount {
			t.Fatalf("%s lines: HTTP %d %s", sample.role, res.Code, res.Body)
		}
		if sample.role == "other" {
			var line platform.Record
			if json.Unmarshal(page.Records[0], &line) != nil || line.ID != "VISIBLE" || line.Revision != latest.GetRevision() || page.Sums["private"] != nil || bytes.Contains(res.Body.Bytes(), []byte("RESTRICTED")) {
				t.Fatalf("restricted lines response: %s", res.Body)
			}
		}
	}
}

func TestInvariantK20PublishedDefaultViewPreservesTheRecordContract(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k20")
	live.accept(t, "build.object.create", build.ObjectType, "LEGACY", invariantObject("legacyview", "Legacy"))
	live.accept(t, "build.object.publish", build.ObjectType, "LEGACY", nil)
	object := invariantObject("preferred", "Preferred")
	live.accept(t, "build.object.create", build.ObjectType, "TYPE", object)
	live.accept(t, "build.object.publish", build.ObjectType, "TYPE", nil)
	_, handler := observationHost(live.Tenant)
	read := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
		request.Header.Set(TenantHeader, live.ID)
		request.Header.Set("Accept-Language", "zh-CN")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d %s", path, response.Code, response.Body)
		}
		return response
	}
	schemaHistory := func() json.RawMessage {
		var reply struct {
			Record struct{ SchemaVersions json.RawMessage } `json:"record"`
		}
		if json.Unmarshal(read("/v1/records/build.object/TYPE").Body.Bytes(), &reply) != nil {
			t.Fatal("invalid object draft response")
		}
		return reply.Record.SchemaVersions
	}
	before := schemaHistory()
	for _, invalid := range []string{`{}`, `[]`, `null`} {
		sub := live.submission("build.object.edit", build.ObjectType, "TYPE", map[string]any{"view": json.RawMessage(invalid)})
		if _, refusal := live.Submit(live.member(t), sub, invariantClock); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
			t.Fatalf("invalid view %s survived: %+v", invalid, refusal)
		}
	}
	view := json.RawMessage(`{"columns":["note"],"group":"note","layout":"table","title":"Draft","version":1}`)
	live.accept(t, "build.object.edit", build.ObjectType, "TYPE", map[string]any{"view": view})
	live.accept(t, "build.object.publish", build.ObjectType, "TYPE", nil)
	var catalog []platform.RecordTypeInfo
	if json.Unmarshal(read("/v1/entities").Body.Bytes(), &catalog) != nil {
		t.Fatal("invalid type catalog response")
	}
	views := map[string]json.RawMessage{}
	for _, metadata := range catalog {
		views[metadata.Type] = metadata.View
	}
	if len(views["build.legacyview"]) != 0 || !bytes.Equal(views["build.preferred"], view) {
		t.Fatalf("published views: legacy=%s preferred=%s", views["build.legacyview"], views["build.preferred"])
	}
	if len(before) == 0 || !bytes.Equal(before, schemaHistory()) {
		t.Fatal("publishing only a default view changed the action schema history")
	}
}
