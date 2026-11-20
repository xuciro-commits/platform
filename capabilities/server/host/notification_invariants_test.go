package host

import (
	"encoding/json"
	"net/http"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
	"platformserver/services/work/activity"
)

func TestInvariantK1NotificationCategoriesSurviveReplay(t *testing.T) {
	const tenant = "invariant-notification-categories"
	const target = "invariantproject.incident/INC-1"
	const follower = "auditor"
	apps := func() []platform.Service {
		return []platform.Service{activity.New(tenant, actions.NewAuthority), work.New(tenant, actions.NewAuthority)}
	}
	seats := []protocol.Seat{
		{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}},
		{Subjects: []string{follower}, Member: platform.Member{ID: follower, Roles: map[string]string{build.ID: ontology.UserRole}}},
	}
	oldNotice := platform.Notification{App: "platform", Key: "legacy", Title: "Existing notification", At: invariantClock}
	live := newInvariantTenantWithApps(t, tenant, apps(), seats...)
	live.Notices.Post(oldNotice, "other")
	live.accept(t, "build.object.create", build.ObjectType, "INCIDENT", map[string]any{
		"name": "incident", "title": "Incident", "access": []ontology.Access{{Role: ontology.UserRole, Read: "all"}},
		"fields": []ontology.Field{{Name: "note", Type: "text", Title: "Note"}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "INCIDENT", nil)
	live.accept(t, "invariantproject.incident.create", "invariantproject.incident", "INC-1", map[string]string{"note": "Open"})
	submit := func(who, app, schema, typ, id string, payload map[string]string) {
		t.Helper()
		member, _ := live.Member(who)
		sub := live.submission(schema, typ, id, payload)
		sub.Authority, sub.PrincipalId = app, who
		if _, err := live.Submit(member, sub, invariantClock); err != nil {
			t.Fatalf("%s: %s", schema, err)
		}
	}
	submit(follower, activity.ID, activity.SchemaFollow, activity.FollowType, protocol.FollowID(follower, target), map[string]string{"target": target})
	submit("builder", activity.ID, activity.SchemaComment, activity.CommentType, "COMMENT-1", map[string]string{"target": target, "text": "Please review @other today"})
	submit("builder", work.ID, work.SchemaTaskCreate, work.TaskType, "TASK-1", map[string]string{"ref": target, "title": "Review incident", "assignee": "other"})
	edited := live.accept(t, "invariantproject.incident.edit", "invariantproject.incident", "INC-1", map[string]string{"note": "Investigating"})

	fresh := newInvariantTenantReplayWithApps(t, tenant, apps(), seats...)
	fresh.Notices.Post(oldNotice, "other")
	if err := fresh.recoverEntries(live.entries); err != nil {
		t.Fatal(err)
	}
	for _, state := range []*invariantTenant{live, fresh} {
		_, handler := observationHost(state.Tenant)
		for member, expected := range map[string]map[string]string{
			"other":  {"legacy": "system", "mention:COMMENT-1": "mention", "task:TASK-1": "assignment"},
			follower: {"comment:COMMENT-1": "mention", "change:build/" + edited.GetChangeId(): "system"},
		} {
			response := observationRequest(t, handler, tenant, observationToken(tenant, member), "/v1/notifications", nil)
			var notices []platform.Notification
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &notices) != nil || len(notices) != len(expected) {
				t.Fatalf("%s: HTTP %d: %s", member, response.Code, response.Body)
			}
			for _, n := range notices {
				if n.Category != expected[n.Key] || n.Member != member {
					t.Fatalf("%s: unexpected notification %+v", member, n)
				}
			}
		}
		saved, _ := state.Notices.State()
		if saved[0].Key != "legacy" || saved[0].Category != "" {
			t.Fatalf("reading rewrote the historical notice: %+v", saved[0])
		}
	}
}
