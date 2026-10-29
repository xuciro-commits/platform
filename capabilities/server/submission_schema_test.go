package platformserver

import "platformserver/studio/build"

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/apps/work"
	"platformserver/platform"
)

func TestSubmissionSchemaHTTPRequiresContentBeforeExecutingPublishedObjects(t *testing.T) {
	tenant := builderTenant(t, "schema-http")
	typ := publishObject(t, tenant, "builder", "item", map[string]any{"name": "item", "title": "Item", "fields": []build.Field{
		{Name: "name", Title: "Name", Type: "text"},
		{Name: "private", Title: "Private", Type: "text", Read: []string{build.Builder}, Write: []string{build.Builder}},
	}}, txnAt)
	binding := schemaAction(t, tenant, typ+".create")
	for _, action := range tenant.Catalog(memberOf(t, tenant, "builder")) {
		if action.Schema == typ+".create" && (action.SchemaVersion == nil || *action.SchemaVersion != binding) {
			t.Fatal("member field projection changed the immutable schema identity")
		}
	}
	host := NewHost(Tokens(map[string]string{"user": "user"}), tenant)
	request := submission(tenant, memberOf(t, tenant, "user"), build.ID, typ+".create", typ, "A", map[string]any{"name": "Declared version"})
	post := func(content string) SubmissionAnswer {
		t.Helper()
		raw, err := protojson.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		call := httptest.NewRequest(http.MethodPost, "/v1/submissions", strings.NewReader(string(raw)))
		call.Header.Set("Authorization", "Bearer user")
		call.Header.Set(SchemaVersionHeader, content)
		response := httptest.NewRecorder()
		host.Handler().ServeHTTP(response, call)
		var answer SubmissionAnswer
		if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
		return answer
	}
	if answer := post(""); answer.Error == nil || answer.Error.Code != pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA.String() {
		t.Fatal("published object accepted an undeclared version", answer)
	}
	if tenant.records.types[typ].rows.Get("A") != nil {
		t.Fatal("undeclared version wrote a row")
	}
	request.IdempotencyKey += "-declared"
	if answer := post("broken"); answer.Error == nil || answer.Error.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT.String() {
		t.Fatal("malformed content identity passed admission", answer)
	}
	if answer := post(binding.Version); answer.Error != nil || answer.Record == nil {
		t.Fatal("malformed transport reserved a valid request's key", answer)
	}
	if answer := post(""); answer.Error == nil || answer.Error.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT.String() {
		t.Fatal("same key silently discarded its content declaration", answer)
	}
}

func schemaAction(t *testing.T, tenant *Tenant, name string) platform.SchemaVersion {
	t.Helper()
	for _, action := range tenant.Catalog(memberOf(t, tenant, "user")) {
		if action.Schema == name && action.SchemaVersion != nil {
			return *action.SchemaVersion
		}
	}
	t.Fatalf("action %s has no schema version", name)
	return platform.SchemaVersion{}
}

func TestSubmissionSchemaFamiliesRetainVersionsAndRecover(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			id := fmt.Sprintf("schema-families-%d", time.Now().UnixNano())
			defer backend.cleanup(t, id)
			journal := backend.open(t, id)
			if _, err := journal.Entries(context.Background(), id, 0); err != nil {
				t.Fatal(err)
			}
			persist := func(e Entry, key, hash string) ([]byte, error) {
				return journal.AppendAccepted(context.Background(), id, e, key, hash)
			}
			live := builderTenant(t, id)
			live.AcceptResult = persist
			fields := []build.Field{{Name: "name", Title: "Name", Type: "text"}}
			typ := publishObject(t, live, "builder", "item", map[string]any{"name": "item", "title": "Item", "fields": fields, "states": []build.State{{Name: "open", Title: "Open"}, {Name: "reviewed", Title: "Reviewed"}}, "actions": []build.Action{{Name: "review", Title: "Review", From: []string{"open"}, To: "reviewed"}}}, txnAt)
			original := schemaAction(t, live, typ+".create")
			if original.Check() != nil || original.Family != 1 {
				t.Fatal("initial schema is not declared")
			}
			fields = append(fields, build.Field{Name: "note", Title: "Note", Type: "text"})
			decide(t, live, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, "item", map[string]any{"fields": fields}, txnAt)
			decide(t, live, "builder", build.ID, build.SchemaPublish, build.ObjectType, "item", map[string]any{}, txnAt)
			compatible := schemaAction(t, live, typ+".create")
			if compatible.Version == original.Version || compatible.Family != original.Family {
				t.Fatal("optional field did not retain its family with a new content version")
			}
			request := submission(live, memberOf(t, live, "user"), build.ID, typ+".create", typ, "A", map[string]any{"name": "Old client"})
			receipt, refusal, _ := live.submitSchemaDiagnosed(memberOf(t, live, "user"), request, original.Version, txnAt)
			if refusal != nil {
				t.Fatal(refusal)
			}
			position := journal.Position(id)
			if _, refusal, _ := live.submitSchemaDiagnosed(memberOf(t, live, "user"), request, compatible.Version, txnAt); refusal == nil {
				t.Fatal("same key silently rebound to another schema version")
			}
			if journal.Position(id) != position {
				t.Fatal("schema key conflict wrote a second result")
			}
			decide(t, live, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, "item", map[string]any{"actions": []build.Action{{Name: "review", Title: "Review", From: []string{"open"}, To: "open"}}}, txnAt)
			decide(t, live, "builder", build.ID, build.SchemaPublish, build.ObjectType, "item", map[string]any{}, txnAt)
			current := schemaAction(t, live, typ+".create")
			if current.Family != 2 {
				t.Fatalf("breaking change retained family %d", current.Family)
			}
			oldRequest := submission(live, memberOf(t, live, "user"), build.ID, typ+".create", typ, "B", map[string]any{"name": "Stale client"})
			_, refusal, _ = live.submitSchemaDiagnosed(memberOf(t, live, "user"), oldRequest, compatible.Version, txnAt)
			if refusal == nil || live.records.types[typ].rows.Get("B") != nil || !strings.Contains(refusal.Message, "refresh") {
				t.Fatal("old family executed current rules or omitted its refresh reason", refusal)
			}
			corrected := submission(live, memberOf(t, live, "user"), build.ID, typ+".create", typ, "B", map[string]any{"name": "Stale client"})
			corrected.CorrelationId = "correction:" + oldRequest.IdempotencyKey
			if _, refusal, _ := live.submitSchemaDiagnosed(memberOf(t, live, "user"), corrected, compatible.Version, txnAt); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
				t.Fatal("same inputs and schema created another intent", refusal)
			}
			corrected.Schema.Version = current.Family
			correctedReceipt, refusal, _ := live.submitSchemaDiagnosed(memberOf(t, live, "user"), corrected, current.Version, txnAt)
			if refusal != nil || correctedReceipt == nil || live.records.types[typ].rows.Get("B") == nil {
				t.Fatal("refreshed schema could not correct unchanged inputs", refusal)
			}
			freshRequest := submission(live, memberOf(t, live, "user"), build.ID, typ+".create", typ, "C", map[string]any{"name": "New client", "note": "V2"})
			freshRequest.Schema.Version = current.Family
			if _, refusal, _ := live.submitSchemaDiagnosed(memberOf(t, live, "user"), freshRequest, current.Version, txnAt); refusal != nil {
				t.Fatal(refusal)
			}
			retiredRequest := submission(live, memberOf(t, live, "user"), build.ID, typ+".review", typ, "A", map[string]any{})
			retiredRequest.Schema.Version = current.Family
			retiredReceipt, refusal, _ := live.submitSchemaDiagnosed(memberOf(t, live, "user"), retiredRequest, current.Version, txnAt)
			if refusal != nil {
				t.Fatal(refusal)
			}
			decide(t, live, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, "item", map[string]any{"actions": []build.Action{}}, txnAt)
			decide(t, live, "builder", build.ID, build.SchemaPublish, build.ObjectType, "item", map[string]any{}, txnAt)
			journal.Close()
			journal = backend.reopen(t, id)
			defer journal.Close()
			entries, err := journal.Entries(context.Background(), id, 0)
			if err != nil {
				t.Fatal(err)
			}
			recovered := builderTenant(t, id)
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			recovered.AcceptResult = persist
			position = journal.Position(id)
			again, refusal, _ := recovered.submitSchemaDiagnosed(memberOf(t, recovered, "user"), request, original.Version, txnAt.Add(time.Hour))
			if refusal != nil || again.GetChangeId() != receipt.GetChangeId() || journal.Position(id) != position {
				t.Fatal("recovery reexecuted or refused a confirmed old-family request", refusal)
			}
			if _, refusal, _ := recovered.submitSchemaDiagnosed(memberOf(t, recovered, "user"), oldRequest, compatible.Version, txnAt.Add(time.Hour)); refusal == nil || journal.Position(id) != position {
				t.Fatal("refusal retry lost its original schema binding")
			}
			again, refusal, _ = recovered.submitSchemaDiagnosed(memberOf(t, recovered, "user"), corrected, current.Version, txnAt.Add(time.Hour))
			if refusal != nil || again.GetChangeId() != correctedReceipt.GetChangeId() || journal.Position(id) != position {
				t.Fatal("recovery lost the corrected request's schema identity", refusal)
			}
			again, refusal, _ = recovered.submitSchemaDiagnosed(memberOf(t, recovered, "user"), retiredRequest, current.Version, txnAt.Add(time.Hour))
			if refusal != nil || again.GetChangeId() != retiredReceipt.GetChangeId() || journal.Position(id) != position {
				t.Fatal("retirement lost an original action receipt", refusal)
			}
			snapshot, _, err := recovered.Snapshot(func() int64 { return position })
			if err != nil {
				t.Fatal(err)
			}
			restored := builderTenant(t, id)
			if err := restored.Restore(snapshot); err != nil {
				t.Fatal(err)
			}
			if _, refusal, _ := restored.submitSchemaDiagnosed(memberOf(t, restored, "user"), freshRequest, current.Version, txnAt); refusal != nil {
				t.Fatal("snapshot lost accepted schema identity", refusal)
			}
			var invalid tenantState
			if err := json.Unmarshal(snapshot, &invalid); err != nil {
				t.Fatal(err)
			}
			wrong := current
			wrong.Family++
			invalid.SubmissionSchemas[build.ID+"/"+freshRequest.GetIdempotencyKey()] = wrong
			if err := builderTenant(t, id).Restore(platform.Raw(invalid)); err == nil {
				t.Fatal("snapshot accepted a schema binding that differs from its receipt")
			}
		})
	}
}

func TestSubmissionSchemaApprovalSnapshotPreservesTheOriginalRequest(t *testing.T) {
	live := builderTenant(t, "schema-approval", work.New("schema-approval"))
	var entries []Entry
	live.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return append([]byte(nil), entry.Body...), nil
	}
	typ := publishObject(t, live, "builder", "item", map[string]any{"name": "item", "title": "Item",
		"fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}},
		"states": []build.State{{Name: "open", Title: "Open"}, {Name: "pending", Title: "Pending"}, {Name: "reviewed", Title: "Reviewed"}},
		"actions": []build.Action{{Name: "review", Title: "Review", From: []string{"open"}, To: "reviewed",
			Approval: &build.ActionApproval{Pending: "pending", Levels: []build.ApproverLevel{{Title: "Reviewer", Role: build.Builder}}}}},
	}, txnAt)
	decide(t, live, "user", build.ID, typ+".create", typ, "A", map[string]any{"name": "Approval request"}, txnAt)
	request := submission(live, memberOf(t, live, "user"), build.ID, typ+".review", typ, "A", map[string]any{})
	binding := schemaAction(t, live, typ+".review")
	receipt, refusal, _ := live.submitSchemaDiagnosed(memberOf(t, live, "user"), request, binding.Version, txnAt)
	if refusal != nil || receipt.GetSubmission().GetAuthority() != work.ID {
		t.Fatal("approval did not produce its original Work receipt", refusal)
	}
	snapshot, _, err := live.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"snapshot", "replay"} {
		t.Run(mode, func(t *testing.T) {
			restored := builderTenant(t, live.ID, work.New(live.ID))
			var err error
			if mode == "snapshot" {
				err = restored.Restore(snapshot)
			} else {
				err = restored.Replay(entries)
			}
			if err != nil {
				t.Fatal(err)
			}
			again, refusal, _ := restored.submitSchemaDiagnosed(memberOf(t, restored, "user"), request, binding.Version, txnAt.Add(time.Hour))
			if refusal != nil || again.GetChangeId() != receipt.GetChangeId() {
				t.Fatal("held request rebound to its child receipt or ran twice", refusal)
			}
		})
	}
}
