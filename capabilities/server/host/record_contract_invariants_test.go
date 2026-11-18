package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
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

func TestInvariantK16ReferencesRespectTargetReadPermissions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k16", protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}})
	for _, kind := range []struct {
		name, read string
	}{{"partner", "none"}, {"customer", "all"}} {
		live.accept(t, "build.object.create", build.ObjectType, kind.name, map[string]any{
			"name": kind.name, "title": kind.name, "display": "note", "fields": []ontology.Field{{Name: "note", Type: "text", Title: "Name"}},
			"access": []ontology.Access{{Role: ontology.UserRole, Read: kind.read}},
		})
		live.accept(t, "build.object.publish", build.ObjectType, kind.name, map[string]any{})
		live.accept(t, "build."+kind.name+".create", "build."+kind.name, "TARGET", map[string]any{"note": kind.name + " label"})
	}
	live.accept(t, "build.object.create", build.ObjectType, "ORDER", map[string]any{
		"name": "order", "title": "Order", "access": []ontology.Access{{Role: ontology.UserRole, Read: "all"}},
		"fields": []ontology.Field{{Name: "partner", Type: "reference", Title: "Partner", Ref: "build.partner"}, {Name: "customer", Type: "reference", Title: "Customer", Ref: "build.customer"}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "ORDER", map[string]any{})
	live.accept(t, "build.order.create", "build.order", "ROW", map[string]any{"partner": "TARGET", "customer": "TARGET"})
	_, handler := observationHost(live.Tenant)
	for _, path := range []string{"/v1/records/build.order", "/v1/records/build.order/ROW"} {
		for _, role := range []string{"builder", "other"} {
			response := observationRequest(t, handler, live.ID, observationToken(live.ID, role), path, nil)
			var view struct{ Refs map[string]map[string]string }
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil {
				t.Fatalf("%s %s: HTTP %d: %s", role, path, response.Code, response.Body)
			}
			if view.Refs["customer"]["TARGET"] != "customer label" || role == "builder" && view.Refs["partner"]["TARGET"] != "partner label" || role == "other" && bytes.Contains(response.Body.Bytes(), []byte("partner label")) {
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
