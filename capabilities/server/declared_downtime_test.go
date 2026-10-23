package platformserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/apps/enterprise"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestDeclaredDowntimeUsesObservationFlowOrganizationalWorkAndHumanReview(t *testing.T) {
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	const id = "declared-downtime"
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("planner", "build:supervisor"), seatOf("other", "build:supervisor"), seatOf("operator", "build:operator"), seatOf("elsewhere", "build:operator")}
	organization := platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}}, Units: []platform.Unit{{ID: "plant", Name: "Plant", Kind: "plant"}, {ID: "L1", Name: "Line one", Kind: "line"}, {ID: "L2", Name: "Line two", Kind: "line"}}, Edges: []platform.Edge{{Structure: "site", Unit: "L1", Parent: "plant"}, {Structure: "site", Unit: "L2", Parent: "plant"}}, Memberships: []platform.Membership{{Party: "member:planner", Unit: "L1", Role: "supervisor"}, {Party: "member:other", Unit: "L2", Role: "supervisor"}, {Party: "member:operator", Unit: "L1", Role: "operator"}, {Party: "member:elsewhere", Unit: "L2", Role: "operator"}}}
	split := false
	calls := 0
	source := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Since, Until time.Time
			Mode         string
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil || q.Mode != "intervals" {
			WriteJSON(w, http.StatusOK, map[string]any{})
			return
		}
		calls++
		input := platform.ObservationIntervalInput{Since: q.Since, Until: q.Until, Revision: "collector-1", Coverage: &platform.DriverCoverage{AvailableSince: q.Since, CompleteUntil: q.Until, Good: 30}}
		for i := range 30 {
			value := "run"
			if i >= 2 && i < 10 {
				value = "down"
			}
			if split && i == 6 {
				value = "run"
			}
			sampleAt := q.Since.Add(time.Duration(i*10) * time.Second)
			input.Points = append(input.Points, platform.ObservationStatePoint{At: sampleAt, ReceivedAt: sampleAt, Quality: "good", Value: value})
		}
		WriteJSON(w, http.StatusOK, map[string]any{"intervals": input})
	})
	var entries []Entry
	tn := declaredIndustryTenant(t, id, "manufacturing", seats, at, declaredFixtureOptions{Entries: &entries, Observations: source, Apps: []platform.App{enterprise.New(id, organization)}})
	pageFound := false
	for _, definition := range tn.Definitions(memberOf(t, tn, "builder")) {
		if definition.Ref.Kind == platform.AssetPage && definition.Ref.Name == "mesdowntime" {
			pageFound = definition.Page != nil && definition.Page.Layout == "composed" && len(definition.Page.Sections) == 4 && definition.Page.Sections[1].Widget == "record-view"
		}
	}
	if !pageFound {
		t.Fatal("the package lost its downtime review page")
	}
	decide(t, tn, "planner", "build", "build.mescenter.create", "build.mescenter", "center", map[string]any{"name": "Assembly", "line": "L1"}, at)
	decide(t, tn, "planner", "build", "build.mesresource.create", "build.mesresource", "resource", map[string]any{"name": "Assembly station", "center": "center", "active": true}, at)
	decide(t, tn, "planner", "build", "build.mesdevice.create", "build.mesdevice", "device", map[string]any{"name": "Line one controller", "resource": "resource", "device": "plc-line1", "unit": "L1", "notify": true, "remindhours": 1}, at)
	evaluate := func(key string) {
		t.Helper()
		answer, problem := tn.EvaluateObservation(context.Background(), memberOf(t, tn, "planner"), platform.AssetRef{App: "build", Kind: platform.AssetObservation, Name: "messtates"}, ObservationEvaluate{Record: "device", Until: at, Key: key}, at)
		if problem != nil || answer.Receipt == nil {
			t.Fatalf("declared interval: %+v %v", answer, problem)
		}
		tn.Work(at.Add(time.Second))
	}
	active := func() []map[string]any {
		t.Helper()
		rows, problem := tn.Records(memberOf(t, tn, "planner"), "build.mesdowntime", platform.Query{Domain: platform.Raw([]any{[]any{"retired", "=", false}}), Limit: 100}, at)
		if problem != nil {
			t.Fatal(problem)
		}
		out := []map[string]any{}
		for _, raw := range rows.Records {
			var row map[string]any
			_ = json.Unmarshal(platform.Raw(raw), &row)
			out = append(out, row)
		}
		return out
	}
	evaluate("first")
	rows := active()
	if len(rows) != 1 {
		t.Fatalf("missing downtime: %+v", rows)
	}
	original := rows[0]["id"].(string)
	if rows[0]["unit"] != "L1" || rows[0]["source"] != "messtates" {
		t.Fatalf("lost declared provenance or organizational scope: %+v", rows[0])
	}
	tasks, _, problem := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{Limit: 100})
	if problem != nil || len(tasks) != 1 || len(tasks[0].Candidates) != 1 || tasks[0].Candidates[0] != "planner" || tasks[0].Due.IsZero() {
		instances, _, _ := platform.Find[flow.FlowInstance](tn.automation(flow.ID, false), platform.Query{Limit: 100})
		t.Fatalf("work ignored organizational recipients: tasks=%s flows=%s error=%v", platform.Raw(tasks), platform.Raw(instances), problem)
	}
	if _, problem := tn.RecordOf(memberOf(t, tn, "elsewhere"), "build.mesdowntime", original, at); problem == nil {
		t.Fatal("another line read downtime")
	}
	if got := refuse(t, tn, "elsewhere", "build", "build.mesdowntime.reason", "build.mesdowntime", original, map[string]any{"reason": "Wrong line"}, at); got == "ok" {
		t.Fatal("another line changed reason")
	}
	notices := len(tn.notices.all)
	evaluate("same")
	if len(active()) != 1 || len(tn.notices.all) != notices {
		t.Fatal("unchanged source duplicated event or notification")
	}
	tn.Work(at.Add(2 * time.Hour))
	overdue := len(tn.notices.all)
	tn.Work(at.Add(2*time.Hour + time.Minute))
	if overdue <= notices || len(tn.notices.all) != overdue {
		t.Fatal("overdue reminder missing or repeated")
	}
	decide(t, tn, "operator", "build", "build.mesdowntime.reason", "build.mesdowntime", original, map[string]any{"reason": "Power isolated for setup"}, at.Add(2*time.Hour))
	tasks, _, _ = platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{Limit: 100})
	if tasks[0].State == "open" {
		t.Fatal("reason did not close original work")
	}
	split = true
	evaluate("split")
	rows = active()
	if len(rows) != 2 {
		t.Fatalf("split did not produce two review targets: %+v", rows)
	}
	for _, row := range rows {
		if row["review"] != true || row["reason"] != nil && row["reason"] != "" {
			t.Fatal("split copied a human decision")
		}
	}
	successor := rows[0]["id"].(string)
	if got := refuse(t, tn, "operator", "build", "build.mesdowntime.reason", "build.mesdowntime", successor, map[string]any{"reason": "Unreviewed attribution"}, at); got == "ok" {
		t.Fatal("operator bypassed split review")
	}
	decide(t, tn, "planner", "build", "build.mesdowntime.review", "build.mesdowntime", successor, map[string]any{"reason": "Reviewed predecessor: setup"}, at)
	reviewed := declaredRecord(t, tn, "planner", "build.mesdowntime", successor, at)
	if reviewed["review"] != false || reviewed["reason"] != "Reviewed predecessor: setup" {
		t.Fatal("review did not retain explicit attribution")
	}
	old := declaredRecord(t, tn, "planner", "build.mesdowntime", original, at)
	if old["reason"] != "Power isolated for setup" || old["retired"] != true {
		t.Fatal("correction erased original decision")
	}
	for _, entry := range entries {
		if strings.Contains(string(entry.Body), "\"receivedAt\"") {
			t.Fatal("native samples escaped into accepted business state")
		}
	}
	if calls != 3 {
		t.Fatalf("unexpected source calls: %d", calls)
	}
	fresh := composeDeclaredTenant(t, id, seats, enterprise.New(id, organization))
	fresh.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("replay called a state store")
		return nil, nil
	}
	if err := fresh.Replay(entries); err != nil || snapshot(tn) != snapshot(fresh) {
		t.Fatalf("declared downtime recovery differs: %v", err)
	}
	for _, definition := range tn.Definitions(memberOf(t, tn, "builder")) {
		if definition.Ref.Kind == platform.AssetObservation && definition.Ref.Name == "messtates" && definition.Observation != nil && definition.Observation.Intervals == nil {
			t.Fatal("package roundtrip lost interval policy")
		}
	}
}
